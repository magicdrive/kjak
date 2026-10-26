package subcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/textbank"
)

// configMigrateSecrets moves the literal secrets of a configuration file to
// a new secrets file. It never changes an existing file: the secrets go to a
// new file (mode 0600) and the configuration that refers to them goes to a
// new file next to the old one, to be reviewed and moved in place by the
// user. The old file keeps its literals until then, and kjak says so.
func configMigrateSecrets(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseConfigMigrate(args)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	scope := configure.ScopeGlobal
	if opt.Scope != "" {
		scope = configure.Scope(opt.Scope)
	}
	path, secretsPath := p.Global, p.Secrets
	if scope == configure.ScopeProject {
		if p.Project == "" {
			return rt.fail(ctx, errors.New("config migrate-secrets: --no-project leaves no project file"))
		}
		path, secretsPath = p.Project, p.ProjectSecrets
	}
	if opt.SecretsOut != "" {
		secretsPath = opt.SecretsOut
	}
	configOut := opt.ConfigOut
	if configOut == "" {
		configOut = path + ".migrated"
	}

	src, err := configure.ReadFile(path)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if !src.Exists {
		return rt.fail(ctx, fmt.Errorf("no configuration file: %s", path))
	}
	lits := configure.Literals(src.File)
	if len(lits) == 0 {
		fmt.Fprintf(rt.Stdout, "%s holds no literal secret: nothing to migrate\n", path)
		return ExitOK
	}

	values := map[string]string{}
	type move struct{ field, name string }
	var moves []move
	for _, c := range lits {
		// "dev.slack" (a feather name may hold dots) -> "dev_slack"
		name := strings.ReplaceAll(c.Feather, ".", "_") + "." + c.Field
		if err := credential.ValidSecretName(name); err != nil {
			return rt.fail(ctx, fmt.Errorf("feathers.%s.%s: %w", c.Feather, c.Field, err))
		}
		if _, dup := values[name]; dup {
			return rt.fail(ctx, fmt.Errorf("feathers.%s.%s: the secret name %q is taken by another feather; rename one of them first", c.Feather, c.Field, name))
		}
		values[name] = *c.Ref.Literal
		n := name
		c.Set(&credential.Ref{Secret: &n})
		moves = append(moves, move{"feathers." + c.Feather + "." + c.Field, name})
	}
	secretsDoc, err := configure.EncodeSecrets(values)
	if err != nil {
		return rt.fail(ctx, err)
	}
	configDoc, err := configure.Encode(src.File)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if _, err := configure.Parse(configDoc); err != nil {
		return rt.fail(ctx, fmt.Errorf("internal error: the migrated configuration is invalid: %w", err))
	}

	verb := "moved"
	if opt.DryRun {
		verb = "would move"
	}
	for _, m := range moves {
		fmt.Fprintf(rt.Stdout, "%s %s -> { secret = %q }\n", verb, m.field, m.name)
	}
	if opt.DryRun {
		fmt.Fprintf(rt.Stdout, "dry run: nothing written (secrets file %s, configuration %s)\n", secretsPath, configOut)
		return ExitOK
	}
	if err := configure.CreateSecretsFile(secretsPath, secretsDoc); err != nil {
		return rt.fail(ctx, fmt.Errorf("config migrate-secrets: %w (pass --secrets-out NEW_FILE, or move the secrets by hand)", err))
	}
	if err := configure.CreateSecretsFile(configOut, configDoc); err != nil {
		// nothing half done: the secrets file just created goes again
		os.Remove(secretsPath)
		return rt.fail(ctx, fmt.Errorf("config migrate-secrets: %w (pass --config-out NEW_FILE)", err))
	}
	fmt.Fprintf(rt.Stdout, "created %s (mode 0600): %d secret(s)\n", secretsPath, len(values))
	fmt.Fprintf(rt.Stdout, "created %s: the configuration with secret references\n", configOut)
	fmt.Fprintf(rt.Stderr, "kjak: warning: %s still holds the %d literal secret(s); it was not changed.\n", path, len(values))
	fmt.Fprintln(rt.Stdout, "next:")
	fmt.Fprintf(rt.Stdout, "  1. review %s, then move it over %s\n", configOut, path)
	if opt.SecretsOut != "" {
		fmt.Fprintf(rt.Stdout, "  2. give the secrets file to kjak: --secrets %s (or KJAK_SECRETS)\n", secretsPath)
	} else {
		fmt.Fprintln(rt.Stdout, "  2. check: kjak config validate --strict && kjak send --to NAME --dry-run test")
	}
	if scope == configure.ScopeProject {
		fmt.Fprintf(rt.Stdout, "  3. keep %s out of the repository (.gitignore), then run kjak config trust\n", secretsPath)
	}
	fmt.Fprintln(rt.Stdout, "  rotate every secret that was ever committed, shared or backed up in the old file")
	return ExitOK
}
