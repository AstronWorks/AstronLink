package protocol

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	"sync"
)

// encPool reuses bytes.Buffer allocations across WriteMessage calls to reduce
// GC pressure on high-throughput control connections.
var encPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// Decode is a generic helper that decodes a gob-encoded payload into T,
// eliminating per-call boilerplate across the codebase.
func Decode[T any](payload []byte) (T, error) {
	var v T
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

// WriteMessage encodes payload using gob, wraps it in an Envelope, and writes
// the frame to conn with a 4-byte big-endian length prefix.
func WriteMessage(conn net.Conn, msgType MsgType, payload any) error {
	// Acquire a pooled buffer for the payload encoding.
	payloadBuf := encPool.Get().(*bytes.Buffer)
	payloadBuf.Reset()
	defer encPool.Put(payloadBuf)

	if err := gob.NewEncoder(payloadBuf).Encode(payload); err != nil {
		return fmt.Errorf("framing: encode payload: %w", err)
	}

	return WriteRawMessage(conn, msgType, payloadBuf.Bytes())
}

// WriteRawMessage writes a frame whose Envelope.Payload is raw instead of a
// gob-encoded struct. It is used for high-rate frames such as MsgVPNPacket,
// where the receiver consumes Envelope.Payload directly. raw is not retained.
func WriteRawMessage(conn net.Conn, msgType MsgType, raw []byte) error {
	env := Envelope{
		Type:    msgType,
		Payload: raw,
	}
	envBuf := encPool.Get().(*bytes.Buffer)
	envBuf.Reset()
	defer encPool.Put(envBuf)

	// Reserve the 4-byte length prefix so prefix + envelope go out in a single
	// Write (one TLS record per frame instead of two).
	envBuf.Write([]byte{0, 0, 0, 0})
	if err := gob.NewEncoder(envBuf).Encode(env); err != nil {
		return fmt.Errorf("framing: encode envelope: %w", err)
	}

	frame := envBuf.Bytes()
	binary.BigEndian.PutUint32(frame[:4], uint32(len(frame)-4))

	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("framing: write frame: %w", err)
	}

	return nil
}

// ReadMessage reads a length-prefixed frame from conn and returns the decoded
// Envelope. Returns an error if the message exceeds MaxMessageSize, if the
// connection is closed, or if the data is truncated.
func ReadMessage(conn net.Conn) (*Envelope, error) {
	// Read 4-byte big-endian length prefix.
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("framing: read length prefix: %w", err)
	}

	// DoS guard.
	if length > MaxMessageSize {
		return nil, fmt.Errorf("framing: message too large: %d bytes", length)
	}

	// Read exactly length bytes.
	buf := make([]byte, length)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, fmt.Errorf("framing: read envelope body: %w", err)
	}

	// Decode Envelope.
	var env Envelope
	if err := gob.NewDecoder(bytes.NewReader(buf)).Decode(&env); err != nil {
		return nil, fmt.Errorf("framing: decode envelope: %w", err)
	}

	return &env, nil
}
