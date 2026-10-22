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

//go:embed config_toml/config.toml
var DefaultConfigToml string
