// Package logview parses the JSON (log/slog) log files written by the server
// and client and renders them as a filterable, auto-scrolling list.
package logview

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// Line is one parsed log record.
type Line struct {
	Time  string // "01-02 15:04:05" in the record's own time zone
	Level string // DEBUG | INFO | WARN | ERROR, "" when absent
	Msg   string
	Attrs string // remaining fields as key=value pairs, in file order
	Raw   string
}

// Rank orders levels for filtering; records without a level count as INFO.
func (l Line) Rank() int { return levelRank(l.Level) }

func levelRank(level string) int {
	switch level {
	case "DEBUG":
		return 0
	case "WARN", "WARNING":
		return 2
	case "ERROR":
		return 3
	}
	return 1
}

// ParseLine parses one slog JSON record. Anything else becomes a line whose
// message is the raw text.
func ParseLine(s string) Line {
	raw := strings.TrimRight(s, "\r")
	ln := Line{Raw: raw}
	body := strings.TrimSpace(raw)
	if !strings.HasPrefix(body, "{") {
		ln.Msg = body
		return ln
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		ln.Msg = body
		return ln
	}
	var attrs []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return Line{Raw: raw, Msg: body}
		}
		key, _ := kt.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return Line{Raw: raw, Msg: body}
		}
		v := rawString(val)
		switch key {
		case "time":
			ln.Time = formatTime(v)
		case "level":
			ln.Level = strings.ToUpper(v)
		case "msg":
			ln.Msg = v
		case "component":
			// Constant per process; it only adds noise to the view.
		default:
			attrs = append(attrs, key+"="+quoteIfNeeded(v))
		}
	}
	ln.Attrs = strings.Join(attrs, "  ")
	return ln
}

func rawString(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return string(v)
}

func quoteIfNeeded(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\"=") {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return s
}

func formatTime(v string) string {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return v
	}
	return t.Format("01-02 15:04:05")
}

// Parse splits data into lines. When partial is true the first line is
// assumed to be cut off (data is the tail of a larger file) and dropped. At
// most max lines are kept, the newest ones.
func Parse(data []byte, partial bool, max int) []Line {
	if partial {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	parts := strings.Split(string(data), "\n")
	out := make([]Line, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, ParseLine(p))
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}
