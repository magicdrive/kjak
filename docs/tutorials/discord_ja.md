# Discord

[English](discord.md) · [チュートリアル一覧](README_ja.md) · [設定](../configuration_ja.md)

このチュートリアルでは、チャンネルの Webhook を使って kjak のメッセージを Discord のチャンネルに送ります。Webhook の作成、URL を秘密に保つ方法、最初のメッセージ、各種オプションを説明します。

## 1. 概要

`discord` feather は、メッセージを 1 つの Discord Webhook に投稿します。Webhook は 1 つのサーバーの 1 つのチャンネルに属します。

```toml
[feathers.discord]
type = "discord"
webhook_url = { secret = "discord.webhook_url" }
```

- メッセージは level で色分けされた 1 つの **embed** です。タイトル、本文、タイトルのリンクとしての URL、フッター `kjak · <level>` を持ちます。
- kjak は Webhook を `?wait=true` 付きで実行するので、Discord は作成したメッセージを返します。`acceptance: confirmed` は、Discord がメッセージを作成したという意味です。
- **メンションは無効です**（`allowed_mentions.parse = []`）。メッセージ中の `@everyone`、`@here`、ユーザーへのメンションは文字列のままで、誰にも通知されません。
- **Webhook URL 自体が認証情報です。** URL を知っている人は誰でもそのチャンネルに投稿できます。kjak は設定ファイルに URL を値として書くことを受け付けません。設定に書くのは読み取り先（secrets file、環境変数、OS のキーチェーン）です。

## 2. 前提条件

- kjak がインストールされていること（`kjak version`）。[導入ガイド](../getting-started_ja.md)を参照してください。
- **ウェブフックの管理**（Manage Webhooks）権限を持つ Discord サーバー（オーナーと管理者は持っています）と、メッセージを受け取るテキストチャンネル。最初のテストには、自分だけが見るチャンネルがおすすめです。
- kjak を実行するマシンから `https://discord.com` に接続できること。

## 3. 準備

Discord で Webhook を作ります。画面は時々変わります。お使いの Discord のバージョンや言語によって、以下の名前が少し違うことがあります。

1. サーバーのメニュー → **サーバー設定** → **連携サービス**（Integrations）を開きます（チャンネルの **チャンネルの編集** → **連携サービス** からでもかまいません）。
2. **ウェブフック** → **新しいウェブフック**。
3. できた Webhook をクリックし、名前（メッセージの投稿者として表示されます）、必要ならアバター、**チャンネル**を設定します。
4. **変更を保存する**、続いて **ウェブフック URL をコピー**。

URL は `https://discord.com/api/webhooks/<id>/<token>` の形です。チャット、Issue、コミットするファイルに貼り付けないでください。クリップボードに置くのは次の手順の間だけにします。

URL は **secrets file** に置くことをおすすめします。何かを書き込む前に、非公開（モード 0600）で作成します。

```sh
mkdir -p ~/.config/kjak
install -m 600 /dev/null ~/.config/kjak/secrets.toml   # まだない場合だけ
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

次の内容を、あなたの URL で書き加えます。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。ダミーの値を置き換える）
[discord]
webhook_url = "https://discord.com/api/webhooks/000000000000000000/replace-me"
```

Windows ではファイルは `%AppData%\kjak\secrets.toml` です。kjak は、あなた（と `SYSTEM`、`Administrators`）しか読めないことを確認します。`%AppData%` の下のファイルは既定でそうなっています。URL のほかの置き場所は 8 章を参照してください。

## 4. 最小構成

設定ファイルは `~/.config/kjak/config.toml` です（Windows では `%AppData%\kjak\config.toml`。`kjak config path` で確認できます）。次のファイルをそのままコピーしてください。

```toml
# ~/.config/kjak/config.toml
[feathers.discord]
type = "discord"
webhook_url = { secret = "discord.webhook_url" }
```

