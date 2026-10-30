# 任意の SMTP サーバーでメールを送る

[English](smtp.md) · [チュートリアル一覧](README_ja.md) · [設定](../configuration_ja.md#email)

このチュートリアルでは、好きな SMTP サーバー（プロバイダーの送信サーバー、社内のリレー、ローカルのテスト用サーバー）を通して、kjak のメッセージをメールで送ります。サーバーの設定、パスワードを秘密に保つ方法、最初のメッセージ、各種オプションを説明します。Gmail のアカウントを使う場合は、Google 固有の手順（アプリ パスワード）がある [Gmail のチュートリアル](gmail_ja.md)を参照してください。

## 1. 概要

`email` feather は、メッセージを 1 通のメールとして、決まった宛先の一覧に、1 つの SMTP サーバーを通して送ります。

```toml
[feathers.mail]
type = "email"
host = "smtp.example.com"
user = "kjak@example.com"
password = { secret = "mail.password" }
from = "kjak@example.com"
to = ["you@example.com"]
```

- kjak は SMTP を自分で話し、1 回の試行ごとに 1 つの SMTP セッションを使います。そのため、失敗したセッションがどこまで進んだかがわかります。メッセージの終わりより前のエラーは、サーバーが確実に受け取っていないことを意味します（一時的なものは再試行します）。メッセージを送り終えた **後で** 接続が切れた場合は、届いた可能性があります（`acceptance: unknown`。再試行しません）。
- 特に指定しない限り、接続は TLS で保護されます。`security = "starttls"`（既定、ポート 587）、`"tls"`（暗黙の TLS、ポート 465）、`"none"`（TLS なし。このときパスワードは使えません）のいずれかです。証明書は必ず検証します。検証を省くオプションはありません。
- 件名はメッセージのタイトルです。タイトルがなければ `[LEVEL] 本文の 1 行目` です。どちらも `subject_prefix` の後に付きます。本文は UTF-8 のプレーンテキストで、その後にメッセージの URL が続きます。ヘッダー `X-Kjak-Level` に level が入るので、メールのフィルターに使えます。
- サーバーが宛先を 1 つでも拒否すると、メッセージ全体が拒否されます。ほかの宛先にも送られません。
- **パスワードは秘密情報です。** 設定ファイルにはパスワードそのものを書きません。kjak がどこから読むか（secrets file、環境変数、OS のキーチェーン）を書きます。

## 2. 前提条件

- kjak がインストールされていること（`kjak version`）。[導入ガイド](../getting-started_ja.md#2-インストール)を参照してください。
- SMTP サーバーとその設定（ホスト名、ポート、TLS の方式）。プロバイダーや管理者の案内に、「SMTP submission」や「送信メールサーバー」として書かれています。
- そのサーバーのアカウント（ユーザー名とパスワード）。認証なしで受け付けるリレー（ローカルのリレー、テスト用サーバー）なら不要です。
- そのアカウントで使える送信者アドレス（`from`）。ほかの送信者を拒否したり書き換えたりするサーバーが多くあります。
- kjak を実行するマシンから、サーバーのポートに接続できること。家庭やクラウドのネットワークではポート 25 がよく塞がれています。送信（submission）には 587 か 465 を使います。
- 実際のサーバーなしで試すには、ローカルのテスト用 SMTP サーバーなら何でも使えます。たとえば [Mailpit](https://mailpit.axllent.org/) は、TLS なしでポート 1025 でメールを受け取ります（7 章）。既定ではすべてのインターフェースで待ち受けるので、このマシンだけに閉じるには `--smtp 127.0.0.1:1025` を付けて起動してください。

## 3. 準備

サーバーの 3 つの設定を調べます。よくある組み合わせは次のとおりです。

| サーバーが提供するもの | `port` | `security` |
|---|---|---|
| STARTTLS 付きの submission（一般的） | 587 | `"starttls"`（既定。両方とも省略できます） |
| 暗黙の TLS の submission（「SSL/TLS」） | 465 | `"tls"` |
| TLS なしのローカルのリレーやテスト用サーバー | 25、1025 など | `"none"`（`user` と `password` は書かない） |

`starttls` は平文に戻りません。STARTTLS を提供しないサーバーはエラーになり、黙って暗号化なしに切り替わることはありません。

パスワードは **secrets file** に置きます。何かを書き込む前に、非公開（モード 0600）で作成します。

```sh
mkdir -p ~/.config/kjak
[ -e ~/.config/kjak/secrets.toml ] || install -m 600 /dev/null ~/.config/kjak/secrets.toml
"${EDITOR:-vi}" ~/.config/kjak/secrets.toml
```

次の内容を、あなたのパスワードで書き加えます。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。ダミーの値を置き換える）
[mail]
password = "replace-me"
```

Windows ではファイルは `%AppData%\kjak\secrets.toml` です（プロファイルの既定の権限で非公開になります）。パスワードのほかの置き場所は 8 章を参照してください。

## 4. 最小構成

ポート 587 で STARTTLS を使うサーバー向けの、完全な `~/.config/kjak/config.toml`（Windows では `%AppData%\kjak\config.toml`）です。ホストとアドレスはあなたのものに置き換えてください。

```toml
# ~/.config/kjak/config.toml
[feathers.mail]
type = "email"
host = "smtp.example.com"
port = 587
security = "starttls"
user = "kjak@example.com"
password = { secret = "mail.password" }
from = "kjak@example.com"
to = ["you@example.com"]
```

| キー | 意味 |
|---|---|
| `host` | SMTP サーバー（必須） |
| `port` | 既定は 587（`starttls`）、465（`tls`）、25（`none`） |
| `security` | `starttls`（既定）、`tls`、`none` |
| `user`、`password` | アカウント。必ず組で指定します。`password` は認証情報の参照です |
| `from` | 送信者。表示名のない素のアドレス（`name@example.com`） |
| `to` | 宛先。素のアドレス（必須） |

確認します。

```sh
kjak config validate
kjak feather show mail
```

`feather show` は `password secret:mail.password` と表示し、パスワードそのものは表示しません。

## 5. CLI での設定

`kjak feather add` でも同じ feather を書き込めます。パスワードをコマンドラインの引数にすることはありません。kjak が読む場所を指定します。

```sh
kjak feather add mail --type email --host smtp.example.com --port 587 --security starttls --user kjak@example.com --secret-ref mail.password --from kjak@example.com --mail-to you@example.com
```

| オプション | キー |
|---|---|
| `--host`、`--port`、`--security` | `host`、`port`、`security` |
| `--user` | `user` |
| `--secret-ref NAME` / `--secret-env VAR` / `--secret-keychain SERVICE/ACCOUNT` | `password`（secrets file の項目、環境変数、キーチェーン） |
| `--from` | `from` |
| `--mail-to ADDRESS`（繰り返し、またはカンマ区切り） | `to` |
| `--subject-prefix TEXT` | `subject_prefix` |
| `--levels L[,L]` | `levels` |
| `--disabled` | `enabled = false` |

グローバルの設定ファイルに次の内容が追加されます。

```toml
[feathers.mail]
type = 'email'
host = 'smtp.example.com'
port = 587
security = 'starttls'
user = 'kjak@example.com'
password = {secret = 'mail.password'}
from = 'kjak@example.com'
to = ['you@example.com']
```

キーを 1 つだけ変えるコマンドはありません。すべてのオプションと `--force` を付けて `feather add` をもう一度実行する（feather 全体を置き換えます）か、ファイルを編集して `kjak config validate` を実行します。そのほかのコマンドです。

```sh
kjak feather list
kjak feather disable mail
kjak feather enable mail
kjak feather remove mail
```

## 6. 最初の通知

まず、送り先の解決とパスワードを確かめます。何も送りません。

```sh
kjak send --to mail --dry-run "test"
```

```
plan  mail (email)
dry run: 1 target(s), 0 skipped; nothing was sent
```

次に送ります。

```sh
kjak send --to mail --title "Deploy" --level success "api 1.4.2 is live"
```

```
ok    mail (email) delivered in 812ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

件名 `Deploy` のメールが届きます。`--title` がなければ、件名は `[SUCCESS] api 1.4.2 is live` です。`delivered` は、サーバーがメッセージを受け付けた（メッセージに 2xx の応答を返した）ことを意味します。その先（迷惑メールのフィルター、転送）はメールシステムの仕事です。届かないときは、まず迷惑メールのフォルダーを見てください。

終了コードは、配信できれば 0、配信に失敗すれば 1、何も送らなかった（設定や認証情報のエラー）なら 2 です。スクリプトでは、すべての試行が入った JSON のレポートを使えます。

```sh
kjak send --to mail -o json "test"
```

`results[0].status` が `delivered`、`acceptance` が `confirmed` になります。失敗したときは `error.code` と `error.message` が理由を示し、`history` に試行ごとの記録が並びます。

## 7. 応用的な設定

よく使うオプションを入れた完全な設定です。アラートだけを送る暗黙の TLS のサーバーと、すべてを送るローカルのリレーを、1 つの group にまとめます。

```toml
# ~/.config/kjak/config.toml
[defaults]
timeout = "30s"       # 送信全体の期限（再試行を含む。SMTP サーバーは遅いことがある）
max_retries = 2       # 一時的な失敗（4xx の応答、ネットワークエラー）の再試行回数
targets = ["mail"]    # --to なしの kjak send の送り先

[feathers.oncall_mail]
type = "email"
host = "smtp.example.com"
port = 465
security = "tls"
user = "alerts@example.com"
password = { env = "KJAK_SMTP_PASSWORD" }
from = "alerts@example.com"
to = ["oncall@example.com", "sre@example.com"]
subject_prefix = "[prod]"
levels = ["error", "critical"]

[feathers.relay_mail]
type = "email"
host = "127.0.0.1"
port = 25
security = "none"
from = "kjak@build-01.example.com"
to = ["builds@example.com"]
subject_prefix = "[kjak]"

[groups.mail]
feathers = ["oncall_mail", "relay_mail"]
```

- **暗黙の TLS**（`security = "tls"`、ポート 465）: 最初のバイトから TLS です。サーバーの案内に 465 の「SSL/TLS」とあるとき、または 587 の STARTTLS が提供されていないときに使います。
- **level**: `levels` は feather が受け付ける level を絞ります。group の中では、メッセージの level を受け付けない feather は飛ばされます。`--to` で直接指定するとエラーです（`does not accept level info`）。
- **複数の宛先** には、全員を `To:` に並べた 1 通のメールが届きます。
- **`security = "none"`** は、このマシンか信頼できるローカルネットワークのリレー向けです。TLS がないので、`user` と `password` は拒否されます（パスワードが平文で流れるため）。
- **タイムアウトと再試行** は `[defaults]`（または `--timeout`、`--max-retries`）で設定します。email feather には独自の `timeout` や `retry` はありません。パスワードの誤り（`535`）は再試行しません。再試行するとアカウントがロックされることがあるためです。

**ローカルのテスト用サーバー**（Mailpit、またはこのマシンで待ち受ける任意の SMTP サーバー）は、どこにも配送せずにすべてを受け取ります。

```toml
# ~/.config/kjak/config.toml
[feathers.test_mail]
type = "email"
host = "127.0.0.1"
port = 1025
security = "none"
from = "kjak@example.com"
to = ["you@example.com"]
```

```sh
kjak send --to test_mail --title "hello" "first test"
```

**プロジェクトの設定ファイル**（`.kjak.toml`）の email feather は、公開アドレスにしか接続できません。このマシンやプライベートネットワークのサーバーは、`allow_networks`（CIDR）か `allow_hosts`（ホスト名）で許可する必要があり、`kjak config trust` がそれを表示します。グローバルの設定ファイルの feather には、この制限はありません。[宛先の制限](../configuration.md#address-policy-of-a-projects-slack-discord-and-email-feathers)を参照してください。

```toml
# .kjak.toml（プロジェクトの設定ファイル。Trust した後で使われる）
[feathers.team_mail]
type = "email"
host = "smtp.corp.example"
user = "ci@corp.example"
password = { env = "TEAM_SMTP_PASSWORD" }
from = "ci@corp.example"
to = ["team@corp.example"]
allow_hosts = ["smtp.corp.example"]
```

## 8. 秘密情報

`password` は、読み取り先をちょうど 1 つ持つ参照です。

| 読み取り先 | TOML | CLI | 向いている場面 |
|---|---|---|---|
| secrets file | `{ secret = "mail.password" }` | `--secret-ref mail.password` | 自分のマシン。ファイルは非公開（モード 0600） |
| 環境変数 | `{ env = "KJAK_SMTP_PASSWORD" }` | `--secret-env KJAK_SMTP_PASSWORD` | CI、サーバー、コンテナー、cron、Windows |
| OS のキーチェーン | `{ keychain = { service = "kjak", account = "mail" } }` | `--secret-keychain kjak/mail` | macOS や Linux のデスクトップ |

3 章の secrets file が `mail.password` の値を持ちます。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[mail]
password = "replace-me"

[oncall_mail]
password = "another-dummy"
```

kjak はキーチェーンを読むだけです。項目は OS のツールで作ります。パスワードはプロンプトで入力するので、シェルの履歴に残りません。

```sh
security add-generic-password -s kjak -a mail -w                     # macOS
secret-tool store --label "kjak mail" service kjak account mail      # Linux (libsecret-tools)
```

`env` の場合は、CI の secret store から変数を設定するか、対話的な bash や zsh でエコーなしに読み込みます（`read -rs KJAK_SMTP_PASSWORD; export KJAK_SMTP_PASSWORD`）。履歴に残るコマンドラインに `KJAK_SMTP_PASSWORD=...` と打ち込まないでください。

- `kjak config validate --strict` は、ファイルに書かれたパスワード（`{ literal = "..." }`、非推奨）を拒否します。`kjak config migrate-secrets` は、そうした literal を secrets file に移します。
- 解決できないパスワードがあると、何も送らずに送信を止めます（終了コード 2）。
- パスワードはすべてのエラーとレポートから伏せられます。AUTH を拒否したときのサーバーの文言は表示しません（サーバーがエンコードしたパスワードを繰り返すことがあるため）。

## 9. トラブルシューティング

| メッセージ | 原因 | 対処 |
|---|---|---|
| `the server does not offer STARTTLS (set security = "tls" for implicit TLS)` | ポートが暗黙の TLS を話す、またはサーバーに STARTTLS がない | サーバーが対応していれば `security = "tls"` と `port = 465` |
| `TLS handshake: connection error` | STARTTLS や平文を期待するポート（587、25）で `security = "tls"` | `security = "starttls"` とポート 587 |
| `delivery deadline exceeded during greeting` | 暗黙の TLS のポート（465）で `security = "starttls"`（または `none`）。双方が期限まで待ち続ける | `security = "tls"` |
| `TLS certificate verification failed` | 証明書が信頼されていない、または名前が `host` と一致しない | 証明書のホスト名を使う。プライベート CA は[認証局](../security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir)を参照 |
| `AUTH: SMTP 535 authentication failed` | ユーザー名かパスワードの誤り、またはプロバイダーがアプリ パスワードを求めている | 両方を確認する。再試行はしない |
| `AUTH: the server offers neither PLAIN nor LOGIN` | サーバーがほかの方式（OAuth 2.0、CRAM-MD5 など）しか提供しない | kjak は AUTH PLAIN と LOGIN に対応。どちらかを提供するアカウントかリレーを使う |
| `connect: dial: connect: connection refused`、または `connect` でのタイムアウト | ホストかポートの誤り、ファイアウォール、ポート 25 の遮断 | そのマシンからホストとポートを確認する |
| `MAIL FROM: SMTP 5xx ...` / `RCPT TO <address>: SMTP 5xx ...` | サーバーが送信者か宛先を拒否した | アカウントが使える `from` にする。アドレスを確認する |
| `user and password go together` | どちらか一方だけが設定されている | 両方を設定するか、どちらも設定しない（リレー） |
| `security = "none" sends the password in clear text; ...` | `none` と `user` の組み合わせ | `starttls` か `tls` にする。リレーならアカウントを外す |
| `from: "Kjak <kjak@example.com>" is not a bare address` | 表示名が付いている | `from = "kjak@example.com"` |
| `routing error: feather "mail" does not accept level info` | `levels` がメッセージの level を除外している | `--level error` を付けるか、group に送る |
| `environment variable KJAK_SMTP_PASSWORD is not set` | `env` のパスワード | kjak の環境で export する |
| `the secrets file is not private: ...` | secrets file をほかの人が読める | ファイルを `chmod 600` |
| `????` / `the reply to the message was lost (...); the server may have accepted it` | メッセージを送り終えた後で接続が切れた | 送り直す前にメールボックスを確認する。kjak は再試行しない |
| `connect: address 127.0.0.1 is loopback: allow it with allow_networks` | `.kjak.toml` の email feather がローカルのサーバーに接続しようとした | その feather に `allow_networks = ["127.0.0.1/32"]` を書き、`kjak config trust` |

詳しくは [トラブルシューティング](../troubleshooting.md) と [SMTP の受付状態の規則](../delivery.md#e-mail-smtp) を参照してください。

```sh
kjak config path
kjak config validate
kjak send --to mail --dry-run "test"
kjak send --to mail -o json "test"
```

## 10. セキュリティ上の注意

- **TLS**: `starttls` と `tls` はサーバーの証明書を検証し（TLS 1.2 以上）、平文に戻ることはありません。STARTTLS の応答と一緒に送られた平文のデータ（インジェクション）があると、セッションを終了します。
- **`security = "none"`** はメッセージを平文で送り、パスワードは決して送りません。このマシンのリレーか、信頼できるネットワークだけで使ってください。
- **パスワード** を kjak が設定ファイルに書き込むことはなく、表示することも、コマンドラインの引数にすることもありません。エラーからも伏せられます。
- **メッセージの内容**: タイトルには改行を入れられません（`Subject:` ヘッダーになるため）。メッセージがヘッダーを追加することはできません。
- **プロジェクトの設定ファイル** は SMTP のホストを URL と同じように選べます。そのため、その email feather は、`allow_networks` / `allow_hosts` で許可しない限り公開アドレスにしか接続できません。また、`env` と `keychain` の参照は、プロジェクトを Trust するときに名前ごとに許可する必要があります。
- kjak の環境の **`SSL_CERT_FILE` / `SSL_CERT_DIR`** が、信頼する認証局を決めます（[セキュリティ](../security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir)）。

## 11. 関連資料

- [設定: email](../configuration_ja.md#email) — すべてのキー
- [Gmail のチュートリアル](gmail_ja.md) — 同じ feather を Gmail の設定で使う
- [配信: e-mail (SMTP)](../delivery.md#e-mail-smtp) — SMTP の段階ごとの受付状態と再試行
- [セキュリティ: 通信の保護](../security.md#transport-security)
- [secrets file](../configuration_ja.md#77-secrets-ファイル) と [認証情報の参照](../configuration_ja.md#71-認証情報の参照)
- [CLI: kjak feather](../cli.md#kjak-feather)
- [設定例: smtp-only.toml](../examples/smtp-only.toml)
- [トラブルシューティング](../troubleshooting.md)
