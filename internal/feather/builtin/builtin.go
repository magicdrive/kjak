// Package builtin builds the feathers of a configuration: slack, discord,
// email, http, and external (a plugin the configuration registers).
package builtin

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strings"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/discord"
	"github.com/magicdrive/kjak/internal/feather/email"
	"github.com/magicdrive/kjak/internal/feather/external"
	"github.com/magicdrive/kjak/internal/feather/httpapi"
	"github.com/magicdrive/kjak/internal/feather/slack"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

// Deps are what the feathers share.
type Deps struct {
	Resolver *credential.Resolver
	// Redactor learns every secret resolved here.
	Redactor   *credential.Redactor
	Clock      clock.Clock
	HTTPClient *http.Client // webhook.NewClient() when nil
	// HTTPAPI are the options of the http feathers (tests: a resolver);
	// each http feather has a client of its own (its address policy).
	HTTPAPI httpapi.Options
	// LookupEnv reads kjak's environment for external feathers (PATH, the
	// variables a plugin may see); GOOS names the plugin file.
	LookupEnv func(string) (string, bool)
	GOOS      string
	// ProjectTrust is set for a feather of the project configuration: its
	// plugin must be the executable the trust recorded.
	ProjectTrust *configure.TrustEntry
}

// Build resolves the credentials of the feather and builds it. An error
// (a credential that does not resolve, a type kjak does not have) is a
// preflight error: nothing is sent.
func Build(ctx context.Context, name string, fc configure.FeatherConfig, d Deps) (feather.Feather, error) {
	if err := configure.ValidateFeather(fc); err != nil {
		return nil, fmt.Errorf("feather %q: %w", name, err)
	}
	httpClient := d.HTTPClient
	if httpClient == nil {
		httpClient = webhook.NewClient()
	}
	// a slack, discord or email feather of a project sends to a
	// destination the project chose: through the address policy of the
	// http feathers (public addresses only, unless allow_networks or
	// allow_hosts say otherwise; never metadata), without a proxy
	guard, err := projectGuard(fc, d)
	if err != nil {
		return nil, fmt.Errorf("feather %q: %w", name, err)
	}
	if guard != nil {
		httpClient = webhook.NewGuardedClient(guard.DialContext)
	}
	resolve := func(field string, ref *credential.Ref) (credential.Secret, error) {
		s, err := d.Resolver.Resolve(ctx, ref)
		if err != nil {
			return s, fmt.Errorf("feather %q: %s: %w", name, field, err)
		}
		if d.Redactor != nil {
			d.Redactor.Add(s)
		}
		return s, nil
	}
	switch fc.Type {
	case configure.TypeSlack:
		u, err := resolve("webhook_url", fc.WebhookURL)
		if err != nil {
			return nil, err
		}
		if err := checkResolvedURL(name, u); err != nil {
			return nil, err
		}
		return &slack.Feather{WebhookURL: u, Poster: &webhook.Poster{Client: httpClient, Clock: d.Clock, Redactor: d.Redactor}}, nil
	case configure.TypeDiscord:
		u, err := resolve("webhook_url", fc.WebhookURL)
		if err != nil {
			return nil, err
		}
		if err := checkResolvedURL(name, u); err != nil {
			return nil, err
		}
		return &discord.Feather{
			WebhookURL: u,
			Username:   fc.Username,
			Poster: &webhook.Poster{
				Client:         httpClient,
				Clock:          d.Clock,
				RetryAfterBody: discord.RetryAfterBody,
				Detail:         discord.Detail,
				Redactor:       d.Redactor,
			},
		}, nil
	case configure.TypeEmail:
		f := &email.Feather{
			Host:          fc.Host,
			Port:          fc.Port,
			Security:      fc.Security,
			User:          fc.User,
			From:          fc.From,
			To:            append([]string(nil), fc.To...),
			SubjectPrefix: fc.SubjectPrefix,
			Clock:         d.Clock,
			Redactor:      d.Redactor,
		}
		if f.Security == "" {
			f.Security = email.SecuritySTARTTLS
		}
		if guard != nil {
			f.Dial = guard.DialContext
		}
		if fc.Password != nil {
			p, err := resolve("password", fc.Password)
			if err != nil {
				return nil, err
			}
			f.Password = p
		}
		return f, nil
	case configure.TypeHTTP:
		return buildHTTP(name, fc, d, resolve)
	case configure.TypeExternal:
		return buildExternal(name, fc, d, resolve)
	}
	return nil, fmt.Errorf("feather %q: unknown type %q", name, fc.Type)
}

// projectGuard is the address policy of a slack, discord or email feather
// of a project (Deps.ProjectTrust set); nil for any other.
func projectGuard(fc configure.FeatherConfig, d Deps) (*httpapi.Guard, error) {
	if d.ProjectTrust == nil {
		return nil, nil
	}
	switch fc.Type {
	case configure.TypeSlack, configure.TypeDiscord, configure.TypeEmail:
	default:
		return nil, nil
	}
	networks, err := configure.ParseAllowNetworks(fc.AllowNetworks)
	if err != nil {
		return nil, err
	}
	return &httpapi.Guard{AllowNetworks: networks, AllowHosts: fc.AllowHosts, Resolver: d.HTTPAPI.Resolver}, nil
}

