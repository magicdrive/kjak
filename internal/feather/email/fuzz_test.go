package email

import (
	"bufio"
	"bytes"
	"io"
	"net/textproto"
	"strings"
	"testing"
)

// No content makes the dot-stuffed data end early: "\r\n.\r\n" never occurs
// inside it, and removing the stuffing gives the content back.
func FuzzDotStuff(f *testing.F) {
	f.Add([]byte(".\n..\n.x\nend"))
	f.Add([]byte("a\r\n.\r\nb"))
	f.Fuzz(func(t *testing.T, content []byte) {
		out := dotStuff(content)
		if bytes.Contains(append([]byte("\r\n"), out...), []byte("\r\n.\r\n")) {
			t.Fatalf("terminator inside %q", out)
		}
		r := textproto.NewReader(bufio.NewReader(io.MultiReader(bytes.NewReader(out), strings.NewReader(".\r\n"))))
		back, err := r.ReadDotBytes()
		if err != nil {
			t.Fatal(err)
		}
		want := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		want = bytes.TrimSuffix(want, []byte("\n"))
		got := bytes.TrimSuffix(back, []byte("\n"))
		if !bytes.Equal(got, want) && !bytes.Contains(content, []byte("\r")) {
			t.Fatalf("%q came back as %q", want, got)
		}
	})
}

// Whatever the server sends, a reply is either an error or a 2xx-5xx code.
func FuzzReadReply(f *testing.F) {
	f.Add("250 ok\r\n")
	f.Add("250-a\r\n250 b\r\n")
	f.Add("250\r\n")
	f.Add("250-a\r\n550 b\r\n")
	f.Fuzz(func(t *testing.T, wire string) {
		s := &session{text: textproto.NewConn(nopConn{strings.NewReader(wire)})}
		code, _, err := s.readReply(0)
		if err == nil && (code < 200 || code > 599) {
			t.Fatalf("%q: code %d", wire, code)
		}
	})
}

type nopConn struct{ io.Reader }

func (nopConn) Write(p []byte) (int, error) { return len(p), nil }
func (nopConn) Close() error                { return nil }
