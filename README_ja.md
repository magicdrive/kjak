# kjak

[English](README.md) · [導入ガイド](docs/getting-started_ja.md) · [設定リファレンス](docs/configuration_ja.md)

kjak は、1つの通知を複数の通知先（このマシンのデスクトップ（macOS、Windows、Linux）、Slack、Discord、メール、任意の HTTP API、自作のプラグイン）へ送るコマンドラインツールです。ターミナル、スクリプト、CI から使えます。通知先1つ1つを **feather（フェザー）** と呼び、feather は **group（グループ）** にまとめられます。

```sh
kjak send --to development --level error --title "nightly build" "tests failed on main"
```

- 明示的なサブコマンド：`send`、`feather`、`group`、`config`、`template`、`version`
- ベストエフォートの配信：feather ごとに独立して配信し、同時に最大 5 件。再試行を含めて1つの期限（10 秒）に収める
- 正直な結果報告：サービスが受け付けを確認したときだけ**配信済み（confirmed）**とする。届いたかもしれないが応答がなかったメッセージは**不明（unknown）**とし、重複を避けるため再試行しない
- 秘密情報は環境変数、OS のキーチェーン、または非公開の secrets ファイルから読み、表示せず、設定ファイルに置かない
- プロジェクト設定（`.kjak.toml`）は Trust した後にだけ使い、あなたの秘密情報は許可した名前のものしか読まない
- テキストまたは JSON（`schema_version: 1`）で出力。終了コードの仕様は固定（[v1 で保証する内容](docs/stability.md)、英語）
- macOS、Linux、Windows 向けの単一の静的 Go バイナリ（`CGO_ENABLED=0`）

## インストール

```sh
brew install magicdrive/tap/kjak
```

