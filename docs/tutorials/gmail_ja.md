# Gmail

[English](gmail.md) · [チュートリアル一覧](README_ja.md) · [設定](../configuration_ja.md) · [任意の SMTP サーバー](smtp_ja.md)

このチュートリアルでは、`email` feather と Google の**アプリ パスワード**（App Password）を使い、Gmail の SMTP サーバー経由で kjak のメッセージをメールとして送ります。使えるアカウントの条件、アプリ パスワードの作成、秘密に保つ方法、最初のメッセージ、各種オプションを説明します。

## 1. 概要

`email` feather は 1 つのサーバーと SMTP で通信し、決まった宛先の一覧にメッセージを送ります。Gmail の場合は次のとおりです。

```toml
[feathers.gmail]
type = "email"
host = "smtp.gmail.com"
port = 587
security = "starttls"
user = "you@gmail.com"
password = { secret = "gmail.password" }
from = "you@gmail.com"
to = ["you@gmail.com"]
```

- パスワードは**アプリ パスワード**です。Google が 1 つのアプリのために発行する 16 桁のパスコードです。**Google アカウントのパスワードは kjak に使わないでください。** ファイルにも書かないでください。
- kjak は TLS の上でユーザー名とパスワードで認証します（SMTP AUTH PLAIN か LOGIN）。**kjak は OAuth を実装していません。**
- 件名はメッセージのタイトル（なければ `[LEVEL] 本文の 1 行目`）で、前に `subject_prefix` が付きます。本文はプレーンテキスト（UTF-8）で、後にメッセージの URL が続きます。ヘッダー `X-Kjak-Level` に level が入ります。
- `acceptance: confirmed` は、Gmail のサーバーがメッセージを受け付けた（データの後に 2xx の応答があった）という意味で、受信トレイに届いたという意味ではありません。

### このチュートリアルを使えるアカウント

アプリ パスワードは、すべての Google アカウントで使えるわけではありません。始める前に確認してください。

| アカウント | アプリ パスワード |
|---|---|
| 2 段階認証プロセスが有効な個人の Google アカウント（`@gmail.com`） | 使える — このチュートリアルの対象 |
| 2 段階認証プロセスが有効でない | 有効にするまで使えない |
| 2 段階認証プロセスが**セキュリティ キーのみ** | アプリ パスワードの項目が表示されない |
| 職場や学校など、組織のアカウント | 項目が表示されない |
| **高度な保護機能**（Advanced Protection）が有効 | 項目が表示されない |
| **Google Workspace** | 2025-05-01 以降、Google はこの種の接続にユーザー名とパスワードではなく OAuth を求めます。kjak は OAuth に対応していないので、Workspace のアカウントでは kjak から Gmail 経由で送れません |

最初の行に当てはまらない場合は、TLS の上でパスワード認証を受け付ける別の SMTP サーバーを使ってください。[SMTP のチュートリアル](smtp_ja.md)を参照してください。これらの規則は Google が変えることがあります。基準になるのは Google のアプリ パスワードのヘルプです（11 章）。

## 2. 前提条件

- kjak がインストールされていること（`kjak version`）。[導入ガイド](../getting-started_ja.md)を参照してください。
- **2 段階認証プロセス**が有効な個人の Google アカウント。セキュリティ キーのみではなく、高度な保護機能も使っていないこと（1 章）。
- kjak を実行するマシンから `smtp.gmail.com` のポート 587（または 465）に接続できること。ネットワークによっては、外向きの SMTP のポートが塞がれています。

## 3. 準備

### 3.1 アプリ パスワードを作る

1. メッセージを送る Google アカウントにログインします。
2. 2 段階認証プロセスが無効なら、Google アカウントの **セキュリティ** のページで有効にします。
3. <https://myaccount.google.com/apppasswords> を開きます。
4. アプリの名前（たとえば `kjak`）を入力して作成します。
5. Google が 16 桁のアプリ パスワードを一度だけ表示します。すぐに secrets file にコピーしてください（3.2）。後から見直すことはできません（新しく作ることはできます）。

設定が使えないという表示が出る場合は、アカウントが 1 章のいずれかに当てはまります。

### 3.2 secrets file に置く

何かを書き込む前に、secrets file を非公開（モード 0600）で作成し、エディタで開きます。

```sh
mkdir -p ~/.config/kjak
install -m 600 /dev/null ~/.config/kjak/secrets.toml   # まだない場合だけ
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

アプリ パスワードを書き加えます。Google は 4 文字ずつ 4 つに区切って表示します。空白を除いた 16 文字を書いてください。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。ダミーの値を置き換える）
[gmail]
password = "abcdefghijklmnop"
```

