// Package email is the e-mail feather: one SMTP transaction per attempt.
//
// kjak speaks SMTP itself (net/textproto) instead of using net/smtp, because
// the acceptance of a message depends on how far the transaction got, which
// net/smtp does not tell: an error before the end of the data is a message
// the server certainly did not accept, an error after the terminating "."
// was sent is one it may have accepted (docs/delivery.md). The SMTP
// libraries do not tell either (docs/development.md, Dependencies).
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
)

// Security modes.
const (
	SecuritySTARTTLS = "starttls"
	SecurityTLS      = "tls"
	SecurityNone     = "none"
)

// DefaultPort of a security mode.
func DefaultPort(security string) int {
	switch security {
	case SecurityTLS:
		return 465
	case SecurityNone:
		return 25
	}
	return 587
}

// Feather sends to a fixed list of recipients through one SMTP server.
type Feather struct {
	Host          string
	Port          int
	Security      string
	User          string
	Password      credential.Secret
	From          string
	To            []string
	SubjectPrefix string

	Clock clock.Clock
	// Rand is the source of Message-ID values (crypto/rand when nil).
	Rand io.Reader
	// TLSConfig overrides the TLS configuration (tests: a test CA).
	TLSConfig *tls.Config
	// Dial overrides the dialer (tests).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// HeloName is the EHLO name ("localhost" when empty).
	HeloName string
}

// stage of the transaction: what an error at that point means.
type stage int

const (
	stageBeforeData stage = iota // nothing of the message is with the server
	stageData                    // the content is being sent; no terminator yet
	stageSent                    // the terminating "." was sent: the server may have it
)

type session struct {
	f     *Feather
	ctx   context.Context
	conn  net.Conn
	text  *textproto.Conn
	stage stage
	ext   map[string]string
	// stop ends the watch of ctx (connect)
	stop func() bool
}

func (f *Feather) Deliver(ctx context.Context, msg core.Message) feather.Outcome {
	content, err := f.Compose(msg)
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, err)
	}
	s := &session{f: f, ctx: ctx}
	defer s.close()
	if o, ok := s.connect(); !ok {
		return o
	}
	return s.transaction(content)
}

func (f *Feather) addr() string {
	port := f.Port
	if port == 0 {
		port = DefaultPort(f.Security)
	}
	return net.JoinHostPort(f.Host, strconv.Itoa(port))
}

func (f *Feather) tlsConfig() *tls.Config {
	if f.TLSConfig != nil {
		c := f.TLSConfig.Clone()
		if c.ServerName == "" {
			c.ServerName = f.Host
		}
		return c
	}
	return &tls.Config{ServerName: f.Host, MinVersion: tls.VersionTLS12}
}

func (s *session) close() {
	if s.stop != nil {
		s.stop()
	}
	if s.text != nil {
		_ = s.text.Close()
	} else if s.conn != nil {
		_ = s.conn.Close()
	}
}

// fail classifies an error at the current stage.
func (s *session) fail(what string, err error) feather.Outcome {
	var te *textproto.Error
	if errors.As(err, &te) && s.stage != stageData {
		return replyOutcome(what, te.Code, te.Msg)
	}
	if s.ctx.Err() != nil {
		if s.stage == stageSent {
			return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("%v while waiting for the reply to the message; the server may have accepted it", feather.ContextErr(s.ctx)))
		}
		return feather.Rejected(feather.ContextCode(s.ctx), 0, false, fmt.Errorf("%v during %s", feather.ContextErr(s.ctx), what))
	}
	reason := describeNetError(err)
	if s.stage == stageSent {
		return feather.Unknown(core.ErrCodeAmbiguous, fmt.Errorf("the reply to the message was lost (%s); the server may have accepted it", reason))
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return feather.Rejected(core.ErrCodeProtocol, 0, false, fmt.Errorf("%s: %s", what, reason))
	}
	if errors.Is(err, errTooMuchData) {
		return feather.Rejected(core.ErrCodeProtocol, 0, false, fmt.Errorf("%s: %v", what, err))
	}
	var pe textproto.ProtocolError
	if errors.As(err, &pe) {
		return feather.Rejected(core.ErrCodeProtocol, 0, false, fmt.Errorf("%s: malformed SMTP reply", what))
	}
	return feather.Rejected(core.ErrCodeNetwork, 0, true, fmt.Errorf("%s: %s", what, reason))
}

