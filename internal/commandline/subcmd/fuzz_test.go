package subcmd

import (
	"encoding/json"
	"testing"
)

// checkJSONObject accepts exactly one JSON object with unique keys.
func FuzzCheckJSONObject(f *testing.F) {
	for _, s := range []string{`{"body":"x"}`, `{"a":1,"a":2}`, `[]`, `{"a":{"b":1}} 1`, `{`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if checkJSONObject(b) != nil {
			return
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("accepted %q: %v", b, err)
		}
	})
}