Windows ではファイルは `%AppData%\kjak\secrets.toml` です（プロファイルの既定の権限で非公開になります）。パスワードのほかの置き場所は 8 章を参照してください。

## 4. 最小構成

設定ファイルは `~/.config/kjak/config.toml` です（Windows では `%AppData%\kjak\config.toml`。`kjak config path` で確認できます）。次のファイルをそのままコピーし、`you@gmail.com` を自分のアドレスに置き換えてください（3 か所）。

```toml
# ~/.config/kjak/config.toml
[feathers.gmail]
type = "email"
host = "smtp.gmail.com"
port = 587
security = "starttls"
user = "you@gmail.com"
password = { secret = "gmail.password" }
from = "you@gmail.com"
to = ["you@gmail.com"]
subject_prefix = "[kjak]"
```

| キー | Gmail での値 |
|---|---|
| `host` | `smtp.gmail.com` |
| `port`、`security` | `587` と `"starttls"`（このチュートリアル）、または `465` と `"tls"`（最初から TLS） |
| `user` | Gmail のアドレス全体 |
| `password` | アプリ パスワードへの参照 |
| `from` | 自分の Gmail のアドレス（アドレスだけ。表示名は付けない） |
| `to` | 宛先。アドレスだけを 1 つ以上 |

`starttls` では、kjak はサーバーが STARTTLS を提供することを求め、平文に切り替えることはありません。両方のファイルを確認します。

```sh
kjak config validate
kjak feather show gmail
```

`feather show` は `password  secret:gmail.password` と表示し、パスワードは表示しません。

## 5. CLI での設定

同じ feather を kjak に書かせます。

```sh
kjak config init
kjak feather add gmail --type email --host smtp.gmail.com --port 587 --security starttls --user you@gmail.com --secret-ref gmail.password --from you@gmail.com --mail-to you@gmail.com --subject-prefix "[kjak]"
```

宛先が複数なら `--mail-to a@example.com,b@example.com` とするか、オプションを繰り返します。ポート 465 で最初から TLS を使う場合:

```sh
kjak feather add gmail --type email --host smtp.gmail.com --port 465 --security tls --user you@gmail.com --secret-ref gmail.password --from you@gmail.com --mail-to you@gmail.com --force
```

パスワードは常に参照で渡し、引数に値を書くことはありません。`--secret-ref NAME`（secrets file）、`--secret-env VAR`、`--secret-keychain SERVICE/ACCOUNT`（macOS、Linux）のいずれかです。`--user` とパスワードの参照はセットで指定します。

## 6. 最初の通知

ドライランは送信先を解決し、secrets file からアプリ パスワードを読むだけで、何も送りません（Gmail にも接続しません）。

```sh
kjak send --to gmail --dry-run "Hello from kjak"
```

```
plan  gmail (email)
dry run: 1 target(s), 0 skipped; nothing was sent
```

次にメールを送ります。

```sh
kjak send --to gmail --title "Hello" "Hello from kjak"
kjak send --to gmail --level error --title "Backup failed" --url https://example.com/runs/42 "The nightly backup stopped"
```

```
ok    gmail (email) delivered in 1.9s (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

最初のメールは件名 `[kjak] Hello` で届きます。`--title` がなければ、件名は `[kjak] [INFO] Hello from kjak` です。

### JSON で結果を見る

```sh
kjak send --to gmail -o json --title "Hello" "Hello from kjak"
kjak send --to gmail -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, attempts, error}'
```

```json
{"status": "delivered", "acceptance": "confirmed", "attempts": 1, "error": null}
```

アプリ パスワードが拒否された場合（終了ステータス 1。アカウントがロックされないよう、kjak は再試行しません）:

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "attempts": 1,
  "error": {"code": "rejected", "message": "AUTH: SMTP 535 authentication failed"}
}
```

`history` には試行ごとの SMTP のコード（`status_code`）が並びます。`acceptance: unknown`（データを送った後に接続が切れた）は、メールが送られた**かもしれない**という意味です。送り直す前に、メールボックス（と送信者の **送信済み**）を確認してください。

## 7. 応用的な設定

完全な設定の例です。失敗は 2 人にメールで、すべてのメッセージはデスクトップに出し、両方を 1 つの group にまとめ、SMTP のために送信の期限を長めにし、template を持ちます。

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["notify"]
level = "info"
timeout = "30s"
max_retries = 2

[feathers.desktop]
type = "os"

[feathers.gmail_alerts]
type = "email"
host = "smtp.gmail.com"
port = 465
security = "tls"
user = "you@gmail.com"
password = { secret = "gmail.password" }
from = "you@gmail.com"
to = ["you@gmail.com", "teammate@example.com"]
subject_prefix = "[kjak alert]"
levels = ["error", "critical"]

[groups.notify]
feathers = ["desktop", "gmail_alerts"]

