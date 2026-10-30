# Configuration examples

[Tutorials](../tutorials/README.md) · [Configuration reference](../configuration.md)

Complete `config.toml` files to copy (`~/.config/kjak/config.toml`, or
`--config FILE`). Each is checked in CI: it parses, it is valid, and it holds
no secret value (secrets are references: `env`, `secret`, `keychain`).

| file | what |
|---|---|
| [desktop-only.toml](desktop-only.toml) | the OS notification of this machine |
| [discord-only.toml](discord-only.toml) | a Discord webhook |
| [slack-only.toml](slack-only.toml) | a Slack incoming webhook, for warnings and errors |
| [gmail-only.toml](gmail-only.toml) | Gmail with an app password |
| [smtp-only.toml](smtp-only.toml) | any SMTP server (implicit TLS, port 465) |
| [http-only.toml](http-only.toml) | an HTTP API with a bearer token and a JSON body |
| [plugin-only.toml](plugin-only.toml) | a plugin, [kjak-feather-log](plugins/kjak-feather-log) |
| [desktop-and-discord.toml](desktop-and-discord.toml) | the desktop and Discord, one send |
| [multi-channel.toml](multi-channel.toml) | every kind of destination, groups by audience |
| [development.toml](development.toml) | a developer's machine: desktop, failures to Discord, a template |
| [secrets-file.toml](secrets-file.toml) | secrets in a secrets file; [secrets.example.toml](secrets.example.toml) is that file |
| [groups.toml](groups.toml) | groups and level filters |
| [templates.toml](templates.toml) | message templates |

`kjak config init` writes a commented catalog of the same examples;
`kjak config init --minimal` a file without them.
