package configure

import "github.com/magicdrive/kjak/internal/credential"

// Merge returns the configuration of global overlaid with project:
//   - defaults: field by field (a field project sets wins);
//   - feathers, groups, templates: by name, a project entry replaces the
//     global entry of its name whole (no field of the global entry remains).
//
// Neither argument is changed; either may be nil.
func Merge(global, project *File) *File {
	out := &File{
		Feathers:  map[string]FeatherConfig{},
		Groups:    map[string]GroupConfig{},
		Templates: map[string]TemplateConfig{},
	}
	for _, f := range []*File{global, project} {
		if f == nil {
			continue
		}
		mergeDefaults(&out.Defaults, f.Defaults)
		for k, v := range f.Feathers {
			out.Feathers[k] = cloneFeather(v)
		}
		for k, v := range f.Groups {
			out.Groups[k] = GroupConfig{Feathers: append([]string(nil), v.Feathers...)}
		}
		for k, v := range f.Templates {
			out.Templates[k] = v
		}
	}
	return out
}

func mergeDefaults(dst *Defaults, src Defaults) {
	if src.Timeout != nil {
		v := *src.Timeout
		dst.Timeout = &v
	}
	if src.MaxRetries != nil {
		v := *src.MaxRetries
		dst.MaxRetries = &v
	}
	if src.Concurrency != nil {
		v := *src.Concurrency
		dst.Concurrency = &v
	}
	if src.Level != nil {
		v := *src.Level
		dst.Level = &v
	}
	if src.Targets != nil {
		dst.Targets = append([]string(nil), src.Targets...)
	}
}

func cloneFeather(f FeatherConfig) FeatherConfig {
	if f.Enabled != nil {
		v := *f.Enabled
		f.Enabled = &v
	}
	f.Levels = append([]string(nil), f.Levels...)
	f.To = append([]string(nil), f.To...)
	f.Headers = cloneMap(f.Headers)
	f.Query = cloneMap(f.Query)
	f.SuccessStatus = append([]string(nil), f.SuccessStatus...)
	f.AllowNetworks = append([]string(nil), f.AllowNetworks...)
	f.AllowHosts = append([]string(nil), f.AllowHosts...)
	if f.Auth != nil {
		a := *f.Auth
		f.Auth = &a
	}
	if f.Body != nil {
		b := *f.Body
		f.Body = &b
	}
	if f.Retry != nil {
		r := *f.Retry
		f.Retry = &r
	}
	if f.Timeout != nil {
		d := *f.Timeout
		f.Timeout = &d
	}
	f.Settings = cloneMap(f.Settings)
	f.Env = append([]string(nil), f.Env...)
	if f.PluginSecrets != nil {
		m := make(map[string]*credential.Ref, len(f.PluginSecrets))
		for k, v := range f.PluginSecrets {
			if v != nil {
				c := *v
				v = &c
			}
			m[k] = v
		}
		f.PluginSecrets = m
	}
	return f
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