// replyOutcome classifies a negative reply before the message was accepted:
// 4xx is transient (retryable), 5xx permanent.
func replyOutcome(what string, code int, msg string) feather.Outcome {
	err := fmt.Errorf("%s: SMTP %d %s", what, code, sanitize(msg))
	switch {
	case code >= 400 && code < 500:
		return feather.Rejected(core.ErrCodeServer, code, true, err)
	case code >= 500 && code < 600:
		return feather.Rejected(core.ErrCodeRejected, code, false, err)
	}
	return feather.Rejected(core.ErrCodeProtocol, code, false, err)
}

func (s *session) connect() (feather.Outcome, bool) {
	f := s.f
	dial := f.Dial
	if dial == nil {
		var d net.Dialer
		dial = d.DialContext
	}
	raw, err := dial(s.ctx, "tcp", f.addr())
	if err != nil {
		return s.fail("connect", err), false
	}
	s.conn = &limitedConn{Conn: raw, left: maxSessionRead}
	// a done context unblocks every read and write of the session
	s.stop = context.AfterFunc(s.ctx, func() { _ = raw.SetDeadline(time.Unix(1, 0)) })
	if f.Security == SecurityTLS {
		tc := tls.Client(s.conn, f.tlsConfig())
		if err := tc.HandshakeContext(s.ctx); err != nil {
			return s.fail("TLS handshake", err), false
		}
		s.conn = tc
	}
	s.text = textproto.NewConn(s.conn)
	if _, _, err := s.readReply(220); err != nil {
		return s.fail("greeting", err), false
	}
	if err := s.hello(); err != nil {
		return s.fail("EHLO", err), false
	}
	if f.Security == SecurityTLS || f.Security == SecurityNone {
		return feather.Outcome{}, true
	}
	if _, ok := s.ext["STARTTLS"]; !ok {
		return feather.Rejected(core.ErrCodeProtocol, 0, false, errors.New("the server does not offer STARTTLS (set security = \"tls\" for implicit TLS)")), false
	}
	if err := s.cmd(220, "STARTTLS"); err != nil {
		return s.fail("STARTTLS", err), false
	}
	// Bytes already received after the 220 came in clear text: they are not
	// protected by the TLS that follows (STARTTLS response injection).
	if s.text.R.Buffered() > 0 {
		return feather.Rejected(core.ErrCodeProtocol, 0, false, errors.New("STARTTLS: the server sent clear-text data after its reply; refusing the session")), false
	}
	tc := tls.Client(s.conn, f.tlsConfig())
	if err := tc.HandshakeContext(s.ctx); err != nil {
		return s.fail("TLS handshake", err), false
	}
	s.conn = tc
	s.text = textproto.NewConn(tc)
	if err := s.hello(); err != nil {
		return s.fail("EHLO", err), false
	}
	return feather.Outcome{}, true
}

func (s *session) hello() error {
	name := s.f.HeloName
	if name == "" {
		name = "localhost"
	}
	if err := s.text.PrintfLine("EHLO %s", name); err != nil {
		return err
	}
	_, msg, err := s.readReply(250)
	if err != nil {
		return err
	}
	s.ext = map[string]string{}
	lines := strings.Split(msg, "\n")
	for _, line := range lines[1:] {
		k, v, _ := strings.Cut(line, " ")
		s.ext[strings.ToUpper(k)] = v
	}
	return nil
}

func (s *session) cmd(expect int, format string, args ...any) error {
	if err := s.text.PrintfLine(format, args...); err != nil {
		return err
	}
	_, _, err := s.readReply(expect)
	return err
}

