// Package textbank holds kjak's help texts and file templates.
package textbank

import (
	_ "embed"
)

//go:embed help.txt
var HelpMessage string

//go:embed short_help.txt
var ShortHelpMessage string

//go:embed individual_help/kjak_send_help.txt
var SendHelp string

//go:embed individual_help/kjak_feather_help.txt
var FeatherHelp string

//go:embed individual_help/kjak_group_help.txt
var GroupHelp string

//go:embed individual_help/kjak_config_help.txt
var ConfigHelp string

//go:embed individual_help/kjak_template_help.txt
var TemplateHelp string

// DefaultConfigToml is the file `kjak config init` writes: no feather, and a
// commented example of every feather and setting.
//
//go:embed config_toml/config.toml
var DefaultConfigToml string

// MinimalConfigToml is the file `kjak config init --minimal` writes: the
// same configuration as DefaultConfigToml, without the examples.
//
//go:embed config_toml/minimal.toml
var MinimalConfigToml string
