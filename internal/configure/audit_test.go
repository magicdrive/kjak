package configure

import (
	"strings"
	"testing"
)

// A configuration file is read with a size limit: an untrusted project file
// (read before its trust is checked) cannot make kjak load gigabytes.
func TestOversizedConfigRefused(t *testing.T) {
	fx := newFixture(t)
	big := "# " + strings.Repeat("x", MaxConfigSize) + "\n"
	write(t, fx.paths.Project, big)
	_, err := Load(fx.paths)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ReadTrustStore(fx.paths.Project); err == nil {
		t.Fatal("an oversized trust store was read")
	}
}

// Parse errors name the position, never the value: a literal secret on the
// broken line is not repeated.
func TestParseErrorsDoNotEchoValues(t *testing.T) {
	for _, doc := range []string{
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = \"https://hooks/SECRETTOKEN\" \n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = \"https://hooks/SECRETTOKEN\", literal = \"x\" }\n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = 'https://hooks/SECRETTOKEN\n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = \"https://hooks/SECRETTOKEN\\q\" }\n",
		"[feathers.a]\ntype = \"email\"\nhost = \"h\"\nuser = \"u\"\npassword = { literal = \"SECRETTOKEN\", env = \"X\" }\nfrom = \"a@example.com\"\nto = [\"b@example.com\"]\n",
		"[feathers.a]\ntype = \"slack\"\nwebhook_url = { literal = \"SECRETTOKEN\" }\n",
	} {
		_, err := Parse([]byte(doc))
		if err == nil {
			t.Errorf("accepted: %q", doc)
			continue
		}
		if strings.Contains(err.Error(), "SECRETTOKEN") {
			t.Errorf("the error repeats the secret: %v", err)
		}
	}
	// a key twice is an error, not "last wins"
	if _, err := Parse([]byte("[defaults]\nlevel = \"info\"\nlevel = \"error\"\n")); err == nil {
		t.Fatal("a duplicate key was accepted")
	}
}