// readReply reads one SMTP reply (RFC 5321 4.2): lines "NNN-text", then
// "NNN text" or a bare "NNN" (the text is optional; net/textproto refuses a
// bare code). The code must be the same on every line. A code that does not
// match expect (1-9: its first digit, 10-99: its first two, 100-999: all;
// 0: any) is a *textproto.Error with the code and the text.
func (s *session) readReply(expect int) (int, string, error) {
	code := 0
	var lines []string
	for {
		line, err := s.text.ReadLine()
		if err != nil {
			return 0, "", err
		}
		if len(line) < 3 {
			return 0, "", textproto.ProtocolError("short reply")
		}
		c, err := strconv.Atoi(line[:3])
		if err != nil || c < 200 || c > 599 || line[0] == '+' || line[0] == '-' {
			return 0, "", textproto.ProtocolError("invalid reply code")
		}
		if code != 0 && c != code {
			return 0, "", textproto.ProtocolError("reply code changed within a reply")
		}
		code = c
		rest := line[3:]
		if rest == "" || rest[0] == ' ' {
			lines = append(lines, strings.TrimPrefix(rest, " "))
			break
		}
		if rest[0] != '-' {
			return 0, "", textproto.ProtocolError("invalid reply separator")
		}
		lines = append(lines, rest[1:])
	}
	msg := strings.Join(lines, "\n")
	if (1 <= expect && expect < 10 && code/100 != expect) ||
		(10 <= expect && expect < 100 && code/10 != expect) ||
		(100 <= expect && expect < 1000 && code != expect) {
		return code, msg, &textproto.Error{Code: code, Msg: msg}
	}
	return code, msg, nil
}

func (s *session) auth() (feather.Outcome, bool) {
	f := s.f
	mechs := strings.Fields(strings.ToUpper(s.ext["AUTH"]))
	has := func(m string) bool {
		for _, x := range mechs {
			if x == m {
				return true
			}
		}
		return false
	}
	switch {
	case has("PLAIN"):
		token := base64.StdEncoding.EncodeToString([]byte("\x00" + f.User + "\x00" + f.Password.Reveal()))
		if err := s.cmd(235, "AUTH PLAIN %s", token); err != nil {
			return s.authFail(err), false
		}
	case has("LOGIN"):
		if err := s.cmd(334, "AUTH LOGIN"); err != nil {
			return s.authFail(err), false
		}
		if err := s.cmd(334, "%s", base64.StdEncoding.EncodeToString([]byte(f.User))); err != nil {
			return s.authFail(err), false
		}
		if err := s.cmd(235, "%s", base64.StdEncoding.EncodeToString([]byte(f.Password.Reveal()))); err != nil {
			return s.authFail(err), false
		}
	default:
		return feather.Rejected(core.ErrCodeProtocol, 0, false, errors.New("AUTH: the server offers neither PLAIN nor LOGIN")), false
	}
	return feather.Outcome{}, true
}

// authFail: a refused login is permanent (retrying a wrong password locks
// accounts), except a 454 or 421 temporary failure. The text of the reply is
// left out: a server may repeat the AUTH argument, the encoded password.
func (s *session) authFail(err error) feather.Outcome {
	var te *textproto.Error
	if errors.As(err, &te) {
		if te.Code == 454 || te.Code == 421 {
			return feather.Rejected(core.ErrCodeServer, te.Code, true, fmt.Errorf("AUTH: SMTP %d temporary authentication failure", te.Code))
		}
		return feather.Rejected(core.ErrCodeRejected, te.Code, false, fmt.Errorf("AUTH: SMTP %d authentication failed", te.Code))
	}
	return s.fail("AUTH", err)
}

func (s *session) transaction(content []byte) feather.Outcome {
	f := s.f
	if f.User != "" {
		if o, ok := s.auth(); !ok {
			return o
		}
	}
	if err := s.cmd(250, "MAIL FROM:<%s>", f.From); err != nil {
		return s.fail("MAIL FROM", err)
	}
	for _, rcpt := range f.To {
		if err := s.text.PrintfLine("RCPT TO:<%s>", rcpt); err != nil {
			return s.fail("RCPT TO", err)
		}
		code, msg, err := s.readReply(25) // 250 or 251
		if err != nil {
			var te *textproto.Error
			if errors.As(err, &te) {
				return replyOutcome("RCPT TO <"+rcpt+">", code, msg)
			}
			return s.fail("RCPT TO", err)
		}
	}
	if err := s.cmd(354, "DATA"); err != nil {
		return s.fail("DATA", err)
	}

	s.stage = stageData
	if _, err := s.text.W.Write(dotStuff(content)); err != nil {
		return s.fail("sending the message", err)
	}
	if err := s.text.W.Flush(); err != nil {
		return s.fail("sending the message", err)
	}
	// From the first byte of the terminator on, the server may complete the
	// message: an error now is ambiguous.
	s.stage = stageSent
	if _, err := s.text.W.WriteString(".\r\n"); err != nil {
		return s.fail("ending the message", err)
	}
	if err := s.text.W.Flush(); err != nil {
		return s.fail("ending the message", err)
	}
	code, msg, err := s.readReply(0)
	if err != nil {
		return s.fail("reply to the message", err)
	}
	switch {
	case code >= 200 && code < 300:
		_ = s.cmd(221, "QUIT")
		return feather.Confirmed(code)
	case code >= 400 && code < 600:
		// an explicit refusal of the data: not accepted
		return replyOutcome("message", code, msg)
	}
	return feather.Unknown(core.ErrCodeProtocol, fmt.Errorf("unexpected SMTP reply %d to the message; the server may have accepted it", code))
}

