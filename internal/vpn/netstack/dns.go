package netstack

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	dnsAnswerTTL   = 60 // seconds
	dnsMaxAnswers  = 8  // keeps every reply well under the 512-byte UDP limit
	dnsLookupLimit = 5 * time.Second
)

// serveDNS answers queries sent by the client to gateway:53 using the server's
// own resolver, so name resolution happens on the server side (the same
// behaviour as socks5h in the proxy mode).
func (s *Session) serveDNS(c net.Conn) {
	defer c.Close()
	stop := context.AfterFunc(s.ctx, func() { c.Close() })
	defer stop()

	buf := make([]byte, 1500)
	for {
		_ = c.SetReadDeadline(time.Now().Add(dnsIdleTimeout))
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		query := append([]byte(nil), buf[:n]...)
		go func() {
			ctx, cancel := context.WithTimeout(s.ctx, dnsLookupLimit)
			defer cancel()
			if resp := answerDNS(ctx, s.opts.Resolver, query); resp != nil {
				_, _ = c.Write(resp)
			}
		}()
	}
}

// answerDNS builds the reply to one DNS query. A and AAAA questions are
// resolved; any other type gets an empty NOERROR answer (NODATA) so stub
// resolvers fall back to A/AAAA. Malformed queries yield nil (no reply).
func answerDNS(ctx context.Context, r Resolver, query []byte) []byte {
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil || hdr.Response {
		return nil
	}
	q, err := p.Question()
	if err != nil {
		return nil
	}

	respHdr := dnsmessage.Header{
		ID:                 hdr.ID,
		Response:           true,
		OpCode:             hdr.OpCode,
		RecursionDesired:   hdr.RecursionDesired,
		RecursionAvailable: true,
		RCode:              dnsmessage.RCodeSuccess,
	}

	var addrs []netip.Addr
	switch {
	case hdr.OpCode != 0:
		respHdr.RCode = dnsmessage.RCodeNotImplemented
	case q.Class != dnsmessage.ClassINET:
		// NODATA
	case q.Type == dnsmessage.TypeA || q.Type == dnsmessage.TypeAAAA:
		network := "ip4"
		if q.Type == dnsmessage.TypeAAAA {
			network = "ip6"
		}
		// Wire names are absolute ("host."); the Go resolver only matches
		// hosts-file entries for the relative form.
		name := strings.TrimSuffix(q.Name.String(), ".")
		if name == "" {
			break // root: NODATA
		}
		addrs, err = r.LookupNetIP(ctx, network, name)
		if err != nil {
			addrs = nil
			var dnsErr *net.DNSError
			if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
				respHdr.RCode = dnsmessage.RCodeServerFailure
				break
			}
			// "Not found" for one family must not become NXDOMAIN when the
			// name exists with the other family; only report NXDOMAIN when
			// the name has no addresses at all.
			if _, anyErr := r.LookupNetIP(ctx, "ip", name); anyErr != nil {
				respHdr.RCode = dnsmessage.RCodeNameError
			}
		}
	}

	b := dnsmessage.NewBuilder(make([]byte, 0, 512), respHdr)
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil
	}
	if err := b.Question(q); err != nil {
		return nil
	}
	if err := b.StartAnswers(); err != nil {
		return nil
	}
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: dnsAnswerTTL}
	count := 0
	for _, a := range addrs {
		if count == dnsMaxAnswers {
			break
		}
		a = a.Unmap()
		switch {
		case q.Type == dnsmessage.TypeA && a.Is4():
			rh.Type = dnsmessage.TypeA
			err = b.AResource(rh, dnsmessage.AResource{A: a.As4()})
		case q.Type == dnsmessage.TypeAAAA && a.Is6():
			rh.Type = dnsmessage.TypeAAAA
			err = b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: a.As16()})
		default:
			continue
		}
		if err != nil {
			return nil
		}
		count++
	}
	resp, err := b.Finish()
	if err != nil {
		return nil
	}
	return resp
}