`{ secret = "discord.webhook_url" }` は、secrets file の `[discord]` テーブルの `webhook_url` キーを読みます。両方のファイルを確認します。

```sh
kjak config validate
kjak config path
kjak feather show discord
```

`feather show` は `webhook_url  secret:discord.webhook_url` と表示し、URL は表示しません。文字列をそのまま書くこと（`webhook_url = "https://..."`）は設定エラーです。

## 5. CLI での設定

同じ feather を kjak に書かせます。

```sh
kjak config init
kjak feather add discord --type discord --secret-ref discord.webhook_url
```

メッセージの表示名を付ける場合（Webhook の名前の代わりに表示されます。最大 80 文字）:

```sh
kjak feather add discord --type discord --secret-ref discord.webhook_url --username kjak --force
```

秘密情報は常に参照で渡し、引数に値を書くことはありません。

| オプション | 保存される形 |
|---|---|
| `--secret-ref NAME` | `{ secret = "NAME" }`。secrets file のエントリ |
| `--secret-env VAR` | `{ env = "VAR" }`。環境変数 |
| `--secret-keychain SERVICE/ACCOUNT` | `{ keychain = { service, account } }`（macOS、Linux） |

そのほかのオプション: `--levels L[,L]`、`--disabled`、`--scope project`、`--force`（同じ名前の feather を置き換える）。

## 6. 最初の通知

ドライランは送信先と URL を解決するだけで、何も送りません。

```sh
kjak send --to discord --dry-run "Hello from kjak"
```

```
plan  discord (discord)
dry run: 1 target(s), 0 skipped; nothing was sent
```

次にメッセージを送ります。

```sh
kjak send --to discord "Hello from kjak"
kjak send --to discord --title "Deployed" --level success --url https://example.com/releases/1.4.2 "api 1.4.2 is live"
```

```
ok    discord (discord) delivered in 380ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

`ok` は Discord がメッセージを作成したという意味で、kjak は 0 で終了します。embed は level で色分けされます（info は青、success は緑、warning は黄、error は赤、critical は暗い赤）。

### JSON で結果を見る

```sh
kjak send --to discord -o json "Hello from kjak"
kjak send --to discord -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, attempts, error}'
```

```json
{"status": "delivered", "acceptance": "confirmed", "attempts": 1, "error": null}
```

Webhook が削除されていた場合（終了ステータス 1）:

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "attempts": 1,
  "error": {"code": "rejected", "message": "HTTP 404: Unknown Webhook"}
}
```

`history` には試行ごとの `status_code` が並びます。`acceptance: unknown`（502/504 や応答の消失）は、メッセージが投稿された**かもしれない**という意味です。送り直す前にチャンネルを確認してください。

## 7. 応用的な設定

2 つのチャンネルを使う完全な設定です。デプロイの通知は一方に、失敗はもう一方に送り、両方を 1 つの group にまとめ、既定の level と template を持ちます。

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["discord_all"]
level = "info"
timeout = "15s"
max_retries = 2

[feathers.discord_deploys]
type = "discord"
webhook_url = { secret = "discord_deploys.webhook_url" }
username = "deploy bot"
levels = ["info", "success"]

[feathers.discord_alerts]
type = "discord"
webhook_url = { secret = "discord_alerts.webhook_url" }
username = "kjak alerts"
levels = ["warning", "error", "critical"]

