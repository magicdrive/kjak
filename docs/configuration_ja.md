# 設定リファレンス

[English](configuration.md) · [導入ガイド](getting-started_ja.md) · [README](../README_ja.md)

- [1. 設定ファイル](#1-設定ファイル)
- [2. CLI による設定の管理](#2-cli-による設定の管理)
- [3. `[defaults]`](#3-defaults)
- [4. Feather](#4-feather)
- [5. Group](#5-group)
- [6. テンプレート](#6-テンプレート)
- [7. 認証情報とセキュリティ](#7-認証情報とセキュリティ)
- [8. 配信の動作](#8-配信の動作)
- [9. 設定例（全体）](#9-設定例全体)
- [10. kjak による設定ファイルの書き換え](#10-kjak-による設定ファイルの書き換え)

## 1. 設定ファイル

kjak は2つの設定ファイルと2つの secrets ファイル（いずれも TOML）を読み込みます。

| ファイル | 場所 | 選択元 | 使われる条件 |
|---|---|---|---|
| グローバル | `--config FILE`、なければ `$KJAK_CONFIG`、なければ `$XDG_CONFIG_HOME/kjak/config.toml`、なければ `~/.config/kjak/config.toml`（macOS も同じ）。Windows で `XDG_CONFIG_HOME` が未設定なら `%AppData%\kjak\config.toml` | explicit（`--config`、`$KJAK_CONFIG`）、なければ user | 存在すれば常に |
| プロジェクト | `./.kjak.toml`（カレントディレクトリのみ。親ディレクトリは探さない） | current | Trust 済みの場合のみ（[7.4](#74-プロジェクト設定の-trust)）。`--no-project` で無視できる |
| 個人の secrets | `--secrets FILE`、なければ `$KJAK_SECRETS`、なければ既定のグローバル設定ファイルと同じディレクトリの `secrets.toml` | explicit（`--secrets`、`$KJAK_SECRETS`）、なければ user | グローバル設定の `secret` 参照、および Trust で許可された名前に限りプロジェクト設定の `secret` 参照（[7.7](#77-secrets-ファイル)） |
| プロジェクトの secrets | `./.kjak.secrets.toml` | current | プロジェクト設定の `secret` 参照のみ。プロジェクト設定と一緒に Trust される。`--no-project` で無視できる |

選択元は3種類です。**explicit**（コマンドラインオプションか環境変数。user のファイルの代わりにそのまま使う）、**user**（自分の設定ディレクトリ）、**current**（カレントディレクトリ。自分が管理していないかもしれないリポジトリなので、Trust した後にだけ使う）。ファイルはそれぞれ独立に選ばれます。`--config` で secrets ファイルの場所は変わらず、`--secrets` で設定ファイルの場所は変わりません。どのファイルがどの選択元で選ばれたか、その状態は `kjak config path` で確認できます（秘密情報は表示しません）。

Trust ストアは、既定のグローバル設定ファイルと同じディレクトリの `trust.toml`（`$XDG_CONFIG_HOME/kjak/trust.toml` など）です。`--config` や `KJAK_CONFIG` を指定しても、Trust ストアの場所は変わりません。

```sh
kjak config path        # kjak が使うファイルと、./.kjak.toml の状態
kjak config init        # グローバル設定ファイルを作成（--force で上書き）
```

`config init` はコメント付きのひな形を書き出します。グローバル設定ファイルがなくてもエラーにはならず、kjak は組み込みの既定値で動作します。

**優先順位**：プロジェクト設定をグローバル設定の上に重ねます。

| 部分 | 規則 |
|---|---|
| `[defaults]` | 項目ごと。プロジェクト側で設定した項目が優先され、それ以外はグローバルの値が残る |
| `[feathers.NAME]` | 名前ごとに**丸ごと**置き換え。グローバル側の項目は1つも残らない |
| `[groups.NAME]` | 名前ごとに丸ごと置き換え |
| `[templates.NAME]` | 名前ごとに丸ごと置き換え |

プロジェクト側のグループから、グローバル側の feather を参照できます。参照の正しさはマージ後に検査します。コマンドラインオプション（`--timeout`、`--level` など）は、どちらのファイルよりも優先されます。

**形式**：両ファイルとも同じ形式で、それぞれ最大 1 MiB です。未知のキーや重複したキーはエラーになるので、打ち間違いが黙って無視されることはありません。`kjak config validate` は両方のファイルを検査し（プロジェクト設定は Trust の有無にかかわらず）、`kjak config show` はマージ後の内容を、リテラルの秘密情報を伏せて表示します。

## 2. CLI による設定の管理

| 操作 | feather | group | テンプレート |
|---|---|---|---|
| 一覧 | `kjak feather list` | `kjak group list` | `kjak template list` |
| 1件表示 | `kjak feather show NAME` | `kjak group show NAME` | `kjak template show NAME` |
| 追加 | `kjak feather add NAME --type T ...` | `kjak group set NAME FEATHER...` | `kjak template set NAME --body ...` |
| 変更 | `kjak feather add NAME ... --force`（feather 全体を置き換え）、`enable` / `disable` | `kjak group set` を再実行（メンバー一覧を置き換え） | `kjak template set` を再実行 |
| 削除 | `kjak feather remove NAME` | `kjak group remove NAME` | `kjak template remove NAME` |
| 試す | `kjak send --to NAME --dry-run "test"`、続けて `--dry-run` なしで送信 | 同左 | `kjak template render NAME --var K=V` |

`list` と `show` は `-o json` に対応しています。feather の一部のキー（たとえばホストだけ）を変更するコマンドはありません。すべてのオプションを付けて `feather add --force` を実行するか、ファイルを直接編集して `kjak config validate` で確認してください。

そのほかの設定（`[defaults]` や、対応するオプションのないキー）は、ファイルを直接編集します。

**スコープ**：`add` / `set` は、`--scope project` を指定しない限りグローバル設定に書き込みます。`remove`、`enable`、`disable` は、`--scope` を指定しなければ、その項目を定義しているファイルを変更します。プロジェクト設定を変更できるのは、Trust 済み（またはまだ存在しない）場合だけです。

**確認方法**：

```sh
kjak config validate          # 両ファイルの構文・キー・参照
kjak config show              # マージ後の設定
kjak send --to NAME --dry-run "test"   # 送信先と認証情報の解決。何も送信しない
```

## 3. `[defaults]`

| キー | 型 | 既定値 | 意味 |
|---|---|---|---|
| `timeout` | 期間の文字列 | `"10s"` | メッセージを読み終えてからの、1回の送信全体の期限。認証情報の解決、再試行、待機をすべて含む（最大 `10m`） |
| `max_retries` | 整数 | `2` | 再試行できる失敗について、最初の試行の後に行う再試行の回数（0〜10） |
| `concurrency` | 整数 | `5` | 同時に配信する feather の数（1〜64） |
| `level` | 文字列 | `"info"` | レベルを指定しないメッセージのレベル |
| `targets` | 名前のリスト | — | `--to` を指定しない送信の送信先（feather と group） |

```toml
[defaults]
timeout = "15s"
level = "info"
targets = ["development"]
```

`kjak send` の `--timeout`、`--max-retries`、`--concurrency`、`--level`、`--to` を指定すると、その送信に限ってこれらを上書きします。

## 4. Feather

feather は1つの通知先です。Slack の Incoming Webhook、Discord の Webhook、SMTP サーバーと宛先の組、または任意の HTTP API です。`kjak feather types` で種類を一覧できます。

**名前**（feather、group、テンプレート共通）：英数字、`_`、`.`、`-`。最大 64 文字で、英数字で始まる必要があります。同じ名前を feather と group の両方に使うことはできません。

**使い方**：`kjak send --to NAME`。feather を直接指定する場合、その feather が有効で、メッセージのレベルを受け付ける必要があります。そうでなければ、何も送信せずに終了します（exit 2）。

### 共通のキー

| キー | 型 | 既定値 | CLI（`feather add`） | 意味 |
|---|---|---|---|---|
| `type` | 文字列 | 必須 | `--type` | `slack`、`discord`、`email`、`http`（OS ネイティブの通知には対応していない） |
| `enabled` | 真偽値 | `true` | `--disabled`、`feather enable` / `disable` | 無効な feather には送信しない |
| `levels` | リスト | すべてのレベル | `--levels L[,L]` | 受け付けるレベル |

種類に属さないキーを書くとエラーになります。

### slack

| キー | CLI | 意味 |
|---|---|---|
| `webhook_url` | `--secret-env`、`--secret-keychain`、`--secret-stdin` | Incoming Webhook の URL（[認証情報](#71-認証情報の参照)） |

```toml
[feathers.dev_slack]
type = "slack"
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }
levels = ["warning", "error", "critical"]
```

メッセージは、テキスト行（`[LEVEL] タイトル: 本文の1行目`）と、レベルに応じた色の添付として投稿されます。添付にはタイトル、本文、タイトルのリンクとして URL が入ります。`&`、`<`、`>` はエスケープされるので、`@channel` やユーザーへのメンションにはなりません。Incoming Webhook は、作成時に選んだチャンネルへ、そのアプリとして投稿します。

### discord

| キー | CLI | 意味 |
|---|---|---|
| `webhook_url` | `--secret-env`、`--secret-keychain`、`--secret-stdin` | Webhook の URL |
| `username` | `--username` | メッセージの表示名（任意） |

```toml
[feathers.ops_discord]
type = "discord"
webhook_url = { keychain = { service = "kjak", account = "ops_discord" } }
username = "kjak"
```

メッセージは、レベルに応じた色の埋め込み（embed）1つとして投稿されます。kjak は Webhook を `?wait=true` 付きで実行し（URL の `thread_id` は維持）、Discord にメッセージの作成を確認させます。メンションは無効です（`allowed_mentions.parse = []`）。4096 文字を超える本文は切り詰められ、末尾が `…` になります。

### email

| キー | CLI | 既定値 | 意味 |
|---|---|---|---|
| `host` | `--host` | 必須 | SMTP サーバー |
| `port` | `--port` | 587 / 465 / 25 | `security` に応じる：`starttls` / `tls` / `none` |
| `security` | `--security` | `"starttls"` | `starttls`（必須。平文に戻らない）、`tls`（最初から TLS）、`none` |
| `user` | `--user` | — | SMTP ユーザー。`password` と組で使う |
| `password` | `--secret-env`、`--secret-keychain`、`--secret-stdin` | — | 認証情報 |
| `from` | `--from` | 必須 | 送信元（アドレスのみ） |
| `to` | `--mail-to` | 必須 | 宛先（アドレスのみ） |
| `subject_prefix` | `--subject-prefix` | — | すべての件名の先頭に付ける文字列 |

```toml
[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
port = 587
security = "starttls"
user = "kjak@example.com"
password = { env = "KJAK_SMTP_PASSWORD" }
from = "kjak@example.com"
to = ["oncall@example.com", "sre@example.com"]
subject_prefix = "[kjak]"
levels = ["error", "critical"]
```

- 証明書を検証します（TLS 1.2 以降）。検証を省略するオプションはありません。
- `user` と `password` は組で指定します。`security = "none"` では指定できません（パスワードが平文で流れるため）。`none` はローカルのリレー向けです。
- 認証は AUTH PLAIN、使えなければ AUTH LOGIN です。
- `from` と `to` はアドレスのみです（`name@example.com`。表示名は不可）。
- 件名は、`subject_prefix` の後にタイトル、タイトルがなければ `[LEVEL] 本文の1行目` です。本文は UTF-8 の quoted-printable テキストで、末尾に URL が付きます。
- 宛先が1つでも拒否されると、メッセージ全体が拒否され、誰にも送信されません。

### http

任意の HTTP API や Webhook に送ります。ガイドは [http.md](http.md)（英語）です。設定ファイルに書きます（`feather add` では作成できません）。

| キー | 型 | 既定値 | 必須 | 検証 | セキュリティ上の意味 | 例 |
|---|---|---|---|---|---|---|
| `url` | 文字列 | — | `url` か `url_secret` のどちらか | 絶対 URL の `https`（`allow_http` があれば `http`）。ホストが必要。ユーザー情報とフラグメントは不可 | `feather show` と `config trust` に表示される。秘密情報には使わない | `"https://api.example.com/v1/alerts"` |
| `url_secret` | 認証情報 | — | `url` か `url_secret` のどちらか | `url` と同じ（解決時に検査） | 表示しない。パスとクエリはエラーから除去 | `{ secret = "chat.webhook_url" }` |
| `method` | 文字列 | `"POST"` | いいえ | `GET`、`POST`、`PUT`、`PATCH`、`DELETE`（大文字） | 再試行の方針を決める | `"PUT"` |
| `headers` | 文字列のテーブル | — | いいえ | ヘッダー名はトークン、値に制御文字なし、最大 64。`Host`、`Content-Length`、`Transfer-Encoding`、`Connection`、`Keep-Alive`、`Upgrade`、`TE`、`Trailer`、`Proxy-*`、`Authorization`、`Cookie`、`Content-Type` は不可 | 値は平文。秘密情報は `[auth]` へ | `{ "X-Team" = "ops" }` |
| `query` | 文字列のテーブル | — | いいえ | キーは空でない | 値は平文。kjak が秘密情報をクエリに入れることはない | `{ source = "kjak" }` |
| `auth.type` | 文字列 | `"none"` | `[auth]` があれば | `none`、`bearer`、`basic`、`header`。その種類のキーのみ | — | `"bearer"` |
| `auth.token` | 認証情報 | — | `bearer` | 認証情報の参照 | `Authorization: Bearer ...` として送る。除去対象 | `{ secret = "api.token" }` |
| `auth.username` | 文字列 | — | `basic` | `:` と改行は不可 | 表示される | `"kjak"` |
| `auth.password` | 認証情報 | — | `basic` | 認証情報の参照 | エンコード後のヘッダー値も除去対象 | `{ env = "API_PASSWORD" }` |
| `auth.header` | 文字列 | — | `header` | ヘッダー名。予約済みのものは不可（ここでは `Authorization` と `Cookie` は可）。`headers` との重複不可 | — | `"X-Api-Key"` |
| `auth.value` | 認証情報 | — | `header` | 改行を含まない認証情報 | 除去対象 | `{ keychain = { service = "kjak", account = "api" } }` |
| `body.format` | 文字列 | `json`（`POST`、`PUT`、`PATCH`）、`none`（`GET`、`DELETE`） | いいえ | `json`、`text`、`none`。`GET` は本文なし | — | `"text"` |
| `body.template` | 文字列 | — | いいえ | `text/template`。`json` では文字列値がテンプレートである JSON 文書。最大 64 KiB | メッセージが JSON テンプレートの構造を変えることはできない。秘密情報はテンプレートから参照できない | `'{"text": "{{.title}}: {{.body}}"}'` |
| `body.content_type` | 文字列 | `application/json` / `text/plain; charset=utf-8` | いいえ | メディアタイプ | — | `"text/markdown"` |
| `timeout` | 期間 | `"10s"` | いいえ | 0 < t <= 10m | — | `"5s"` |
| `success_status` | 文字列のリスト | `["2xx"]` | いいえ | `"NNN"`（100〜599）または `"Nxx"` | — | `["200", "204"]` |
| `max_response_bytes` | 整数 | 65536 | いいえ | 1〜10485760 | 応答が使うメモリを制限する | `4096` |
| `allow_http` | 真偽値 | `false` | いいえ | — | `http://` ではすべてが平文で流れる | `true` |
| `allow_networks` | 文字列のリスト | — | いいえ | CIDR またはアドレス | 範囲内のループバック、リンクローカル、プライベート、IPv6 移行アドレスを許可。メタデータ、未指定、マルチキャスト、予約済みは決して許可しない | `["10.1.0.0/16"]` |
| `allow_hosts` | 文字列のリスト | — | いいえ | ホスト名 | そのホスト名のプライベートアドレスを許可（ループバック、リンクローカルは不可） | `["alerts.corp.example"]` |
| `max_redirects` | 整数 | 0 | いいえ | 0〜10 | リダイレクト先も URL と同じく検査する。オリジンが変わると認証情報を外す | `2` |
| `idempotency_key` | 真偽値 | `false` | いいえ | — | 方針 `safe` で `POST` と `PATCH` を再試行できるようにする | `true` |
| `idempotency_header` | 文字列 | `"Idempotency-Key"` | いいえ | `idempotency_key` と一緒に。予約済みでないヘッダー名 | — | `"X-Request-Id"` |
| `retry.policy` | 文字列 | `"safe"` | いいえ | `safe`、`never`、`always` | `always` では `POST` が二重に届くことがある | `"never"` |
| `retry.max_attempts` | 整数 | 0（上限なし） | いいえ | 0〜11 | — | `3` |

```toml
[feathers.ops_api]
type = "http"
url = "https://api.example.com/v1/alerts"
headers = { "X-Team" = "ops" }

[feathers.ops_api.auth]
type = "bearer"
token = { secret = "ops_api.token" }

[feathers.ops_api.body]
template = '{"summary": "[{{.level}}] {{.title}}", "details": "{{.body}}"}'
```

http feather は環境変数のプロキシを使わず、すべての接続をアドレスポリシーで検査します（[http.md](http.md#the-address-policy-ssrf)）。

## 5. Group

group は feather の一覧に名前を付けたもので、1つの `--to` で複数の通知先へ送れます。

```sh
kjak group set development dev_slack dev_discord      # 作成または置き換え
kjak group set incident dev_slack dev_discord oncall_mail
kjak group show development
kjak group remove development
kjak send --to development "deploy started"
```

```toml
[groups.development]
feathers = ["dev_slack", "dev_discord"]
```

- メンバーは1つ以上で、すべて feather です（group の入れ子はできません）。同じ feather を2回書くことはできません。
- `group set` はメンバー一覧全体を置き換えます。
- 複数の送信先を一度に指定できます：`--to development,oncall_mail` または `--to A --to B`。複数回指定された feather にも、送信は1回だけです。
- **無効**なメンバーや、`levels` がメッセージのレベルを含まないメンバーは**スキップ**されます。`skip` と表示され、失敗には数えません。
- メンバーは互いに独立して配信されます。1つが失敗しても、他のメンバーは止まりません。

一部が失敗した場合：

```
ok    dev_slack (slack) delivered in 280ms (1 attempt)
FAIL  dev_discord (discord) failed (1 attempt): HTTP 404: Unknown Webhook
skip  oncall_mail (email): level_filter (group development)
partial: 1 delivered, 1 failed, 1 skipped
```

結果は `partial` で、終了コードは `1` です。

## 6. テンプレート

```sh
kjak template set deploy --title 'Deployed {{.service}}' \
    --body '{{.service}} {{.version}} is live' --level success
kjak template render deploy --var service=api --var version=1.4.2
kjak send --to development --template deploy --var service=api --var version=1.4.2
```

```toml
[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"              # テンプレートにもできる："{{.level}}"
url = "https://ci.example.com/runs/{{.run}}"
```

`body` は必須です。変数は `--var KEY=VALUE` で渡し、足りない変数があるとエラーになります。使えるのは `text/template` の組み込みアクションだけです。テンプレートファイル（`--template-file FILE`）には、同じキーをトップレベルに書きます。

## 7. 認証情報とセキュリティ

### 7.1 認証情報の参照

feather の秘密情報（Slack や Discord の Webhook URL。URL 自体が認証情報です。または SMTP のパスワード）は、送信時に解決する**参照**として書きます。参照元はちょうど1つです。

| 参照元 | TOML | CLI | 解決方法 |
|---|---|---|---|
| `env` | `{ env = "KJAK_DEV_SLACK_WEBHOOK" }` | `--secret-env VAR` | 環境変数（設定済みで空でないこと） |
| `keychain` | `{ keychain = { service = "kjak", account = "dev_slack" } }` | `--secret-keychain SERVICE/ACCOUNT` | OS のキーチェーン（macOS、Linux） |
| `secret` | `{ secret = "dev_slack.webhook_url" }` | `--secret-ref NAME` | secrets ファイルのエントリー（[7.7](#77-secrets-ファイル)） |
| `literal` | `{ literal = "https://hooks.slack.com/..." }` | `--secret-stdin` | 設定ファイルの値そのもの（**非推奨**、[7.8](#78-literal-非推奨)） |

- 参照元が2つ、または1つもない場合はエラーです。単なる文字列（`webhook_url = "https://..."`）もエラーになります。
- Webhook URL は `https` である必要があります（`http` は `localhost` のみ）。ユーザー情報を含む URL は使えません。
- 解決できない認証情報があると、何も送信せずに終了します（exit 2）。
- `feather add` は秘密情報を引数として受け取りません。参照で指定するか、`--secret-stdin` で標準入力から渡します。

**どれを使うか**

| 状況 | 使うもの |
|---|---|
| 自分の macOS / Linux デスクトップ | `keychain`、または非公開の secrets ファイルと `secret` |
| CI、サーバー、コンテナ、cron、Windows | `env`（各環境のシークレット管理から設定する）、または `--secrets FILE` と `secret` |
| 手元での簡単なテスト | 自分の secrets ファイルと `secret` |

kjak はキーチェーンを読むだけです。エントリーは OS のツールで作成してください。どちらのツールも秘密情報の入力を求めるので、シェルの履歴に残りません。

```sh
security add-generic-password -s kjak -a dev_slack -w                  # macOS
secret-tool store --label "kjak dev_slack" service kjak account dev_slack   # Linux（libsecret-tools）
```

Linux では Secret Service（GNOME Keyring や KWallet のあるデスクトップセッション）が動いている必要があります。ヘッドレスのマシンでは `env` を使ってください。Windows では kjak はキーチェーンに対応していません。

### 7.2 kjak の表示

秘密情報そのものは表示しません。`feather show`、`feather list`、`config show`、`config trust` は、値の代わりに参照元を表示します。

```
webhook_url  env:KJAK_DEV_SLACK_WEBHOOK
webhook_url  keychain:kjak/dev_slack
webhook_url  secret:dev_slack.webhook_url
webhook_url  literal:[REDACTED]
```

エラーやレポートからは、解決したすべての秘密情報（Webhook URL のパスとクエリを含む）を、表示前に取り除きます。サービス側が返したテキストに含まれていた場合も同様です。

### 7.3 ファイルと権限

kjak はファイルをモード `0600`（所有者のみ）で作成します。既存ファイルのモードは維持しますが、グループとその他の書き込み権限は外します。`literal` の秘密情報はファイルに平文で書かれるので、できるだけ `env` か `keychain` を使ってください。リテラルを含むファイルは、共有するバックアップやリポジトリに入れないでください。

### 7.4 プロジェクト設定の Trust

リポジトリの `.kjak.toml` には、環境変数やキーチェーンのエントリーと、その送信先 URL を書けます。つまり、クローンしたリポジトリによって、`kjak send` があなたの秘密情報を別の場所へ送ってしまう可能性があります。そのため kjak は、そのファイルを明示的に Trust するまでプロジェクト設定を使いません。

```sh
kjak config trust
```

```
project configuration /home/me/src/app/.kjak.toml
sha256 3f1c...e9
  feather app_slack: slack, webhook URL from env:APP_SLACK_WEBHOOK
  group app: app_slack, dev_slack
Trust this exact content? [y/N]
```

- Trust の対象は、ファイルの正規化されたパスと内容の SHA-256 で、Trust ストアに保存されます。内容が**少しでも**変わったり、パスが変わったりした場合は、Trust し直す必要があります。
- 同じディレクトリの `./.kjak.secrets.toml` も一緒に Trust されます（パスと SHA-256。値は表示しません）。Trust の後に secrets ファイルが現れた、変わった、なくなった場合は、Trust が取り消されます。
- プロジェクト設定の `secret` 参照のうち、プロジェクトの secrets ファイルにない名前は、名前ごとに許可した場合に限り**あなたの** secrets ファイルから読みます。`config trust` はその名前を一覧表示し、2つ目の質問で許可を求めます（`--yes` のときは `--allow-user-secrets` が必要）。許可していない名前は送信時にエラーになるので、Trust したリポジトリでも、それ以外の秘密情報には届きません。プロジェクトの `http` feather（プロジェクトが選んだ URL に送る）の `env` と `keychain` の参照も、同じく名前ごとに許可が必要です。
- Trust していないプロジェクト設定は、それを使うすべてのコマンドでエラーになります（exit 2）。黙って使われることも、黙って無視されることもありません。

  ```
  kjak: the project configuration is not trusted: /home/me/src/app/.kjak.toml
    review it, then run `kjak config trust` (or pass --no-project to ignore it)
  ```

- 信頼できないリポジトリでは、先に `.kjak.toml` を読むか、`--no-project` を付けて kjak を実行してください。
- 端末がない環境では、`config trust` に `--yes`（上記の名前があれば `--allow-user-secrets` も）が必要です。先にファイルを確認してください。端末がなければ回答を待つことはありません。
- `kjak config untrust` で Trust を取り消せます。状態は `kjak config path` で確認できます（`trusted`、`untrusted`、`untrusted (changed since it was trusted)`、または secrets ファイルによる untrusted）。

### 7.5 秘密情報を Git に入れない

- コミットする `.kjak.toml` には、`env` か `keychain` の参照だけを書き、`literal` は書かないでください。秘密情報は各利用者が自分の環境で用意します。
- Webhook URL は漏れやすい情報です。コミット前に `git diff` を確認し、URL をコミットまたは共有してしまった Webhook は無効化（削除して作り直し）してください。
- 秘密情報は secrets ファイルに置き、リポジトリには入れないでください。`.kjak.secrets.toml` は `.gitignore` に入れてください。

### 7.6 CI で使う場合

- 秘密情報は、CI のシークレット管理から環境変数として渡してください（`env` 参照）。CI ではキーチェーンは使えません。
- CI で `kjak config trust --yes` を実行すると、チェックアウトしたコミットの内容を、プルリクエストによる変更も含めてそのまま Trust します。パイプラインが管理する設定（`--config FILE` または `KJAK_CONFIG`）と `--no-project` を使うか、レビュー済みのコードだけを実行するパイプラインでのみ Trust してください。
- 機械で読める記録を残すには `-o json` を使ってください。秘密情報は含まれません。
- パイプラインが書き出した secrets ファイル（モード 0600、ジョブのユーザーが所有）は `--secrets FILE` か `KJAK_SECRETS` で渡せます。コンテナの secret マウント（Kubernetes、Docker）は多くの場合、他者が読めるか root の所有なので拒否されます。その場合は `env` を使ってください。

### 7.7 secrets ファイル

secrets ファイルは、テーブルの中に文字列の値を並べた TOML です。`secret` 参照はキーのパスで値を指定します。

```toml
# ~/.config/kjak/secrets.toml   （モード 0600）
[dev_slack]
webhook_url = "https://hooks.slack.com/services/..."

[oncall_mail]
password = "..."
```

```toml
# ~/.config/kjak/config.toml
[feathers.dev_slack]
type = "slack"
webhook_url = { secret = "dev_slack.webhook_url" }
```

| | |
|---|---|
| 名前 | ドット区切りのセグメント。各セグメントは英字か数字で始まり、英数字・`_`・`-` が続く（最大 64）。最大 8 セグメント、255 バイト。ドットを含むキーはエラー |
| 値 | 文字列のみ。空の値は使うときにエラー。値はそのまま使い、中身を展開しない（`${...}`、`{ env = ... }`、`{{...}}` はただの文字列） |
| サイズ | 最大 1 MiB。不明な項目や文字列以外の項目はエラーで、キー名だけを示し、値は示さない |
| 権限（Unix） | kjak を実行しているユーザーが所有する通常ファイルで、グループとその他に権限がないこと（`chmod 600`）。そうでなければ解析前に拒否する。検査はパスではなく開いたファイルに対して行う（`fstat`） |
| シンボリックリンク | 個人のファイルはリンクでもよい（dotfiles）。リンク先のファイルを検査する。プロジェクトのファイルはリンクであってはならない（リポジトリがあなたの個人のファイルへリンクできるため）。`O_NOFOLLOW` で開く |
| FIFO、デバイス | 待たずに拒否する（`--secrets <(...)` は使えない。`env` を使う） |
| Windows | 通常ファイルであることのみ検査し、アクセス制御リスト（ACL）は**検査しない**（kjak はまだ ACL を読まない）。`%AppData%` の下のファイルはプロファイルの既定の ACL で本人専用になるので、そこに置くこと。リパースポイントであるプロジェクトの secrets ファイルは拒否する（開く前にパスで検査） |

**参照がどのファイルを読むか**：2つのファイルを、それぞれ独立に読みます。

| 参照がある場所 | 読むファイル |
|---|---|
| グローバル設定（`--config`、`$KJAK_CONFIG`、または user のファイル） | 個人の secrets ファイルのみ（`--secrets`、なければ `$KJAK_SECRETS`、なければ user のファイル）。プロジェクトの secrets ファイルは決して読まないので、リポジトリがあなたの feather の宛先を変えることはできない |
| プロジェクト設定 | プロジェクトの secrets ファイル。なければ、**Trust で許可された名前に限り**個人の secrets ファイル。それ以外の名前はエラー |

個人のファイルは、送信で最初に秘密情報が必要になったときに1回だけ読みます（`kjak config validate` でも検査します）。秘密情報やファイルがない、またはファイルが拒否された場合は、何も送信せずに終了します（exit 2、`error.code: credential`）。

### 7.8 literal（非推奨）

`{ literal = "..." }` は秘密情報を設定ファイルに置くので、共有・コミット・バックアップで漏れやすくなります。引き続き動作しますが、`kjak send` は stderr に警告を出し、`kjak config validate --strict` はエラーにします。`kjak config migrate-secrets` は、ファイルを変更せずに literal を新しい secrets ファイルへ移します。[migration.md](migration.md)（英語）を参照してください。

詳しくは [security.md](security.md)（英語）を参照してください。

## 8. 配信の動作

### 期限とタイムアウト

`timeout`（`--timeout`。既定 10 秒、最大 10 分）は、メッセージを読み終えた時点から始まる**送信全体**の期限です。認証情報の解決（キーチェーンの参照。確認ダイアログを待つことがあります）と、すべての feather のすべての試行、試行の間の待機が、この時刻までに終わります。認証情報の解決中に期限を過ぎた場合は、何も送信せずに終了コード 2（`error.code: deadline`）で終わります。キーチェーンのダイアログにもっと時間が必要なら `--timeout` を延ばしてください。期限を過ぎた時点でまだ空き枠を待っていた feather は、試行せずに失敗します。メッセージの読み込み（標準入力や FIFO）は期限の対象外です。書き込む側を待ち続けますが、SIGINT または SIGTERM で待機を終えられます。

### 再試行

- 一時的と分かっている失敗に限り、最初の試行の後に最大 `max_retries` 回（既定 2 回）再試行します。対象は HTTP 429、408、502/504 以外の 5xx、リクエストを送り終える前のネットワークエラー、SMTP の 4xx です。
- 待機時間は、サービスが `Retry-After` を返せばその値、なければ 0.5 秒、1 秒、2 秒……（最大 5 秒、ゆらぎあり）です。期限内に収まらない待機は行いません。
- 再試行しないもの：HTTP 400/401/403/404 などの拒否、SMTP の 5xx、SMTP のパスワード誤り、証明書の検証失敗。

### 受付状態が不明な場合

リクエスト全体はサービスに届いたのに応答が返ってこなかった場合（接続が切れた、期限を過ぎた、HTTP 502/504）、メッセージは**配信されている可能性があります**。kjak は重複を避けるために再試行せず、受付状態不明の失敗として報告します。

```
????  dev_slack (slack) failed (1 attempt): ... [acceptance unknown: it may have been delivered; not retried]
```

再送する前に、送信先を確認してください。

### キャンセル

Ctrl-C（SIGINT）または SIGTERM（CI ジョブのキャンセル、サービスの停止など）で送信をキャンセルできます。実行中の試行はできるだけ早く終わり、待機中の feather は試行されません。レポートは結果 `canceled` として表示され、kjak は終了コード 130 で終了します。もう一度シグナルを送ると、すぐに終了します。

### 結果と終了コード

| 結果 | 条件 | 終了コード |
|---|---|---|
| `success` | 失敗なし（スキップは可） | 0 |
| `partial` | 一部が配信され、一部が失敗 | 1 |
| `failure` | 1つも配信されず、1つ以上が失敗 | 1 |
| `canceled` | SIGINT または SIGTERM | 130 |
| （エラー） | 送信前の入力・設定・Trust・送信先・認証情報のエラー、または認証情報の解決中の期限切れ。何も送信していない | 2 |

配送の結果と、レポートを出力できたかどうかは別の事実です。`kjak send` のレポートを書き込めなかったとき（ディスクが一杯、読み手がパイプを閉じた）は、stderr に `cannot write the result` と表示されますが、終了コードは配送の結果のままです。この表示がなければ、stdout の JSON は完全です。受付状態が `unknown` の配送は届いている可能性があるので、再送する前に送信先を確認してください。`send` 以外のコマンドは出力そのものが結果なので、書き込めなければ終了コード 2 で失敗します。読み手がパイプを閉じた場合（`| head`）は、Unix でも Windows でも stderr に何も表示しません。

テキスト出力では、送信先ごとに次の印が付きます：`ok` 配信済み、`FAIL` 失敗、`????` 受付状態不明の失敗、`skip` スキップ、`plan`（ドライラン）。`-q` を付けると、すべて配信できたときは何も表示しません。

### JSON 出力

`-o json` は `schema_version: 1` の文書を1つ出力します。内容は `outcome`、`summary`（`total`、`delivered`、`failed`、`unknown`、`skipped`）、送信先ごとの `results`（`status`、`acceptance`、`attempts`、`error`、全試行の `history`）です。送信前に失敗した場合も同じ形で、`outcome: "error"` と `error: {code, message}` を標準出力に、診断メッセージを標準エラー出力に出します。各フィールドの詳細は [cli.md](cli.md#json-output--o-json)（英語）を参照してください。

### エラーの読み方

- 標準エラー出力の `kjak: ...` と終了コード 2：何も送信していません。メッセージに、該当するファイル、キー、feather、環境変数が示されます。
- `FAIL` 行：サービスが拒否した（`HTTP 404: ...`、`SMTP 535 ...`）か、ネットワークが失敗しました。`-o json` ですべての試行を確認できます。
- [troubleshooting.md](troubleshooting.md)（英語）に、メッセージとその対処をまとめています。

### OS ごとの違い

| | macOS | Linux | Windows |
|---|---|---|---|
| `XDG_CONFIG_HOME` 未設定時のグローバル設定ファイル | `~/.config/kjak/config.toml` | `~/.config/kjak/config.toml` | `%AppData%\kjak\config.toml` |
| `keychain` 参照 | 対応（`security`） | 対応（`secret-tool`。Secret Service が必要） | 非対応。`env` を使う |
| 設定ファイルのロック | flock | flock | LockFileEx |

配信の詳細は [delivery.md](delivery.md)（英語）を参照してください。

## 9. 設定例（全体）

```toml
[defaults]
timeout = "15s"
targets = ["development"]

[feathers.dev_slack]
type = "slack"
webhook_url = { env = "KJAK_DEV_SLACK_WEBHOOK" }

[feathers.dev_discord]
type = "discord"
webhook_url = { env = "KJAK_DEV_DISCORD_WEBHOOK" }
levels = ["error", "critical"]

[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
user = "kjak@example.com"
password = { keychain = { service = "kjak", account = "oncall_mail" } }
from = "kjak@example.com"
to = ["oncall@example.com"]
levels = ["critical"]

[groups.development]
feathers = ["dev_slack", "dev_discord"]

[groups.incident]
feathers = ["dev_slack", "dev_discord", "oncall_mail"]

[templates.deploy]
title = "Deployed {{.service}}"
body = "{{.service}} {{.version}} is live"
level = "success"
```

## 10. kjak による設定ファイルの書き換え

`feather add`、`group set`、`template set` などの変更系コマンドは、ファイルを解析して変更し、結果を検査してから（単体でも、もう一方のファイルとマージしても）書き戻します。

- アトミックに書き込みます。同じディレクトリに一時ファイルを作って同期し、元のファイルへリネームします。
- シンボリックリンクをたどります。リンクになっているファイル（dotfiles のリポジトリなど）はリンクのまま残り、リンク先が書き換わります。リンク先が存在しない場合は拒否します。
- ロックを取ります。`<file>.lock` に OS の排他ロックをかけ、別の kjak が書き換え中なら待たずに失敗します（`the configuration is being changed by another kjak process`）。ロックはプロセスの終了時に OS が解放するので、異常終了した kjak が他をブロックすることはありません。`.lock` ファイル自体は残りますが、害はありません。`kjak send` は読み込むだけで、ロックを取りません。
- 変更を検出します。書き込み中に別の手段（ロックを無視するエディターなど）でファイルが変更された場合、kjak は中止し、その変更には手を付けません。

kjak は独自のレイアウトで書き出すため、**コメントとキーの順序は保持されません**。残したい場合は、ファイルを直接編集してください。
