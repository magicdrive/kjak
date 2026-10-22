package credential

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func TestRefValidate(t *testing.T) {
	good := []*Ref{
		{Env: strp("KJAK_HOOK")},
		{Keychain: &KeychainRef{Service: "kjak", Account: "a"}},
		{Literal: strp("https://x")},
	}
	for _, r := range good {
		if err := r.Validate(); err != nil {
			t.Errorf("%s: %v", r.Describe(), err)
		}
	}
	bad := map[string]*Ref{
		"none":          {},
		"env+literal":   {Env: strp("A"), Literal: strp("x")},
		"env+keychain":  {Env: strp("A"), Keychain: &KeychainRef{Service: "s", Account: "a"}},
		"all":           {Env: strp("A"), Keychain: &KeychainRef{Service: "s", Account: "a"}, Literal: strp("x")},
		"bad env":       {Env: strp("A-B")},
		"empty literal": {Literal: strp("")},
		"half keychain": {Keychain: &KeychainRef{Service: "s"}},
		"nil":           nil,
	}
	for name, r := range bad {
		if err := r.Validate(); !errors.Is(err, ErrCredential) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDescribeNeverShowsLiteral(t *testing.T) {
	r := &Ref{Literal: strp("https://hooks.slack.com/services/T/B/SECRET")}
	if d := r.Describe(); strings.Contains(d, "SECRET") || d != "literal:"+Redacted {
		t.Fatalf("describe = %q", d)
	}
}

type fakeKeychain map[string]string

func (k fakeKeychain) Lookup(_ context.Context, service, account string) (string, error) {
	v, ok := k[service+"/"+account]
	if !ok {
		return "", ErrKeychainNotFound
	}
	return v, nil
}

func TestResolve(t *testing.T) {
	env := map[string]string{"HOOK": "https://env.example/x", "EMPTY": ""}
	res := NewResolver(func(k string) (string, bool) { v, ok := env[k]; return v, ok }, fakeKeychain{"kjak/a": "from-keychain"})
	for _, tc := range []struct {
		ref  *Ref
		want string
	}{
		{&Ref{Env: strp("HOOK")}, "https://env.example/x"},
		{&Ref{Keychain: &KeychainRef{Service: "kjak", Account: "a"}}, "from-keychain"},
		{&Ref{Literal: strp("lit")}, "lit"},
	} {
		s, err := res.Resolve(context.Background(), tc.ref)
		if err != nil || s.Reveal() != tc.want {
			t.Errorf("%s: %q, %v", tc.ref.Describe(), s.Reveal(), err)
		}
	}
	for _, r := range []*Ref{{Env: strp("MISSING")}, {Env: strp("EMPTY")}, {Keychain: &KeychainRef{Service: "kjak", Account: "nope"}}} {
		if _, err := res.Resolve(context.Background(), r); !errors.Is(err, ErrCredential) {
			t.Errorf("%s: %v", r.Describe(), err)
		}
	}
	// no keychain on this OS: an error, never an empty secret
	none := NewResolver(func(string) (string, bool) { return "", false }, nil)
	if _, err := none.Resolve(context.Background(), &Ref{Keychain: &KeychainRef{Service: "s", Account: "a"}}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := NewSecret("hunter2-secret")
	for _, out := range []string{
		fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s %q %x", s, s, s, s, s, s), s.String(),
	} {
		if strings.Contains(out, "hunter2") {
			t.Fatalf("printed: %q", out)
		}
	}
	b, _ := json.Marshal(struct{ S Secret }{s})
	if strings.Contains(string(b), "hunter2") {
		t.Fatalf("json: %s", b)
	}
}

func TestRedactor(t *testing.T) {
	var r Redactor
	r.Add(NewSecret("https://hooks.slack.com/services/T01/B02/abcDEF123"))
	r.Add(NewSecret("p@ss word"))
	r.Add(NewSecret("abc")) // too short to search for
	in := `Post "https://hooks.slack.com/services/T01/B02/abcDEF123": EOF; path /services/T01/B02/abcDEF123; q=p%40ss+word; p@ss word; abc`
	out := r.Redact(in)
	for _, leak := range []string{"abcDEF123", "p@ss word", "p%40ss+word", "T01/B02"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q survived: %s", leak, out)
		}
	}
	if !strings.HasSuffix(out, "abc") {
		t.Errorf("a short secret was searched for: %s", out)
	}
	var nilRedactor *Redactor
	if nilRedactor.Redact("x") != "x" {
		t.Fatal("nil redactor")
	}
}

func TestKeychainBackends(t *testing.T) {
	calls := [][]string{}
	run := func(code int, out string) Runner {
		return func(_ context.Context, name string, args ...string) ([]byte, int, error) {
			calls = append(calls, append([]string{name}, args...))
			return []byte(out), code, nil
		}
	}
	found := func(string) (string, error) { return "/usr/bin/secret-tool", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }

	mac := keychainFor("darwin", run(0, "s3cret\n"), found)
	if v, err := mac.Lookup(context.Background(), "kjak", "dev"); err != nil || v != "s3cret" {
		t.Fatalf("mac: %q %v", v, err)
	}
	if got := strings.Join(calls[0], " "); got != "/usr/bin/security find-generic-password -s kjak -a dev -w" {
		t.Fatalf("mac command = %s", got)
	}
	if _, err := keychainFor("darwin", run(44, ""), found).Lookup(context.Background(), "k", "a"); !errors.Is(err, ErrKeychainNotFound) {
		t.Fatalf("mac missing: %v", err)
	}
	if _, err := keychainFor("darwin", run(51, ""), found).Lookup(context.Background(), "k", "a"); err == nil {
		t.Fatal("mac failure accepted")
	}

	linux := keychainFor("linux", run(0, "tok\n"), found)
	if v, err := linux.Lookup(context.Background(), "kjak", "dev"); err != nil || v != "tok" {
		t.Fatalf("linux: %q %v", v, err)
	}
	if got := strings.Join(calls[len(calls)-1], " "); got != "/usr/bin/secret-tool lookup service kjak account dev" {
		t.Fatalf("linux command = %s", got)
	}
	if _, err := keychainFor("linux", run(0, ""), missing).Lookup(context.Background(), "k", "a"); !errors.Is(err, ErrKeychainUnavailable) {
		t.Fatalf("linux without secret-tool: %v", err)
	}
	if _, err := keychainFor("linux", run(1, ""), found).Lookup(context.Background(), "k", "a"); !errors.Is(err, ErrKeychainNotFound) {
		t.Fatalf("linux missing: %v", err)
	}
	if keychainFor("windows", run(0, ""), found) != nil {
		t.Fatal("windows must have no keychain in v0.1.0")
	}
}

// The keychain tool runs under the caller's context: SIGINT ends it.
func TestKeychainLookupCanceled(t *testing.T) {
	blocking := func(ctx context.Context, name string, args ...string) ([]byte, int, error) {
		<-ctx.Done()
		return nil, -1, ctx.Err()
	}
	found := func(string) (string, error) { return "/usr/bin/secret-tool", nil }
	for _, goos := range []string{"darwin", "linux"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := keychainFor(goos, blocking, found).Lookup(ctx, "s", "a"); !errors.Is(err, context.Canceled) {
			t.Errorf("%s: err = %v", goos, err)
		}
	}
}