[groups.discord_all]
feathers = ["discord_deploys", "discord_alerts"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
url = "https://example.com/releases/{{.version}}"

[templates.failure]
title = "{{.job}} failed"
body = "{{.job}} failed: {{.reason}}"
level = "error"
```

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[discord_deploys]
webhook_url = "https://discord.com/api/webhooks/000000000000000001/replace-me"

[discord_alerts]
webhook_url = "https://discord.com/api/webhooks/000000000000000002/replace-me"
```

```sh
kjak send --template deploy --var service=api --var version=1.4.2
kjak send --template failure --var job=nightly --var reason=timeout
kjak send --to discord_all --level warning "Disk at 85%"
```

どの送信も group `discord_all` に送られます。`levels` がメッセージの level を含まないメンバーは **skip** され（`skip  discord_deploys (discord): level_filter (group discord_all)`）、送信全体は成功します。

### スレッド

スレッドに投稿するには、secrets file の Webhook URL に `?thread_id=<スレッドの ID>` を付けます。kjak は `wait=true` を足すときにこれを残します。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[discord_alerts]
webhook_url = "https://discord.com/api/webhooks/000000000000000002/replace-me?thread_id=000000000000000003"
```

### 上限

kjak はタイトルを 256 文字、本文（embed の description）を 4096 文字に切り詰め、末尾を `…` にします。1 回の送信の本文は最大 16384 文字です。Discord 自身のレート制限には HTTP 429 が返ります。kjak は `Retry-After`（または本文の `retry_after`）だけ待ち、送信の `--timeout` の範囲で再試行します。

## 8. 秘密情報

| 置き場所 | 設定 | 向いている用途 |
|---|---|---|
| secrets file（`~/.config/kjak/secrets.toml`、モード 0600） | `{ secret = "discord.webhook_url" }` | 自分のマシン（OS を問わない。おすすめ） |
| 環境変数 | `{ env = "KJAK_DISCORD_WEBHOOK" }` | CI、サーバー、cron（それぞれのシークレットストアから設定する） |
| OS のキーチェーン（macOS、Linux のデスクトップ） | `{ keychain = { service = "kjak", account = "discord" } }` | 自分のマシン |

**環境変数。** URL はプロンプトで入力すると、シェルの履歴に残りません（`export KJAK_DISCORD_WEBHOOK='https://...'` をコマンドとして打つと履歴に残ります）。

```sh
printf 'Discord webhook URL: '; read -rs KJAK_DISCORD_WEBHOOK; echo
export KJAK_DISCORD_WEBHOOK
kjak feather add discord --type discord --secret-env KJAK_DISCORD_WEBHOOK --force
```

PowerShell の場合（`Read-Host` で入力した値は履歴に残りません）:

```powershell
$env:KJAK_DISCORD_WEBHOOK = Read-Host "Discord webhook URL"
kjak feather add discord --type discord --secret-env KJAK_DISCORD_WEBHOOK --force
```

環境変数はそのシェルのセッションの間だけ有効で、kjak を実行する環境で設定されている必要があります。

**macOS のキーチェーン**（末尾の `-w` で、`security` が URL をプロンプトで尋ねます）:

```sh
security add-generic-password -s kjak -a discord -w
kjak feather add discord --type discord --secret-keychain kjak/discord --force
```

Linux では `secret-tool store --label "kjak discord" service kjak account discord` を使います（libsecret-tools とデスクトップセッションが必要）。kjak は Windows のキーチェーンに対応していません。secrets file か環境変数を使ってください。

`kjak config validate --strict` は、設定ファイルに値として書かれた秘密情報（`{ literal = ... }`、非推奨）を拒否します。詳しくは[設定](../configuration_ja.md)の認証情報の参照と secrets file の節を参照してください。

## 9. トラブルシューティング

| 表示 | 原因 | 対処 |
|---|---|---|
| `secret:discord.webhook_url is not in the user secrets file ...`（終了ステータス 2） | テーブルかキーがない、または別のファイルが使われている | `kjak config path` でファイルを確認し、`[discord] webhook_url = "..."` を書く |
| `the secrets file is not private: group or others have access (mode 0644)` | 他人が読めるファイル | `chmod 600 ~/.config/kjak/secrets.toml` |
| `environment variable KJAK_DISCORD_WEBHOOK is not set`（終了ステータス 2） | その変数のないシェルで `env` 参照を使った | もう一度設定する（8 章） |
| `webhook_url: must be an absolute https URL` / `must be https ...` | 値が Discord の示した URL ではない | URL をもう一度コピーする（**ウェブフック URL をコピー**） |
| `a credential is a table naming one source: ...` | 設定に `webhook_url = "https://..."` と書いた | `webhook_url = { secret = "discord.webhook_url" }` |
| `discord feathers take no email settings ...` | ほかの type のキー | 削除する |
| `HTTP 404: Unknown Webhook`（`rejected`） | Webhook が削除された、または URL が欠けている | 新しい Webhook を作り、秘密情報を更新する |
| `HTTP 401: Invalid Webhook Token` | URL のトークン部分が違う | URL をもう一度コピーする |
| `HTTP 400: ...` | Discord がペイロードを拒否した（`HTTP 400:` の後は Discord の文言） | `username` とメッセージを確認する |
| `HTTP 429`（`rate_limited`） | メッセージが多すぎる | kjak は Discord の指定する時間だけ待って再試行する。諦めるなら `--timeout` を延ばす |
| `????  discord (discord) failed ...: HTTP 504 from a gateway`（`unknown`） | 応答が失われた。メッセージがあるかもしれない | 送り直す前にチャンネルを確認する |
| `TLS certificate verification failed` | プロキシ、または `SSL_CERT_FILE` の CA ファイル | [トラブルシューティング](../troubleshooting.md#results)を参照 |

診断には次を使います。

```sh
kjak config validate
kjak send --to discord --dry-run "test"
kjak send --to discord -o json "test"
```

## 10. セキュリティ上の注意

- **URL はパスワードです。** 持っている人は誰でもその Webhook として投稿できます。コミットした、貼り付けた、共有した場合は、**サーバー設定 → 連携サービス → ウェブフック** でその Webhook を削除して作り直し、秘密情報を更新してください。
- 設定ファイル、リポジトリ、シェルの履歴に入れないでください。モード 0600 の secrets file、シークレットストアから設定する環境変数、キーチェーンのいずれかを使います。
- kjak は URL を表示しません。`feather show`、`config show`、すべてのエラーは参照先（`secret:discord.webhook_url`）を表示し、レポートからは Webhook URL のパスとクエリを取り除きます。
- Webhook URL は `https` でなければなりません（`http` は `localhost` だけ）。ユーザー情報を含めることはできません。
- **メンションは無効です。** 本文がコミットメッセージやログから来ても、`@everyone` で通知されることはありません。
- **プロジェクトのファイル。** リポジトリの `.kjak.toml` は、好きな宛先に投稿する `discord` feather を定義できます。kjak がそれを使うのは `kjak config trust` の後だけで、プロジェクトの feather はパブリックなアドレスにしか接続しません。[設定](../configuration_ja.md)のプロジェクトの trust の節を参照してください。
- 投稿した内容はチャンネルの全員に見えます。メッセージに秘密情報を入れないでください。

## 11. 関連資料

- [設定](../configuration_ja.md) — discord のすべてのキー、認証情報の参照、secrets file（英語版: [configuration.md](../configuration.md#discord)）
- [CLI](../cli.md) — `kjak send`、`kjak feather add`、JSON のレポート
- [配送](../delivery.md#webhooks-slack-discord) — HTTP ステータスごとの意味、再試行
- [セキュリティ](../security.md)
- [トラブルシューティング](../troubleshooting.md)
- 設定例: [discord-only.toml](../examples/discord-only.toml)、[desktop-and-discord.toml](../examples/desktop-and-discord.toml)、[multi-channel.toml](../examples/multi-channel.toml)
- Discord のガイド: [Intro to Webhooks](https://support.discord.com/hc/en-us/articles/228383668-Intro-to-Webhooks)
- ほかのチュートリアル: [OS の通知](os_ja.md)、[Slack](slack_ja.md)、[Gmail](gmail_ja.md)