または [GitHub Releases](https://github.com/magicdrive/kjak/releases) からアーカイブ（macOS / Linux は `.tar.gz`、Windows は `.zip`）をダウンロードし、確認してください。

```sh
sha256sum -c kjak_checksums.txt --ignore-missing
gh attestation verify kjak_1.0.0_linux_amd64.tar.gz --repo magicdrive/kjak \
  --signer-workflow magicdrive/kjak/.github/workflows/release.yml --source-ref refs/tags/v1.0.0
```

macOS でブラウザからダウンロードしたアーカイブには quarantine の印が付きます。kjak のバイナリは notarize されていないため、印を外すまで Gatekeeper が実行を止めることがあります（`xattr -d com.apple.quarantine kjak`）。Homebrew、インストールスクリプト、`curl` では起きません。

ソースから（開発者向け、Go 1.27）：`git clone https://github.com/magicdrive/kjak.git && cd kjak && make build`（バイナリは `build/kjak`）

macOS と Linux（amd64、arm64）では、インストールスクリプトも使えます。ダウンロードして、必要なら中身を確認してから `sh` で実行します。

```sh
curl -fsSLO https://raw.githubusercontent.com/magicdrive/kjak/main/install.sh
sh install.sh                                  # 最新の安定版を ~/.local/bin に
sh install.sh --version v0.0.3                 # 版を指定（古い版も可、v0.0.2 以降）
sh install.sh --dir "$HOME/bin"                # インストール先を指定
```

アーカイブはリリースの `kjak_checksums.txt` の SHA-256 で検証してから展開し、取り出すのは `kjak` だけです。`sudo` は使わず、`PATH` も変更しません。Homebrew の管理するディレクトリには入れません（Homebrew の kjak は `brew upgrade kjak` で更新してください）。Windows では動きません（WSL の中では Linux と同じ手順）。オプション、検証、注意点の詳細は [README](README.md#install-script-macos-and-linux-amd64-and-arm64)（英語）を参照してください。

対応プラットフォーム：darwin/arm64、darwin/amd64、linux/arm64、linux/amd64、windows/amd64

詳しい手順は[導入ガイド](docs/getting-started_ja.md#2-インストール)を参照してください。

## クイックスタート

このマシンの画面に通知を出すまで、3 つのコマンドです。

```sh
kjak config init                    # ~/.config/kjak/config.toml（コメント付きの設定例入り）
kjak feather add desktop --type os
kjak send --to desktop --title "Build complete" --level success "All tests passed"
```

macOS は GUI のセッション、Windows はデスクトップのセッション、Linux は `notify-send` とデスクトップのセッションで表示します。SSH や CI には表示する画面がありません（[OS の通知](docs/os.md)、英語）。デスクトップ、Slack、Discord、メールそれぞれの手順は[導入ガイド](docs/getting-started_ja.md)で詳しく説明しています。Slack の場合は次のとおりです。

```sh
# 1. 設定ファイルを作成（~/.config/kjak/config.toml）
kjak config init

# 2. feather を追加。Webhook URL は環境変数から読む
printf 'Slack webhook URL: '; read -rs KJAK_DEV_SLACK_WEBHOOK; echo
export KJAK_DEV_SLACK_WEBHOOK
kjak feather add dev_slack --type slack --secret-env KJAK_DEV_SLACK_WEBHOOK

# 3. group を作成
kjak group set development dev_slack

# 4. 何が起きるかを確認してから送信
kjak send --to development --dry-run "build finished"
kjak send --to development --title "CI" --level success "build finished"
```

```
ok    dev_slack (slack) delivered in 312ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

## チュートリアルと設定例

すべての feather にチュートリアル（日本語・英語）があります。サービスや OS での準備から最初の通知まで、そのまま使える `config.toml` 付きで説明します：[OS の通知](docs/tutorials/os_ja.md)、[Discord](docs/tutorials/discord_ja.md)、[Slack](docs/tutorials/slack_ja.md)、[Gmail](docs/tutorials/gmail_ja.md)、[任意の SMTP サーバー](docs/tutorials/smtp_ja.md)、[HTTP API](docs/tutorials/http_ja.md)、[プラグイン](docs/tutorials/plugin_ja.md)（[一覧](docs/tutorials/README_ja.md)）。

[docs/examples](docs/examples/README.md) には、そのままコピーして使える設定（1 つの feather、複数、group、template、secrets ファイル）があり、すべて CI で検証しています。`kjak config init` は新しい設定ファイルに全 feather のコメント付き設定例を書き込みます。`kjak config init --minimal` は設定例なしで書き込みます。

## 送信

メッセージの入力元は、次のうちちょうど1つです。

```sh
kjak send --to dev_slack "deploy started"                     # 引数
make test 2>&1 | tail -n 20 | kjak send --to development --stdin --level error --title "tests failed"
echo '{"title":"Disk","body":"/var at 91%","level":"warning"}' | kjak send --to ops --json-input -
kjak send --to ops --template deploy --var service=api --var version=1.4.2
kjak send --to ops --template-file deploy.toml --var service=api
```

| オプション | 意味 |
|---|---|
| `-t, --to NAME[,NAME]` | feather または group（繰り返し可）。既定は `defaults.targets` |
| `-T, --title` / `-l, --level` / `-u, --url` | タイトル、レベル（`info`、`success`、`warning`、`error`、`critical`）、リンク |
| `--timeout 10s` / `--max-retries 2` / `--concurrency 5` | 配信の上限 |
| `--dry-run` | 送信先と認証情報を解決するだけで、何も送らない |
| `-o json` | JSON レポート（`schema_version: 1`） |

終了コード：`0` 配信済み、`1` 配信に失敗した（または受付状態が不明な）送信先がある、`2` 入力・設定・事前チェックのエラー（何も送信していない）、`130` 中断。

すべてのコマンドは [docs/cli.md](docs/cli.md)（英語）を参照してください。

## 設定と secrets

```toml
# ~/.config/kjak/config.toml
[feathers.development_slack]
type = "slack"
webhook_url = { secret = "development_slack.webhook_url" }

[groups.development]
feathers = ["development_slack"]
```

秘密情報は、自分専用（モード 0600。グループやその他が読めるファイルは拒否）の secrets ファイルに置き、設定ファイルからは名前で参照します。

```toml
# ~/.config/kjak/secrets.toml
[development_slack]
webhook_url = "https://hooks.slack.com/services/..."
```

`{ env = "VAR" }` と `{ keychain = { service = "...", account = "..." } }` も使えます。設定ファイルに秘密情報を書く `{ literal = "..." }` は引き続き動作しますが非推奨です。`kjak config migrate-secrets` で新しい secrets ファイルへ移せます（[移行ガイド](docs/migration.md)、英語）。

| | 設定 | secrets |
|---|---|---|
| explicit（明示） | `--config FILE`、`$KJAK_CONFIG` | `--secrets FILE`、`$KJAK_SECRETS` |
| user（ユーザー） | `~/.config/kjak/config.toml` | `~/.config/kjak/secrets.toml` |
| current（カレント） | `./.kjak.toml`（Trust 後） | `./.kjak.secrets.toml`（`.kjak.toml` と一緒に Trust され、そのためだけに使う） |

explicit のファイルは同じ種類の user のファイルの代わりに使われ、種類ごとに独立して選ばれます。使われているファイルと選択元、状態は `kjak config path` で確認できます。プロジェクト設定が**あなたの** secrets ファイルから読む名前は、`kjak config trust` のときに名前ごとに許可する必要があります。詳しくは[設定リファレンス](docs/configuration_ja.md#7-認証情報とセキュリティ)を参照してください。

## HTTP API / Webhook feather

`http` feather は任意の HTTP API に送ります。メソッド、ヘッダー、クエリ、認証（bearer、basic、任意のヘッダー。秘密情報は参照で指定）、JSON またはテキストの本文（テンプレートも可）を設定できます。

```toml
[feathers.ops_api]
type = "http"
url = "https://api.example.com/v1/alerts"

[feathers.ops_api.auth]
type = "bearer"
token = { secret = "ops_api.token" }

[feathers.ops_api.body]
template = '{"summary": "[{{.level}}] {{.title}}", "details": "{{.body}}"}'
```

既定では `https` のみ（`allow_http` で http）、リダイレクトは追従せず（`max_redirects` で許可）、ループバック・リンクローカル・プライベート・クラウドのメタデータのアドレスには、設定でネットワークを許可しない限り接続しません。アドレスは接続する時点で検査するので、DNS リバインディングでは回避できません。`POST` と `PATCH` は、冪等性キー（または方針）で許可しない限り再試行しません。詳しくは [HTTP feather ガイド](docs/http.md)（英語）を参照してください。

## 外部プラグイン

プラグインは、`PATH` 上にある `kjak-feather-<name>` という名前の実行ファイルです。kjak は、それを指定した feather があるときだけ実行します。

```toml
[feathers.pager]
type = "external"
plugin = "pagerduty"                     # kjak-feather-pagerduty を実行
secrets = { routing_key = { secret = "pagerduty.routing_key" } }
```

プラグインは標準入力で JSON のリクエスト（メッセージ、設定値、渡すと指定した秘密情報のみ）を1つ受け取り、標準出力に JSON の応答を1つ返します（プロトコル v1）。**プラグインはあなたの権限で動きます**。信頼できるプラグインだけをインストールしてください。kjak は絶対パスの `PATH` ディレクトリだけを探し、あいまいな実行ファイルや他者が書き換えられる実行ファイルを拒否し、実行のたびにファイルを再検査し、最小限の環境変数だけを渡し、タイムアウトでプラグインを終了させます。`kjak feather plugins` は何も実行せずに `PATH` 上のプラグインを一覧します。詳しくは[プラグインガイド](docs/plugins.md)（英語。作り方も含む）を参照してください。

## セキュリティ

- 秘密情報は参照（secrets ファイル、環境変数、キーチェーン）で、送信時に解決し、表示せず、すべてのエラーとレポートから取り除きます。
- リポジトリの `.kjak.toml`（と `.kjak.secrets.toml`）は `kjak config trust` の後にだけ使い、変更されると Trust は取り消されます。あなたの秘密情報を読むこと、http feather やプラグインにあなたの環境変数を渡すことは、許可した名前に限ります。
- http feather：https のみ、リダイレクトなし、ループバック・プライベート・リンクローカル・クラウドのメタデータのアドレスには許可がない限り接続しない（接続時に検査）、プロキシは使わない。プロジェクトの slack、discord、email の feather にも同じアドレスの制限がかかります。
- プラグインはあなたの権限で動きます。kjak はどの実行ファイルを動かすか（絶対パスの `PATH` のみ、実行のたびに検査）と何を渡すか（最小限の環境変数、自身の秘密情報のみ）を制御しますが、プラグインの動作そのものは制御しません。

詳しくは [docs/security.md](docs/security.md)（英語）を参照してください。脆弱性は [SECURITY.md](SECURITY.md) の方法で非公開に報告してください。

## 既知の制限

- feather：OS の通知（`os`：macOS、Windows、Linux）、Slack（Incoming Webhook）、Discord（Webhook）、メール（SMTP）、HTTP API（`http`）、プラグイン（`external`）。OS の通知は「スクリプトエディタ」（macOS）や「Windows PowerShell」（Windows）からの通知として表示され、デスクトップのセッションが必要です。`confirmed` は OS が受け付けたことを意味し、画面で見たことまでは意味しません。
- OS のキーチェーン：macOS（`security`）と Linux（`secret-tool`、libsecret）は読み取りのみ対応。**Windows はキーチェーンに対応していません**（`env` を使ってください）。
- group は入れ子にできません。
- 設定を変更するコマンドがコメントを保つのは、新しい feather、group、template を追加するときだけです。それ以外の変更（`remove`、`--force`、`enable` など）はコメントなしで書き直し、その旨を表示します。
- プラグインはサンドボックス化されません（あなたの権限で動きます）。タイムアウト、送信の期限、Ctrl-C のときは、プラグインとそれが起動したすべてのプロセスを終了させます（Unix ではプロセスグループ、Windows では Job Object 単位）。

詳しくは [docs/troubleshooting.md](docs/troubleshooting.md)（英語）を参照してください。

## ドキュメント

- [導入ガイド](docs/getting-started_ja.md)（[English](docs/getting-started.md)）：インストール、デスクトップ、Slack、Discord、メール、利用例
- [チュートリアル](docs/tutorials/README_ja.md)（[English](docs/tutorials/README.md)）：feather ごと
- [設定例](docs/examples/README.md)
- [設定リファレンス](docs/configuration_ja.md)（[English](docs/configuration.md)）：設定キー、CLI による管理、認証情報、Trust、配信の動作
- 以下は英語のみです：[CLI usage](docs/cli.md)、[Stability and compatibility (v1)](docs/stability.md)、[Credentials, trust and security](docs/security.md)、[Migrating to v1.0.0](docs/migration.md)、[HTTP API / webhook feathers](docs/http.md)、[External feather plugins](docs/plugins.md)、[Delivery, retry and acceptance semantics](docs/delivery.md)、[Discord end-to-end check](docs/discord-e2e.md)、[Development guide](docs/development.md)、[Release runbook](docs/release.md)、[Troubleshooting](docs/troubleshooting.md)

## ライセンス

MIT
