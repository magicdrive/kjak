// Package builtin builds the feathers of a configuration. Every feather is
// built in: kjak has no plugin mechanism.
package builtin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/discord"
	"github.com/magicdrive/kjak/internal/feather/email"
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
		return &slack.Feather{WebhookURL: u, Poster: &webhook.Poster{Client: httpClient, Clock: d.Clock}}, nil
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
		}
		if f.Security == "" {
			f.Security = email.SecuritySTARTTLS
		}
		if fc.Password != nil {
			p, err := resolve("password", fc.Password)
			if err != nil {
				return nil, err
			}
			f.Password = p
		}
		return f, nil
	}
	return nil, fmt.Errorf("feather %q: unknown type %q", name, fc.Type)
}

// checkResolvedURL checks a webhook URL from env or keychain, which the
// configuration check could not see. The error does not quote the URL.
func checkResolvedURL(name string, u credential.Secret) error {
	if err := configure.CheckWebhookURL(u.Reveal()); err != nil {
		return fmt.Errorf("feather %q: webhook_url: %w: the resolved value %v", name, credential.ErrCredential, err)
	}
	return nil
}
