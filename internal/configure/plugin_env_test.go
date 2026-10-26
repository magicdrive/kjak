package configure

import (
	"strings"
	"testing"
)

// A plugin never gets kjak's own variables, the dynamic loader's (code
// injected into the plugin) or the Go runtime's through env, whatever their
// case.
func TestCritical_PluginEnvDenied(t *testing.T) {
	for _, name := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "ld_preload", "DYLD_INSERT_LIBRARIES", "dyld_library_path", "KJAK_SECRETS", "kjak_config", "GODEBUG", "GOTRACEBACK"} {
		fc := FeatherConfig{Type: TypeExternal, Plugin: "pager", Env: []string{name}}
		if err := ValidateFeather(fc); err == nil || !strings.Contains(err.Error(), "never passed to a plugin") {
			t.Errorf("env %s: %v", name, err)
		}
	}
	for _, name := range []string{"HTTPS_PROXY", "LDAP_URI", "DYLDX", "GOPATH", "MY_KJAK_X"} {
		fc := FeatherConfig{Type: TypeExternal, Plugin: "pager", Env: []string{name}}
		if err := ValidateFeather(fc); err != nil {
			t.Errorf("env %s refused: %v", name, err)
		}
	}
}
