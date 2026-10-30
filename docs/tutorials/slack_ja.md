# Slack

[English](slack.md) · [チュートリアル一覧](README_ja.md) · [設定](../configuration_ja.md)

このチュートリアルでは、Slack App の Incoming Webhook を使って kjak のメッセージを Slack のチャンネルに送ります。App と Webhook の作成、URL を秘密に保つ方法、最初のメッセージ、各種オプションを説明します。

## 1. 概要

`slack` feather は、メッセージを 1 つの Incoming Webhook に投稿します。Incoming Webhook は 1 つの Slack App に属し、作成時に選んだ 1 つのチャンネルに、その App として投稿します。

```toml
[feathers.slack]
type = "slack"
webhook_url = { secret = "slack.webhook_url" }
```

- メッセージは、テキスト 1 行（`[LEVEL] タイトル: 本文の 1 行目`。プッシュ通知と、attachment を表示しないクライアントで使われます）と、level で色分けされた **attachment**（タイトル、本文、タイトルのリンクとしての URL、フッター `kjak · <level>`）です。
- Slack はメッセージを受け付けると HTTP 200 を返します。これが `acceptance: confirmed` です。
- `&`、`<`、`>` はエスケープされるので、本文が何であっても `@channel`、`@here`、ユーザーへの**メンションはできません**。
- **Webhook URL 自体が認証情報です。** URL を知っている人は誰でもそのチャンネルに投稿できます。kjak は設定ファイルに URL を値として書くことを受け付けません。設定に書くのは読み取り先です。
- Slack には `username` はありません。Webhook は App として（App の名前とアイコンで）投稿します。`username` キーは設定エラーです。

## 2. 前提条件

- kjak がインストールされていること（`kjak version`）。[導入ガイド](../getting-started_ja.md#2-インストール)を参照してください。
- App を作成してインストールできる Slack ワークスペース。ワークスペースによっては、App に管理者の承認が必要です。
- メッセージを受け取るチャンネル。最初のテストには、自分だけが見るチャンネルがおすすめです。
- kjak を実行するマシンから `https://hooks.slack.com` に接続できること。

## 3. 準備

App とその Webhook を作ります。Slack の画面は時々変わります。以下の名前が少し違うことがあります。Slack 自身のガイドは [Sending messages using incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/) です。

1. <https://api.slack.com/apps> を開き、**Create New App** → **From scratch**。名前（メッセージの投稿者になります）を付け、ワークスペースを選びます。
2. App の設定で **Incoming Webhooks** を開き、**Activate Incoming Webhooks** をオンにします。
3. **Add New Webhook to Workspace**（新しい画面では **Add New Webhook**）で、チャンネルを選んで **許可する**（Allow）。
4. 追加された行の **Webhook URL** をコピーします。

URL は `https://hooks.slack.com/services/T…/B…/…` の形です。チャット、Issue、コミットするファイルに貼り付けないでください。

URL は **secrets file** に置くことをおすすめします。何かを書き込む前に、非公開（モード 0600）で作成します。

```sh
mkdir -p ~/.config/kjak
[ -e ~/.config/kjak/secrets.toml ] || install -m 600 /dev/null ~/.config/kjak/secrets.toml
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

次の内容を、あなたの URL で書き加えます。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。ダミーの値を置き換える）
[slack]
webhook_url = "https://hooks.slack.com/services/T00000000/B00000000/replace-me"
```

Windows ではファイルは `%AppData%\kjak\secrets.toml` です（プロファイルの既定の権限で非公開になります）。URL のほかの置き場所は 8 章を参照してください。

## 4. 最小構成

設定ファイルは `~/.config/kjak/config.toml` です（Windows では `%AppData%\kjak\config.toml`。`kjak config path` で確認できます）。次のファイルをそのままコピーしてください。

```toml
# ~/.config/kjak/config.toml
[feathers.slack]
type = "slack"
webhook_url = { secret = "slack.webhook_url" }
```

`{ secret = "slack.webhook_url" }` は、secrets file の `[slack]` テーブルの `webhook_url` キーを読みます。両方のファイルを確認します。

```sh
kjak config validate
kjak config path
kjak feather show slack
```

`feather show` は `webhook_url  secret:slack.webhook_url` と表示し、URL は表示しません。

## 5. CLI での設定

同じ feather を kjak に書かせます。

```sh
kjak config init
kjak feather add slack --type slack --secret-ref slack.webhook_url
```

一部の level だけ（ここでは警告と失敗）:

```sh
kjak feather add slack --type slack --secret-ref slack.webhook_url --levels warning,error,critical --force
```

秘密情報は常に参照で渡し、引数に値を書くことはありません。

| オプション | 保存される形 |
|---|---|
| `--secret-ref NAME` | `{ secret = "NAME" }`。secrets file のエントリ |
| `--secret-env VAR` | `{ env = "VAR" }`。環境変数 |
| `--secret-keychain SERVICE/ACCOUNT` | `{ keychain = { service, account } }`（macOS、Linux） |

そのほかのオプション: `--disabled`、`--scope project`、`--force`（同じ名前の feather を置き換える）。`--username` は Discord 専用です。

## 6. 最初の通知

ドライランは送信先と URL を解決するだけで、何も送りません。

```sh
kjak send --to slack --dry-run "Hello from kjak"
```

```
plan  slack (slack)
dry run: 1 target(s), 0 skipped; nothing was sent
```

次にメッセージを送ります。

```sh
kjak send --to slack "Hello from kjak"
kjak send --to slack --title "kjak" --level success "Slack works"
```

```
ok    slack (slack) delivered in 312ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

チャンネルに `[SUCCESS] kjak: Slack works` と緑の attachment が表示されます。kjak は 0 で終了します。

### JSON で結果を見る

```sh
kjak send --to slack -o json "Hello from kjak"
kjak send --to slack -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, attempts, error}'
```

```json
{"status": "delivered", "acceptance": "confirmed", "attempts": 1, "error": null}
```

Webhook が削除されていた場合（終了ステータス 1。ステータスの後の文言は Slack のもの）:

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "attempts": 1,
  "error": {"code": "rejected", "message": "HTTP 404: no_service"}
}
```

