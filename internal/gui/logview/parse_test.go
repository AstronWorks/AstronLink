package logview

import "testing"

func TestParseLine(t *testing.T) {
	ln := ParseLine(`{"time":"2026-10-26T16:02:23.0441115+09:00","level":"INFO","msg":"dialing client","component":"server","name":"my-pc","addr":"192.168.1.221:8443"}`)
	if ln.Time != "10-26 16:02:23" || ln.Level != "INFO" || ln.Msg != "dialing client" {
		t.Fatalf("unexpected %+v", ln)
	}
	if ln.Attrs != "name=my-pc  addr=192.168.1.221:8443" {
		t.Fatalf("attrs = %q", ln.Attrs)
	}

	ln = ParseLine(`{"level":"WARN","msg":"x","err":"read tcp: i/o timeout","n":3}`)
	if ln.Level != "WARN" || ln.Rank() != 2 || ln.Attrs != `err="read tcp: i/o timeout"  n=3` {
		t.Fatalf("unexpected %+v", ln)
	}

	ln = ParseLine("panic: boom")
	if ln.Msg != "panic: boom" || ln.Level != "" || ln.Rank() != 1 {
		t.Fatalf("unexpected %+v", ln)
	}
}

func TestParseTail(t *testing.T) {
	data := []byte("cut-off\n{\"msg\":\"a\"}\n\n{\"msg\":\"b\"}\n{\"msg\":\"c\"}\n")
	lines := Parse(data, true, 2)
	if len(lines) != 2 || lines[0].Msg != "b" || lines[1].Msg != "c" {
		t.Fatalf("unexpected %+v", lines)
	}
	if got := Parse([]byte("no newline"), true, 0); len(got) != 0 {
		t.Fatalf("partial single line kept: %+v", got)
	}
}