[templates.failure]
title = "{{.job}} failed on {{.host}}"
body = "{{.job}} failed on {{.host}}: {{.reason}}"
level = "error"
url = "https://example.com/jobs/{{.job}}"
```

```sh
kjak send "Nightly job done"
kjak send --template failure --var job=nightly --var host=build-1 --var reason=timeout
kjak send --to gmail_alerts --level critical --title "Disk full" "/var is at 100%"
```

最初の送信はデスクトップにだけ表示されます。`gmail_alerts` は `info` を受け付けないので skip され（`skip  gmail_alerts (email): level_filter (group notify)`）、これはエラーではありません。template の送信は、メールとデスクトップの通知の両方になります。

### 補足

- **期限。** SMTP のセッションは Webhook より時間がかかります（接続、TLS、認証）。`timeout = "30s"` にすると、再試行の余裕ができます。
- **再試行。** 一時的な拒否（SMTP 4xx、データの前のネットワークエラー）は再試行されます。5xx とログインの拒否は再試行されません。
- **差出人（from）。** 自分の Gmail のアドレスを使ってください。アカウントに追加したアドレス（「他のアドレスからメールを送信」）でなければ、Gmail が別の `from` をアカウントのアドレスに置き換えることがあります。
- **振り分け。** 件名（`[kjak alert]`）で Gmail のフィルタを作れば、ラベルやスターを付けられます。
- **Google の制限。** Google は 1 つのアカウントが 1 日に送れるメッセージの数を制限しています（Google Workspace では SMTP 経由で 1 日 2,000 通）。これは kjak ではなく Google の制限です。kjak は数えず、上限を超えたメッセージは Gmail が拒否します（`rejected`）。通知の数が多いなら、チャットの Webhook（[Slack](slack_ja.md)、[Discord](discord_ja.md)）を検討してください。

## 8. 秘密情報

| 置き場所 | 設定 | 向いている用途 |
|---|---|---|
| secrets file（`~/.config/kjak/secrets.toml`、モード 0600） | `{ secret = "gmail.password" }` | 自分のマシン（OS を問わない。おすすめ） |
| 環境変数 | `{ env = "KJAK_GMAIL_PASSWORD" }` | CI、サーバー、cron（それぞれのシークレットストアから設定する） |
| OS のキーチェーン（macOS、Linux のデスクトップ） | `{ keychain = { service = "kjak", account = "gmail" } }` | 自分のマシン |

**環境変数。** アプリ パスワードはプロンプトで入力すると、シェルの履歴に残りません（`export KJAK_GMAIL_PASSWORD=...` をコマンドとして打つと履歴に残ります）。

```sh
printf 'Gmail app password: '; read -rs KJAK_GMAIL_PASSWORD; echo
export KJAK_GMAIL_PASSWORD
kjak feather add gmail --type email --host smtp.gmail.com --user you@gmail.com --secret-env KJAK_GMAIL_PASSWORD --from you@gmail.com --mail-to you@gmail.com --force
```

PowerShell の場合:

```powershell
$env:KJAK_GMAIL_PASSWORD = Read-Host "Gmail app password"
kjak feather add gmail --type email --host smtp.gmail.com --user you@gmail.com --secret-env KJAK_GMAIL_PASSWORD --from you@gmail.com --mail-to you@gmail.com --force
```

**macOS のキーチェーン**（末尾の `-w` で、`security` がプロンプトで尋ねます）:

```sh
security add-generic-password -s kjak -a gmail -w
kjak feather add gmail --type email --host smtp.gmail.com --user you@gmail.com --secret-keychain kjak/gmail --from you@gmail.com --mail-to you@gmail.com --force
```

Linux では `secret-tool store --label "kjak gmail" service kjak account gmail` を使います（libsecret-tools とデスクトップセッションが必要）。kjak は Windows のキーチェーンに対応していません。secrets file か環境変数を使ってください。

詳しくは[設定](../configuration_ja.md)の認証情報の参照と secrets file の節を参照してください。

## 9. トラブルシューティング

| 表示 | 原因 | 対処 |
|---|---|---|
| **アプリ パスワード** のページがない、またはその設定がアカウントで利用できないという表示 | 2 段階認証プロセスが無効、セキュリティ キーのみ、組織のアカウント、高度な保護機能、Google Workspace | 1 章を参照。別のサーバーで [SMTP のチュートリアル](smtp_ja.md)を使う |
| `AUTH: SMTP 535 authentication failed`（`rejected`、再試行なし） | `user` がアドレス全体でない、アプリ パスワードが違う（空白、打ち間違い、取り消し済み）、アプリ パスワードではない | `user` を確認する。新しいアプリ パスワードを作り、secrets file の値を置き換える |
| `AUTH: SMTP 534 authentication failed` | Gmail がアプリ パスワードを求めている（たとえばアカウントのパスワードを使った） | アプリ パスワードを作る（3 章） |
| `secret:gmail.password is not in the user secrets file ...`（終了ステータス 2） | テーブルかキーがない、または別のファイルが使われている | `kjak config path` で確認し、`[gmail] password = "..."` を書く |
| `the secrets file is not private: ...` | 他人が読めるファイル | `chmod 600 ~/.config/kjak/secrets.toml` |
| `user and password go together` | `user` だけ、または `password` だけ | 両方を設定する |
| `security = "none" sends the password in clear text ...` | パスワードと `security = "none"` | `starttls`（587）か `tls`（465） |
| `the server does not offer STARTTLS (set security = "tls" for implicit TLS)` | STARTTLS を提供しないポートで `starttls` を使った | 587 には `starttls`、465 には `tls` |
| `connect` や `TLS handshake` のネットワークエラー、タイムアウト | ネットワークがポートを塞いでいる、またはポートと security が合っていない（465 に `starttls`、587 に `tls`） | もう一方の組み合わせを試す。ファイアウォールやプロキシを確認する |
| `TLS certificate verification failed` | TLS を検査するプロキシ、または `SSL_CERT_FILE` | [トラブルシューティング](../troubleshooting.md#results)を参照 |
| データの後の SMTP 5xx（`rejected`）。たとえば送信数の上限 | Gmail がメッセージを拒否した | エラーに含まれる応答を読む。Google の上限がリセットされるのを待つ |
| `ok` なのにメールがない | フィルタされた、または **迷惑メール** に入った。受信側のサーバーが遅れている | メールボックスを検索する（送信者の **送信済み** も） |

Google のヘルプによれば、アカウントのパスワードを変更すると、そのアカウントのアプリ パスワードは取り消されます。パスワードを変えた後は、アプリ パスワードを作り直してください。

```sh
kjak config validate
kjak send --to gmail --dry-run "test"
kjak send --to gmail -o json "test"
```

## 10. セキュリティ上の注意

- **Google のパスワードは使わない。** kjak に必要なのはアプリ パスワードです。アカウントのパスワードを、設定ファイル、secrets file、環境変数、コマンドラインのどこにも入れないでください。
- **アプリ パスワードはメールボックスのパスワードです。** 持っている人は、あなたのアカウントのメールを使えます。モード 0600 の secrets file（またはシークレットストア）に置き、リポジトリ、共有するバックアップ、シェルの履歴に入れないでください。そのマシンで kjak を使わなくなったとき、漏れた可能性があるときは、<https://myaccount.google.com/apppasswords> で取り消してください。マシンごとに 1 つずつ作っておけば、1 つだけを取り消せます。
- kjak はパスワードを表示しません。`feather show`、`config show`、すべてのエラーは参照先（`secret:gmail.password`）を表示します。拒否されたログインの応答の文言はエラーに含めません（サーバーがエンコードされたパスワードを繰り返すことがあるためです）。
- **TLS だけ。** `starttls` は平文に切り替えず、`tls` は最初のバイトから TLS で、サーバーの証明書は常に検証されます。`security = "none"` ではパスワードは拒否されます。
- **宛先の人は送った内容を見ます。** メッセージに秘密情報を入れないでください。他の人へのメールは、あなたの Gmail のアドレスから送られます。
- **プロジェクトのファイル。** リポジトリの `.kjak.toml` は、任意のアドレスに送る `email` feather を定義できます。kjak がそれを使うのは `kjak config trust` の後だけで、プロジェクトがあなたの秘密情報を読めるのは、あなたが許可した名前だけです。[設定](../configuration_ja.md)のプロジェクトの trust の節を参照してください。

## 11. 関連資料

- [設定](../configuration_ja.md) — email のすべてのキー、認証情報の参照、secrets file（英語版: [configuration.md](../configuration.md#email)）
- [SMTP のチュートリアル](smtp_ja.md) — ほかの SMTP サーバー
- [CLI](../cli.md) — `kjak send`、`kjak feather add`、JSON のレポート
- [配送: メール](../delivery.md#e-mail-smtp) — SMTP の段階と応答ごとの意味、再試行
- [セキュリティ](../security.md)
- [トラブルシューティング](../troubleshooting.md)
- 設定例: [gmail-only.toml](../examples/gmail-only.toml)
- Google: [アプリ パスワードでログインする](https://support.google.com/accounts/answer/185833?hl=ja)、[2 段階認証プロセス](https://support.google.com/accounts/answer/185839?hl=ja)
- ほかのチュートリアル: [OS の通知](os_ja.md)、[Discord](discord_ja.md)、[Slack](slack_ja.md)