// buildExternal finds and checks the plugin's executable and resolves the
// secrets the configuration gives it (only those).
func buildExternal(name string, fc configure.FeatherConfig, d Deps, resolve func(string, *credential.Ref) (credential.Secret, error)) (feather.Feather, error) {
	goos := d.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	pathList := ""
	if d.LookupEnv != nil {
		pathList, _ = d.LookupEnv("PATH")
	}
	exe, err := external.Resolve(fc.Plugin, fc.Path, pathList, goos)
	if err != nil {
		return nil, fmt.Errorf("feather %q: %w", name, err)
	}
	if fc.SHA256 != "" && exe.SHA256 != fc.SHA256 {
		return nil, fmt.Errorf("feather %q: %w: %s has SHA-256 %s, the configuration pins %s", name, external.ErrPlugin, exe.Real, exe.SHA256, fc.SHA256)
	}
	rehash := fc.SHA256 != ""
	if d.ProjectTrust != nil {
		tp, ok := d.ProjectTrust.Plugin(fc.Plugin)
		switch {
		case !ok:
			return nil, fmt.Errorf("feather %q: %w: its plugin %s was not part of the trust: review the project, then run `kjak config trust`", name, configure.ErrUntrustedProject, fc.Plugin)
		case tp.Path != exe.Real || tp.SHA256 != exe.SHA256:
			return nil, fmt.Errorf("feather %q: %w: its plugin %s is not the executable that was trusted (%s): review it, then run `kjak config trust`", name, configure.ErrUntrustedProject, fc.Plugin, exe.Real)
		}
		rehash = true
		// a variable a project gives its plugin is one of the user's
		// secrets, possibly: allowed by name like its env references
		for _, e := range fc.Env {
			if !d.ProjectTrust.AllowsUserSecret("env:" + e) {
				return nil, fmt.Errorf("feather %q: %w: the trust of the project does not allow its plugin to get the environment variable %s: review the project, then run `kjak config trust`", name, credential.ErrCredential, e)
			}
		}
	}
	secrets := map[string]credential.Secret{}
	for _, k := range sortedRefKeys(fc.PluginSecrets) {
		s, err := resolve("secrets."+k, fc.PluginSecrets[k])
		if err != nil {
			return nil, err
		}
		secrets[k] = s
	}
	f, err := external.New(name, fc, exe, secrets, external.Options{LookupEnv: d.LookupEnv, Clock: d.Clock, Redactor: d.Redactor})
	if err != nil {
		return nil, fmt.Errorf("feather %q: %w", name, err)
	}
	f.Rehash = rehash
	return f, nil
}

func sortedRefKeys(m map[string]*credential.Ref) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildHTTP resolves the URL and the authentication of an http feather.
func buildHTTP(name string, fc configure.FeatherConfig, d Deps, resolve func(string, *credential.Ref) (credential.Secret, error)) (feather.Feather, error) {
	rawURL := fc.URL
	if fc.URLSecret != nil {
		u, err := resolve("url_secret", fc.URLSecret)
		if err != nil {
			return nil, err
		}
		if _, err := configure.CheckHTTPURL(u.Reveal(), fc.AllowHTTP); err != nil {
			return nil, fmt.Errorf("feather %q: url_secret: %w: the resolved value %v", name, credential.ErrCredential, err)
		}
		rawURL = u.Reveal()
	}
	var auth httpapi.Auth
	if a := fc.Auth; a != nil {
		switch a.Type {
		case configure.AuthBearer:
			t, err := resolve("auth.token", a.Token)
			if err != nil {
				return nil, err
			}
			auth = httpapi.Auth{Header: "Authorization", Value: credential.NewSecret("Bearer " + t.Reveal())}
		case configure.AuthBasic:
			p, err := resolve("auth.password", a.Password)
			if err != nil {
				return nil, err
			}
			auth = httpapi.Auth{Header: "Authorization", Value: httpapi.BasicAuth(a.Username, p)}
			if d.Redactor != nil {
				// an API may echo the encoded header
				d.Redactor.Add(credential.NewSecret(strings.TrimPrefix(auth.Value.Reveal(), "Basic ")))
			}
		case configure.AuthHeader:
			v, err := resolve("auth.value", a.Value)
			if err != nil {
				return nil, err
			}
			auth = httpapi.Auth{Header: a.Header, Value: v}
		}
	}
	// a header value cannot hold a line break (net/http would refuse the
	// request at every attempt): refused before anything is sent
	if strings.ContainsAny(auth.Value.Reveal(), "\r\n\x00") {
		return nil, fmt.Errorf("feather %q: auth: %w: the resolved value contains a line break", name, credential.ErrCredential)
	}
	opt := d.HTTPAPI
	opt.Clock = d.Clock
	opt.Redactor = d.Redactor
	f, err := httpapi.New(name, fc, rawURL, auth, opt)
	if err != nil {
		return nil, fmt.Errorf("feather %q: %w", name, err)
	}
	return f, nil
}

// checkResolvedURL checks a webhook URL from env or keychain, which the
// configuration check could not see. The error does not quote the URL.
func checkResolvedURL(name string, u credential.Secret) error {
	if err := configure.CheckWebhookURL(u.Reveal()); err != nil {
		return fmt.Errorf("feather %q: webhook_url: %w: the resolved value %v", name, credential.ErrCredential, err)
	}
	return nil
}
