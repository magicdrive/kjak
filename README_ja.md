# kjak

[English](README.md) · [導入ガイド](docs/getting-started_ja.md) · [設定リファレンス](docs/configuration_ja.md)

kjak は、1つの通知を複数の通知先（Slack、Discord、メール）へ送るコマンドラインツールです。ターミナル、スクリプト、CI から使えます。通知先1つ1つを **feather（フェザー）** と呼び、feather は **group（グループ）** にまとめられます。

```sh
kjak send --to development --level error --title "nightly build" "tests failed on main"
```

- 明示的なサブコマンド：`send`、`feather`、`group`、`config`、`template`、`version`
- ベストエフォートの配信：feather ごとに独立して配信し、同時に最大 5 件。再試行を含めて1つの期限（10 秒）に収める
- 正直な結果報告：サービスが受け付けを確認したときだけ**配信済み（confirmed）**とする。届いたかもしれないが応答がなかったメッセージは**不明（unknown）**とし、重複を避けるため再試行しない
- 秘密情報は環境変数、OS のキーチェーン、または非公開の secrets ファイルから読み、表示せず、設定ファイルに置かない
- プロジェクト設定（`.kjak.toml`）は Trust した後にだけ使い、あなたの秘密情報は許可した名前のものしか読まない
- テキストまたは JSON（`schema_version: 1`）で出力。終了コードの仕様は固定
- macOS、Linux、Windows 向けの単一の静的 Go バイナリ（`CGO_ENABLED=0`）

## インストール

```sh
brew install magicdrive/tap/kjak
```

または [GitHub Releases](https://github.com/magicdrive/kjak/releases) からアーカイブ（macOS / Linux は `.tar.gz`、Windows は `.zip`）をダウンロードし、確認してください。

```sh
sha256sum -c kjak_checksums.txt --ignore-missing
gh attestation verify kjak_0.0.2_linux_amd64.tar.gz --repo magicdrive/kjak
```

ソースから（Go 1.27）：`go install github.com/magicdrive/kjak@latest`

対応プラットフォーム：darwin/arm64、darwin/amd64、linux/arm64、linux/amd64、windows/amd64

詳しい手順は[導入ガイド](docs/getting-started_ja.md#2-インストール)を参照してください。

## クイックスタート

Slack、Discord、メールそれぞれの手順は[導入ガイド](docs/getting-started_ja.md)で詳しく説明しています。

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

## 既知の制限

- feather：Slack（Incoming Webhook）、Discord（Webhook）、メール（SMTP）。**OS ネイティブの通知には対応していません**。種類 `os` の feather は設定エラーになります。
- OS のキーチェーン：macOS（`security`）と Linux（`secret-tool`、libsecret）は読み取りのみ対応。**Windows はキーチェーンに対応していません**（`env` を使ってください）。
- group は入れ子にできません。
- 設定を変更するコマンド（`feather add`、`group set` など）は、設定ファイルをコメントなしで書き直します。
- プラグインはありません。feather はすべて組み込みです。

詳しくは [docs/troubleshooting.md](docs/troubleshooting.md)（英語）を参照してください。

## ドキュメント

- [導入ガイド](docs/getting-started_ja.md)（[English](docs/getting-started.md)）：インストール、Slack、Discord、メール、利用例
- [設定リファレンス](docs/configuration_ja.md)（[English](docs/configuration.md)）：設定キー、CLI による管理、認証情報、Trust、配信の動作
- 以下は英語のみです：[CLI usage](docs/cli.md)、[Credentials, trust and security](docs/security.md)、[Migrating literal secrets](docs/migration.md)、[Delivery, retry and acceptance semantics](docs/delivery.md)、[Development guide](docs/development.md)、[Release runbook](docs/release.md)、[Troubleshooting](docs/troubleshooting.md)

## ライセンス

MIT
