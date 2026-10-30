# Tutorials

[日本語](README_ja.md) · [Getting started](../getting-started.md) · [Configuration examples](../examples/README.md) · [Configuration reference](../configuration.md)

Each tutorial takes one feather from nothing to a first notification: what
to set up on the service, a complete `config.toml` to copy, the
command line, the first send, then the options, secrets, troubleshooting and
security notes. The [references](../configuration.md) give every key and
rule; the tutorials show how to get it working.

| feather | tutorial | needs |
|---|---|---|
| `discord` | [Discord](discord.md) | a Discord server where you can create webhooks |
| `slack` | [Slack](slack.md) | a Slack workspace where you can add an app |
| `email` | [Gmail](gmail.md) | a personal Google account with 2-Step Verification (an app password) |
| `email` | [Any SMTP server](smtp.md) | an SMTP server and an account on it |
| `http` | [HTTP APIs](http.md) | an HTTP endpoint (a local test server is enough) |
| `external` | [Plugins](plugin.md) | an executable of your own (the tutorial installs an example one) |

Several at once: [groups](../configuration.md#5-groups) and the
[multi-channel example](../examples/multi-channel.toml).