// Compose renders msg as an RFC 5322 message with CRLF line endings.
func (f *Feather) Compose(msg core.Message) ([]byte, error) {
	id, err := f.messageID()
	if err != nil {
		return nil, err
	}
	subject := msg.Title
	if subject == "" {
		subject = "[" + strings.ToUpper(string(msg.Level)) + "] " + firstLine(msg.Body)
	}
	if f.SubjectPrefix != "" {
		subject = f.SubjectPrefix + " " + subject
	}
	if r := []rune(subject); len(r) > 200 {
		subject = string(r[:199]) + "…"
	}
	now := time.Now()
	if f.Clock != nil {
		now = f.Clock.Now()
	}
	var b bytes.Buffer
	header := func(k, v string) {
		b.WriteString(k + ": " + v + "\r\n")
	}
	header("From", f.From)
	header("To", fold("To", f.To, ", "))
	header("Subject", fold("Subject", strings.Split(mime.QEncoding.Encode("UTF-8", subject), " "), " "))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+id+"@kjak>")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=UTF-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("X-Kjak-Level", string(msg.Level))
	b.WriteString("\r\n")

	text := strings.ReplaceAll(msg.Body, "\r\n", "\n")
	if msg.URL != "" {
		text += "\n\n" + msg.URL
	}
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(text + "\n")); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// fold joins the parts of the value of header name with sep, and folds
// before a part that would make the line longer than 76 characters (RFC 5322
// 2.1.1 limits a line to 998: a long encoded subject, many recipients). The
// space that ends sep becomes the folding white space, so unfolding gives
// back the parts joined with sep.
func fold(name string, parts []string, sep string) string {
	var b strings.Builder
	line := len(name) + len(": ")
	for i, p := range parts {
		if i > 0 {
			if line+len(sep)+len(p) > 76 {
				b.WriteString(strings.TrimSuffix(sep, " ") + "\r\n ")
				line = 1
			} else {
				b.WriteString(sep)
				line += len(sep)
			}
		}
		b.WriteString(p)
		line += len(p)
	}
	return b.String()
}

func (f *Feather) messageID() (string, error) {
	r := f.Rand
	if r == nil {
		r = rand.Reader
	}
	buf := make([]byte, 16)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// dotStuff doubles a leading "." of each line (RFC 5321 4.5.2) and ends the
// content with CRLF.
func dotStuff(content []byte) []byte {
	var out bytes.Buffer
	lines := bytes.Split(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")), []byte("\n"))
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}
	for _, l := range lines {
		if bytes.HasPrefix(l, []byte(".")) {
			out.WriteByte('.')
		}
		out.Write(l)
		out.WriteString("\r\n")
	}
	return out.Bytes()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// maxSessionRead bounds what kjak reads from an SMTP server in a session
// (replies are short; RFC 5321 limits a reply line to 512 octets).
const maxSessionRead = 1 << 20

var errTooMuchData = errors.New("the server sent more than 1 MiB of replies")

// limitedConn fails reads after a total of left bytes.
type limitedConn struct {
	net.Conn
	left int
}

func (c *limitedConn) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errTooMuchData
	}
	if len(p) > c.left {
		p = p[:c.left]
	}
	n, err := c.Conn.Read(p)
	c.left -= n
	return n, err
}

func describeNetError(err error) string {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed by the server"
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return "TLS certificate verification failed"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Op + ": " + op.Err.Error()
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "DNS lookup failed"
	}
	return "connection error"
}

func sanitize(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "..."
	}
	return s
}
