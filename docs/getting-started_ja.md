# はじめに（導入ガイド）

[English](getting-started.md) · [設定リファレンス](configuration_ja.md) · [README](../README_ja.md)

このガイドでは、kjak のインストールから、Slack・Discord・メールへの最初の通知までを順に説明します。最後に、シェルスクリプトなどから kjak を使う例を紹介します。

- [1. kjak とは](#1-kjak-とは)
- [2. インストール](#2-インストール)
- [3. 最初の通知を送る](#3-最初の通知を送る)
- [4. Slack](#4-slack)
- [5. Discord](#5-discord)
- [6. メール（SMTP）](#6-メールsmtp)
- [7. 利用例](#7-利用例)
- [8. 次に読むもの](#8-次に読むもの)

## 1. kjak とは

kjak は、**1つのメッセージを複数の通知先へ送る**コマンドラインツールです。通知先は Slack のチャンネル、Discord のチャンネル、メールアドレスです。ターミナル、シェルスクリプト、cron、CI から実行できます。

```sh
kjak send --to development --level error --title "nightly build" "tests failed on main"
```

- **feather（フェザー）** は1つの通知先です。Slack の Incoming Webhook、Discord の Webhook、または SMTP サーバーと宛先の組を指します。
- **group（グループ）** は feather に名前を付けてまとめたものです。`--to development` と指定すると、グループ `development` に属するすべての feather に送ります。
- kjak は各サービスへ直接送信します。Slack と Discord には Webhook URL で、メールはお使いの SMTP サーバー経由で送ります。kjak 独自のサーバーはありません。
- メッセージは本文と、任意のタイトル、レベル（`info`、`success`、`warning`、`error`、`critical`）、リンクで構成されます。

スクリプトや時間のかかるビルド、定期実行ジョブの結果を知りたいけれど、サービスごとに通知のコードを書きたくない――そんな開発者や運用担当者向けのツールです。

## 2. インストール

### Homebrew（macOS、Linux）

```sh
brew install magicdrive/tap/kjak
```

Formula は [magicdrive/homebrew-tap](https://github.com/magicdrive/homebrew-tap) にあります。

### Go

Go 1.27 以降が必要です（古い `go` は 1.27 のツールチェーンを自動でダウンロードします）。

```sh
go install github.com/magicdrive/kjak@latest
```

バイナリは `$(go env GOPATH)/bin`（通常は `~/go/bin`）に置かれます。

### GitHub Releases（全プラットフォーム）

[GitHub Releases](https://github.com/magicdrive/kjak/releases) には、プラットフォームごとのアーカイブがあります。darwin/arm64、darwin/amd64、linux/arm64、linux/amd64 は `.tar.gz`、windows/amd64 は `.zip` です。中身は `kjak`（`kjak.exe`）、`LICENSE`、`README.md` です。

```sh
# 例：Linux x86-64 用の v0.0.2
curl -LO https://github.com/magicdrive/kjak/releases/download/v0.0.2/kjak_0.0.2_linux_amd64.tar.gz
curl -LO https://github.com/magicdrive/kjak/releases/download/v0.0.2/kjak_checksums.txt
sha256sum -c kjak_checksums.txt --ignore-missing     # macOS: shasum -a 256 -c kjak_checksums.txt --ignore-missing
tar -xzf kjak_0.0.2_linux_amd64.tar.gz kjak
sudo install -m 0755 kjak /usr/local/bin/kjak       # PATH の通った任意のディレクトリで構いません
```

必要であれば、アーカイブが kjak のリリースワークフローでビルドされたものか確認できます（[GitHub CLI](https://cli.github.com/) が必要です）。

```sh
gh attestation verify kjak_0.0.2_linux_amd64.tar.gz --repo magicdrive/kjak
```

Windows では、`.zip` から `kjak.exe` を取り出し、`PATH` の通ったディレクトリに置いてください。

### インストールの確認

```sh
kjak version        # kjak version v0.0.2
command -v kjak     # シェルが見つけた場所（PowerShell では Get-Command kjak）
```

`command not found` と表示される場合は、バイナリのあるディレクトリが `PATH` に入っていません（`go install` の場合は `$(go env GOPATH)/bin` を追加してください）。

## 3. 最初の通知を送る

この節では Slack を例にします。Discord やメールを使う場合は、[5 章](#5-discord)または[6 章](#6-メールsmtp)で Webhook URL や SMTP の設定値を用意し、同じ手順で進めてください。

### 手順 1 — 通知先を用意する

メッセージを受け取りたいチャンネルに、Slack の Incoming Webhook を作成します（[4.1](#41-incoming-webhook-を作成する)）。最初のテストには、自分だけが見るチャンネルがおすすめです。

### 手順 2 — Webhook URL を取得する

Slack に `https://hooks.slack.com/services/T…/B…/…` のような URL が表示されます。**この URL 自体が認証情報です。**URL を知っている人は誰でもそのチャンネルに投稿できます。チャットや Issue、コミットするファイルに貼り付けないでください。

### 手順 3 — kjak に設定する

kjak の設定ファイルに保存するのは、秘密情報そのものではなく、その**参照先**です。最も簡単なのは環境変数で渡す方法です。

```sh
kjak config init                       # ~/.config/kjak/config.toml を作成

# bash / zsh：プロンプトに URL を入力すると、シェルの履歴に残りません
printf 'Slack webhook URL: '; read -rs KJAK_DEV_SLACK_WEBHOOK; echo
export KJAK_DEV_SLACK_WEBHOOK

kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK
```

PowerShell の場合：

```powershell
kjak config init
$env:KJAK_DEV_SLACK_WEBHOOK = Read-Host "Slack webhook URL"
kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK
```

`dev_slack` は feather の名前です。英数字、`_`、`.`、`-` が使えます。環境変数は kjak を実行する環境で設定されている必要があります。次回以降のセッションでも URL を使えるようにする方法は [4.3](#43-url-を保存しておく) を参照してください。

### 手順 4 — テスト通知を送る

まずドライランで確認します。送信先と認証情報を解決するだけで、何も送信しません。

```sh
kjak send --to dev_slack --dry-run "hello from kjak"
```

```
plan  dev_slack (slack)
dry run: 1 target(s), 0 skipped; nothing was sent
```

続いて、実際に送信します。

```sh
kjak send --to dev_slack "hello from kjak"
```

### 手順 5 — 送信成功を確認する

```
ok    dev_slack (slack) delivered in 312ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

`ok` は Slack がメッセージの受け付けを確認したことを示し、kjak は終了コード `0` で終了します。チャンネルにメッセージが表示されます。終了コードは、コマンドの直後に `echo $?` を実行すると確認できます。

| 終了コード | 意味 |
|---|---|
| `0` | すべての送信先に配信した |
| `1` | 配信に失敗した送信先がある（または配信結果が不明） |
| `2` | 入力・設定・事前チェックのエラー。何も送信していない |
| `130` | 中断された（Ctrl-C または SIGTERM） |

### 手順 6 — うまくいかないとき

| 表示 | 原因 | 対処 |
|---|---|---|
| `environment variable KJAK_DEV_SLACK_WEBHOOK is not set`（exit 2） | このシェルで環境変数が設定されていない | 手順 3 の export をやり直す |
| `webhook_url: ... must be https ...`（exit 2） | 値が Slack の発行した URL ではない | `https://hooks.slack.com/...` の URL をコピーし直す |
| `FAIL  dev_slack (slack) failed (1 attempt): HTTP 404 ...`（exit 1） | Webhook が削除された、または URL が間違っている | Webhook を作り直し、環境変数を設定し直す |
| `HTTP 403` / `HTTP 401` | Webhook またはアプリが無効化された | Webhook を作り直す |
| `the project configuration is not trusted: ...`（exit 2） | カレントディレクトリに `.kjak.toml` がある | [Trust](configuration_ja.md#74-プロジェクト設定の-trust) を参照するか、`--no-project` を付ける |
| `routing error: no feather or group named "dev-slack"`（exit 2） | `--to` の名前の打ち間違い | `kjak feather list` で名前を確認する |

`kjak send ... -o json` を使うと、試行ごとの HTTP ステータスを確認できます。そのほかのエラーは [troubleshooting.md](troubleshooting.md)（英語）を参照してください。

## 4. Slack

### 4.1 Incoming Webhook を作成する

Slack の画面は変わることがあります。公式ガイドは [Sending messages using incoming webhooks](https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/) です。手順の概要は次のとおりです。

1. <https://api.slack.com/apps> で、ワークスペース用の Slack アプリを作成します（「Create New App」→「From scratch」）。
2. アプリの **Incoming Webhooks** を開き、有効にします。
3. **Add New Webhook**（to Workspace）で投稿先のチャンネルを選び、許可します。
4. Webhook URL（`https://hooks.slack.com/services/...`）をコピーします。

ワークスペースによっては、アプリの追加に管理者の承認が必要です。

### 4.2 kjak に登録する

```sh
kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK
```

特定のレベルだけを受け取るには `--levels warning,error,critical` を付けます。feather を変更するときは、`--force` を付けて `feather add` をもう一度実行します（feather 全体が置き換わります）。

### 4.3 URL を保存しておく

`export` の効果はそのシェルセッションの間だけです。秘密情報を保存しておく方法は次のとおりです。

| 保存先 | 方法 | 向いている環境 |
|---|---|---|
| OS のキーチェーン（macOS、Linux デスクトップ） | 一度登録し、`--secret-keychain SERVICE/ACCOUNT` で参照する | 自分のマシン |
| 環境変数 | CI のシークレット管理、サービスマネージャー、シェルが読み込むファイルで設定する | CI、サーバー、cron |
| 設定ファイル | `--secret-stdin`（ファイルはモード 0600 で作成される） | 手元での簡単な利用。共有するファイルには使わない |

macOS のキーチェーン（末尾の `-w` により、`security` が URL の入力を求めます）：

```sh
security add-generic-password -s kjak -a dev_slack -w
kjak feather add dev_slack --type slack --secret-keychain kjak/dev_slack --force
```

Linux（GNOME Keyring、KWallet。`libsecret-tools` の `secret-tool` とデスクトップセッションが必要です。URL はプロンプトから入力します）：

```sh
secret-tool store --label "kjak dev_slack" service kjak account dev_slack
kjak feather add dev_slack --type slack --secret-keychain kjak/dev_slack --force
```

Windows では kjak はキーチェーンに対応していません。環境変数を使ってください。

標準入力から渡して、設定ファイルにリテラルとして保存する場合：

```sh
read -rs URL && printf '%s\n' "$URL" | kjak feather add dev_slack --type slack --secret-stdin --force; unset URL
```

### 4.4 テストする

```sh
kjak send --to dev_slack --title "kjak" --level success "Slack works"
```

メッセージは `[SUCCESS] kjak: Slack works` という本文と、レベルに応じた色の添付で表示されます。`@channel` などのメンションはエスケープされるので、通知で誰かを呼び出すことはありません。

### 4.5 よくあるエラー

| エラー | 意味 |
|---|---|
| `HTTP 404` | Webhook またはそのチャンネルが存在しない |
| `HTTP 403` / `HTTP 401` | アプリが削除された、または Webhook が無効化された |
| `HTTP 400` | Slack がペイロードを拒否した。メッセージの長さを確認する（本文は最大 16384 文字） |
| `HTTP 429` | レート制限。kjak は `Retry-After` の時間だけ待ち、期限内で再試行する |

## 5. Discord

### 5.1 Webhook を作成する

チャンネルの **ウェブフックの管理（Manage Webhooks）** 権限が必要です。Discord のヘルプ記事は [Intro to Webhooks](https://support.discord.com/hc/en-us/articles/228383668-Intro-to-Webhooks)、API リファレンスは [Webhook Resource](https://discord.com/developers/docs/resources/webhook) です。

1. サーバー設定 → **連携サービス（Integrations）** → **ウェブフック（Webhooks）** → **新しいウェブフック（New Webhook）**。
2. 投稿先のチャンネルを選びます（名前やアイコンは任意）。
3. **ウェブフックURLをコピー（Copy Webhook URL）**（`https://discord.com/api/webhooks/<id>/<token>`）。

### 5.2 kjak に登録する

```sh
printf 'Discord webhook URL: '; read -rs KJAK_DEV_DISCORD_WEBHOOK; echo
export KJAK_DEV_DISCORD_WEBHOOK
kjak feather add dev_discord --type discord --secret-env KJAK_DEV_DISCORD_WEBHOOK --username kjak
```

`--username` はメッセージの表示名です（任意）。URL を保存しておく方法は Slack と同じです（[4.3](#43-url-を保存しておく)）。

### 5.3 テストする

```sh
kjak send --to dev_discord --title "kjak" --level warning "Discord works"
```

メッセージは、レベルに応じた色の埋め込み（embed）として表示されます。kjak は Discord にメッセージ作成の確認を求める（`?wait=true`）ので、`ok` は Discord がメッセージを作成したことを意味します。メンションは無効化されます。

### 5.4 よくあるエラー

| エラー | 意味 |
|---|---|
| `HTTP 401` / `HTTP 404` | Webhook が削除された、または URL 内のトークンが間違っている |
| `HTTP 400` | Discord がペイロードを拒否した |
| `HTTP 429` | レート制限。kjak は Discord の指定どおりに待ち、期限内で再試行する |

本文が 4096 文字を超える場合は、Discord の埋め込みに収まるように切り詰められます（末尾は `…`）。

## 6. メール（SMTP）

### 6.1 必要な情報

メールプロバイダーまたは管理者から、次の情報を入手してください。

| 設定 | kjak のオプション | 説明 |
|---|---|---|
| SMTP ホスト | `--host` | 必須 |
| ポート | `--port` | 既定値：`starttls` なら 587、`tls` なら 465、`none` なら 25 |
| 暗号化方式 | `--security` | `starttls`（既定）：平文で接続してから STARTTLS で暗号化。STARTTLS は必須。`tls`：最初から TLS（通常はポート 465）。`none`：暗号化なし。ローカルのリレー専用 |
| ユーザーとパスワード | `--user`、`--secret-env` / `--secret-keychain` / `--secret-stdin` | AUTH PLAIN、使えなければ AUTH LOGIN。`security none` では使えない |
| 送信元 | `--from` | アドレスのみ：`kjak@example.com` |
| 宛先 | `--mail-to` | 1つ以上のアドレス（アドレスのみ） |

ホスト・ポート・暗号化方式や、SMTP 用に別のパスワード（アプリパスワード）が必要かどうかは、プロバイダーのドキュメントで確認してください。kjak は常にサーバー証明書を検証し（TLS 1.2 以降）、平文の通信に切り替えることはありません。

### 6.2 kjak に登録する

```sh
printf 'SMTP password: '; read -rs KJAK_SMTP_PASSWORD; echo
export KJAK_SMTP_PASSWORD
kjak feather add oncall_mail --type email \
    --host smtp.example.com --port 587 --security starttls \
    --user kjak@example.com --secret-env KJAK_SMTP_PASSWORD \
    --from kjak@example.com --mail-to oncall@example.com \
    --subject-prefix "[kjak]"
```

宛先が複数ある場合は `--mail-to a@example.com,b@example.com` のように指定します（`--mail-to` を繰り返しても構いません）。

### 6.3 テストする

```sh
kjak send --to oncall_mail --dry-run "mail test"     # パスワードの参照だけを確認する
kjak send --to oncall_mail --title "kjak" "mail test"
```

ドライランではサーバーに接続しません。実際の送信は、サーバーがメッセージを配送のために受け付けた時点（メッセージデータの後に `2xx` の応答があった時点）で成功となります。件名は `[kjak] kjak`（接頭辞とタイトル）です。タイトルがない場合は `[LEVEL] 本文の1行目` になります。

### 6.4 よくあるエラー

| エラー | 意味 |
|---|---|
| `AUTH: SMTP 535 ...` | ユーザー名またはパスワードが違う（アカウントのロックを避けるため再試行しない） |
| `the server does not offer STARTTLS` | そのポートは最初から TLS を使う。`--security tls --port 465` を指定する |
| `TLS certificate verification failed` | 証明書がシステムで信頼されていない。kjak には検証を省略するオプションはない |
| RCPT で `SMTP 550 ...` | 宛先が拒否された。誰にも送信されない |
| 接続拒否・タイムアウト | ホストかポートが違う、またはファイアウォールが SMTP の送信を遮断している |

## 7. 利用例

以下の例ではグループ `development` を使います。登録済みの feather から作成してください。

```sh
kjak group set development dev_slack dev_discord
```

### 複数の通知先へ送る

```sh
kjak send --to development "deploy started"
kjak send --to dev_slack,oncall_mail --level error "disk almost full"
```

```
ok    dev_slack (slack) delivered in 280ms (1 attempt)
FAIL  dev_discord (discord) failed (1 attempt): HTTP 404: Unknown Webhook
partial: 1 delivered, 1 failed, 0 skipped
```

送信先ごとに独立して配信するので、1つが失敗しても他には影響しません。1つでも失敗すると終了コードは `1` です。無効化されているメンバーや、そのレベルを受け付けないメンバーは `skip` と表示され、失敗には数えません。

### シェルスクリプトから通知する

スクリプト自身の終了コードを保ち、通知の失敗で本来の処理結果が隠れないようにします。

```sh
#!/bin/sh
# notify.sh: run a command, report its result, exit with its status
"$@"
status=$?
if [ "$status" -eq 0 ]; then
	kjak send --to development --level success --title "$1" "finished" ||
		echo "notify.sh: the notification failed (kjak exit $?)" >&2
else
	kjak send --to development --level error --title "$1" "failed with exit status $status" ||
		echo "notify.sh: the notification failed (kjak exit $?)" >&2
fi
exit "$status"
```

```sh
sh notify.sh make test
```

### 長時間処理の完了を通知する

```sh
start=$(date +%s)
./long-job.sh
status=$?
elapsed=$(( $(date +%s) - start ))
if [ "$status" -eq 0 ]; then level=success; else level=error; fi
kjak send --to development --level "$level" --title "long-job" \
    "exit status $status after ${elapsed}s"
```

### 失敗したコマンドの出力を送る

パイプを使うと左側のコマンドの終了コードが失われるので、先に出力を変数に取り込みます。

```sh
if ! out=$(make test 2>&1); then
	printf '%s\n' "$out" | tail -n 20 |
		kjak send --to development --stdin --level error --title "tests failed"
fi
```

`--stdin` は本文を標準入力から読みます（最大 16384 文字）。

### cron から通知する

cron は最小限の環境でコマンドを起動します。シェルのプロファイルは読み込まれず、`PATH` は短く、export した変数もありません。kjak に必要なものは crontab かラッパースクリプトで設定してください。

```sh
#!/bin/sh
# ~/bin/backup-and-notify.sh, run by cron
PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin
. "$HOME/.config/kjak/env"     # export KJAK_DEV_SLACK_WEBHOOK=...; chmod 600 this file
/usr/local/bin/backup.sh
status=$?
if [ "$status" -ne 0 ]; then
	kjak send --to development --level error --title "backup" "backup failed with exit status $status"
fi
exit "$status"
```

```
# crontab -e
0 3 * * * $HOME/bin/backup-and-notify.sh >> $HOME/backup.log 2>&1
```

`env` ファイルはスクリプトが読み込む普通のシェルスクリプトです。他人に読まれないようにし（`chmod 600`）、リポジトリには入れないでください。

### プログラムで JSON を使う

```sh
kjak send --to development -o json "build finished" > report.json
```

レポート（`schema_version: 1`）には、送信先ごとの `status`、`acceptance`、試行、エラーが含まれます。詳しくは [cli.md](cli.md#json-output--o-json)（英語）を参照してください。

## 8. 次に読むもの

- [設定リファレンス](configuration_ja.md)：すべての設定キー、管理コマンド、認証情報、Trust、配信の仕様。
- [CLI usage](cli.md)（英語）：すべてのコマンドとオプション。
- [Troubleshooting](troubleshooting.md)（英語）。
