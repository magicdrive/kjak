package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// The binary resolves a secret reference from the user's secrets file,
// from an explicit one ($KJAK_SECRETS), and refuses a file others can read.
func TestBinarySecretsFiles(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	s := newSandbox(t)
	s.must("feather", "add", "dev", "--type", "slack", "--secret-ref", "dev.hook")
	user := filepath.Join(filepath.Dir(s.dir), "xdg", "kjak", "secrets.toml")
	if err := os.WriteFile(user, []byte("[dev]\nhook = \""+srv.URL+"/services/T/B/X\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.must("send", "--to", "dev", "hi")
	if hits.Load() != 1 {
		t.Fatal("not delivered")
	}
	explicit := filepath.Join(s.dir, "ci.toml")
	if err := os.WriteFile(explicit, []byte("[dev]\nhook = \""+srv.URL+"/services/T/B/Y\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.env = append(s.env, "KJAK_SECRETS="+explicit)
	r := s.must("config", "path", "-o", "json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc["secrets_source"] != "explicit" || doc["secrets_state"] != "1 secret(s)" {
		t.Fatalf("config path: %s %v", r.stdout, err)
	}
	s.must("send", "--to", "dev", "hi")
	// others can read it: the mode bits (Unix), an ACE for Everyone
	// (Windows)
	want := "group or others"
	if runtime.GOOS == "windows" {
		want = "Everyone"
		if out, err := exec.Command("icacls", explicit, "/grant", "*S-1-1-0:(R)").CombinedOutput(); err != nil {
			t.Fatalf("icacls: %v %s", err, out)
		}
	} else if err := os.Chmod(explicit, 0o644); err != nil {
		t.Fatal(err)
	}
	r = s.kjak("", "send", "--to", "dev", "hi")
	if r.code != 2 || !strings.Contains(r.stderr, want) || hits.Load() != 2 {
		t.Fatalf("a readable secrets file: %+v", r)
	}
	if strings.Contains(r.stdout+r.stderr, "services/T/B") {
		t.Fatal("the secret was printed")
	}
	// config path says why it is refused
	r = s.must("config", "path", "-o", "json")
	doc = nil
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if st, _ := doc["secrets_state"].(string); !strings.HasPrefix(st, "refused") || !strings.Contains(st, want) || doc["secrets_permissions_checked"] != true {
		t.Fatalf("config path: %s", r.stdout)
	}
	if r = s.must("config", "path"); !strings.Contains(r.stdout, "refused") || !strings.Contains(r.stdout, want) || strings.Contains(r.stdout, "not checked") {
		t.Fatalf("config path: %s", r.stdout)
	}
}