`history` には試行ごとの `status_code` が並びます。`acceptance: unknown`（502/504 や応答の消失）は、メッセージが投稿された**かもしれない**という意味です。送り直す前にチャンネルを確認してください。

## 7. 応用的な設定

1 つの Webhook は 1 つのチャンネルに投稿するので、チャンネルが複数なら feather も複数にします。すべてを `#deploys` に、失敗は `#alerts` にも送り、両方の group、defaults、template を持つ完全な設定です。

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["slack_all"]
level = "info"
timeout = "15s"
max_retries = 2

[feathers.slack_deploys]
type = "slack"
webhook_url = { secret = "slack_deploys.webhook_url" }

[feathers.slack_alerts]
type = "slack"
webhook_url = { secret = "slack_alerts.webhook_url" }
levels = ["warning", "error", "critical"]

[groups.slack_all]
feathers = ["slack_deploys", "slack_alerts"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
url = "https://example.com/releases/{{.version}}"

[templates.failure]
title = "{{.job}} failed"
body = "{{.job}} failed on {{.host}}: {{.reason}}"
level = "error"
```

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[slack_deploys]
webhook_url = "https://hooks.slack.com/services/T00000000/B00000001/replace-me"

[slack_alerts]
webhook_url = "https://hooks.slack.com/services/T00000000/B00000002/replace-me"
```

```sh
kjak send --template deploy --var service=api --var version=1.4.2
kjak send --template failure --var job=nightly --var host=ci-1 --var reason=timeout
kjak template render failure --var job=nightly --var host=ci-1 --var reason=timeout
```

deploy のメッセージは `slack_deploys` にだけ届きます。`slack_alerts` は `success` を受け付けないので **skip** されます（`skip  slack_alerts (slack): level_filter (group slack_all)`）。skip されたメンバーはエラーではなく、送信全体は成功します。

### 失敗したコマンドの出力を送る

```sh
make test 2>&1 | tail -n 20 | kjak send --to slack_alerts --stdin --level error --title "tests failed"
```

本文は最大 16384 文字、`--stdin` が読むのは最大 1 MiB です。長い出力は先に切り詰めます（`tail`）。

### レート制限

Slack は Incoming Webhook に制限をかけています（Slack の文書では、Webhook ごとにおおむね 1 秒に 1 メッセージ）。HTTP 429 は、Slack の `Retry-After` の後に、送信の `--timeout` の範囲で再試行されます。

## 8. 秘密情報

| 置き場所 | 設定 | 向いている用途 |
|---|---|---|
| secrets file（`~/.config/kjak/secrets.toml`、モード 0600） | `{ secret = "slack.webhook_url" }` | 自分のマシン（OS を問わない。おすすめ） |
| 環境変数 | `{ env = "KJAK_SLACK_WEBHOOK" }` | CI、サーバー、cron（それぞれのシークレットストアから設定する） |
| OS のキーチェーン（macOS、Linux のデスクトップ） | `{ keychain = { service = "kjak", account = "slack" } }` | 自分のマシン |

**環境変数。** URL はプロンプトで入力すると、シェルの履歴に残りません（`export KJAK_SLACK_WEBHOOK='https://...'` をコマンドとして打つと履歴に残ります）。

```sh
printf 'Slack webhook URL: '; read -rs KJAK_SLACK_WEBHOOK; echo
export KJAK_SLACK_WEBHOOK
kjak feather add slack --type slack --secret-env KJAK_SLACK_WEBHOOK --force
```

PowerShell の場合:

```powershell
$env:KJAK_SLACK_WEBHOOK = Read-Host "Slack webhook URL"
kjak feather add slack --type slack --secret-env KJAK_SLACK_WEBHOOK --force
```

環境変数はそのシェルのセッションの間だけ有効で、kjak を実行する環境で設定されている必要があります。CI では、CI のシークレットをこの変数に割り当てます（GitHub Actions なら step か job に次のように書きます）。

```yaml
env:
  KJAK_SLACK_WEBHOOK: ${{ secrets.SLACK_WEBHOOK }}
```

**macOS のキーチェーン**（末尾の `-w` で、`security` が URL をプロンプトで尋ねます）:

```sh
security add-generic-password -s kjak -a slack -w
kjak feather add slack --type slack --secret-keychain kjak/slack --force
```

Linux では `secret-tool store --label "kjak slack" service kjak account slack` を使います（libsecret-tools とデスクトップセッションが必要）。kjak は Windows のキーチェーンに対応していません。secrets file か環境変数を使ってください。

詳しくは[設定](../configuration_ja.md)の認証情報の参照と secrets file の節を参照してください。

## 9. トラブルシューティング

| 表示 | 原因 | 対処 |
|---|---|---|
| `secret:slack.webhook_url is not in the user secrets file ...`（終了ステータス 2） | テーブルかキーがない、または別のファイルが使われている | `kjak config path` でファイルを確認し、`[slack] webhook_url = "..."` を書く |
| `the secrets file is not private: ...` | 他人が読めるファイル | `chmod 600 ~/.config/kjak/secrets.toml` |
| `environment variable KJAK_SLACK_WEBHOOK is not set`（終了ステータス 2） | その変数のないシェルで `env` 参照を使った | もう一度設定する（8 章） |
| `webhook_url: credential error: the resolved value must be an absolute https URL` / `the resolved value must be https ...` | 値が Slack の示した URL ではない | Webhook URL をもう一度コピーする |
| `slack feathers take no username ...` | `username`（Discord 専用） | 削除する。名前は Slack 側で App の名前を変える |
| `HTTP 404: no_service`（`rejected`） | Webhook が削除された、または App がアンインストールされた | 新しい Webhook を追加し、秘密情報を更新する |
| `HTTP 404: channel_not_found` / `HTTP 410: channel_is_archived` | チャンネルが削除またはアーカイブされた | 別のチャンネルの Webhook を追加する |
| `HTTP 403: action_prohibited`（またはほかの理由） | ワークスペースか管理者が投稿を禁止している | ワークスペースの管理者に相談する |
| `HTTP 400: invalid_payload` | Slack がペイロードを拒否した | メッセージ（長さ、文字列）を確認する。続くようならコマンドラインを添えて kjak に報告する |
| `HTTP 429`（`rate_limited`） | メッセージが多すぎる | kjak は Slack の指定する時間だけ待って再試行する。送る頻度を下げる |
| `????  slack (slack) failed ...: HTTP 504 from a gateway`（`unknown`） | 応答が失われた。メッセージがあるかもしれない | 送り直す前にチャンネルを確認する |
| `feather "slack" does not accept level info`（終了ステータス 2） | `levels` があり、feather を直接指定した | 合う `--level` を指定するか、group に送る |

`HTTP 4xx:` の後の理由は Slack の文言です。理由の一覧は Slack のガイド（3 章）にあります。

```sh
kjak config validate
kjak send --to slack --dry-run "test"
kjak send --to slack -o json "test"
```

## 10. セキュリティ上の注意

- **URL はパスワードです。** 持っている人は誰でも、あなたの App としてそのチャンネルに投稿できます。コミットした、貼り付けた、共有した場合は、App の **Incoming Webhooks** のページでその Webhook を削除し（または App をアンインストールし）、新しく追加して秘密情報を更新してください。
- 設定ファイル、リポジトリ、シェルの履歴に入れないでください。モード 0600 の secrets file、シークレットストアから設定する環境変数、キーチェーンのいずれかを使います。
- kjak は URL を表示しません。`feather show`、`config show`、すべてのエラーは参照先（`secret:slack.webhook_url`）を表示し、レポートからは Webhook URL のパスとクエリを取り除きます。
- Webhook URL は `https` でなければなりません（`http` は `localhost` だけ）。ユーザー情報を含めることはできません。
- **メンションはエスケープされます。** 本文がコミットメッセージやログから来ても、`@channel` で通知されることはありません。
- **プロジェクトのファイル。** リポジトリの `.kjak.toml` は、好きな宛先に投稿する `slack` feather を定義できます。kjak がそれを使うのは `kjak config trust` の後だけで、プロジェクトの `env` と `keychain` の参照には名前ごとにあなたの許可が要ります。[設定](../configuration_ja.md)のプロジェクトの trust の節を参照してください。
- 投稿した内容はチャンネルの全員に見えます。メッセージに秘密情報を入れないでください。

## 11. 関連資料

- [設定](../configuration_ja.md) — slack のすべてのキー、認証情報の参照、secrets file（英語版: [configuration.md](../configuration.md#slack)）
- [CLI](../cli.md) — `kjak send`、`kjak feather add`、JSON のレポート
- [配送](../delivery.md#webhooks-slack-discord) — HTTP ステータスごとの意味、再試行
- [セキュリティ](../security.md)
- [トラブルシューティング](../troubleshooting.md)
- 設定例: [slack-only.toml](../examples/slack-only.toml)、[groups.toml](../examples/groups.toml)、[multi-channel.toml](../examples/multi-channel.toml)
- Slack の導入: [導入ガイド](../getting-started_ja.md)
- ほかのチュートリアル: [Discord](discord_ja.md)、[Gmail](gmail_ja.md)
