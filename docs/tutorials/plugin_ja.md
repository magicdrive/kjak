# plugin（external feather）

[English](plugin.md) · [チュートリアル一覧](README_ja.md) · [リファレンス: plugin](../plugins.md)

このチュートリアルでは、小さな plugin `kjak-feather-log` をインストールし、`external` feather として登録して、最初の通知を送ります。そのうえで、kjak が plugin に何を渡し何を受け取るか（protocol v1）、kjak が plugin をどう止めるか、秘密情報の渡し方、信頼の境界、Windows での違いを説明します。protocol の全体はリファレンス [plugins.md](../plugins.md) にあります。

## 1. 概要

plugin は `kjak-feather-<name>` という名前の実行ファイルで、kjak が知らない場所にメッセージを届けます。type が `external` の feather がそれを指定します。

```toml
[feathers.log]
type = "external"
plugin = "log"                                   # kjak-feather-log を実行する
settings = { file = "/home/me/kjak-notifications.log" }
```

- kjak は plugin を **試行ごとに 1 回**、引数なしで実行します。plugin の標準入力に JSON の **request** を 1 つ書き（メッセージ、feather の `settings`、あなたが渡す秘密情報）、標準出力から JSON の **response** を 1 つ読みます（`delivered` か `failed`）。これが protocol v1 です。
- kjak は、*どの* 実行ファイルを動かすか（絶対パスの `PATH` ディレクトリだけ、ファイルの権限の検査、任意の固定）と、*何を渡すか*（最小限の環境、新しい作業ディレクトリ、その feather の秘密情報だけ）を決めます。また、timeout のときに plugin とそれが起動したものをすべて止めます。
- **plugin は sandbox されません。** plugin はあなたの権限で動きます。あなたと同じように、ファイルや鍵を読み、ネットワークを使えます。plugin は、ほかのプログラムをインストールするときと同じ注意でインストールしてください。

このチュートリアルの plugin [kjak-feather-log](../examples/plugins/kjak-feather-log) は POSIX シェルのスクリプトです。メッセージを 1 行の JSON として、設定 `file` が指すファイルに追記します。

## 2. 前提条件

- kjak がインストールされていること（`kjak version`）。[導入ガイド](../getting-started_ja.md#2-インストール)を参照してください。
- macOS、Linux、またはその他の Unix で、`/bin/sh` と標準の `cat`、`tr`、`grep`、`sed`、`date` があること。例の plugin は `jq` を必要としません。
- 自分のプログラム用のディレクトリが **絶対パスで** `PATH` に入っていて、あなたの所有で、グループやほかの人が書き込めないこと。`~/.local/bin` や `~/bin` がたいていそうです。kjak は空の要素や相対パスの要素（`bin`、`.`）を探さないので、`cd` したリポジトリが plugin を差し込むことはできません。
- **Windows**: plugin は `.exe` ファイルでなければなりません。スクリプト（`.ps1`、`.cmd`、`.bat`、`.sh`）は実行されません。このチュートリアルのシェルの plugin は Windows では動かないので、Go の plugin をビルドしてください（7 章の *Windows*）。

## 3. 準備

kjak のリポジトリのチェックアウトから plugin をインストールします（ファイルは `docs/examples/plugins/kjak-feather-log` です）。

```sh
mkdir -p ~/.local/bin
install -m 755 docs/examples/plugins/kjak-feather-log ~/.local/bin/kjak-feather-log
```

`~/.local/bin` がまだ `PATH` にないなら、シェルのプロファイル（`~/.profile`、`~/.zshrc` など）に、絶対パスに展開される形で加えます。

```sh
export PATH="$HOME/.local/bin:$PATH"
```

（引用符で囲んだ `"~/.local/bin"` は `~` のまま残り、相対パスの要素になるので、kjak は探しません。）

kjak から見えることを確認します。`kjak feather plugins` は `PATH` のディレクトリにある `kjak-feather-*` のファイルを一覧にし、どれも実行しません。

```sh
kjak feather plugins
```

```
PLUGIN  PATH                                   STATE  FEATHERS
log     /home/me/.local/bin/kjak-feather-log   ok     -
A plugin runs only for a feather of type "external" that names it (docs/plugins.md).
```

`ok` は、ファイルが検査を通ったことを意味します。あなた（または root）が所有する通常の実行ファイルで、グループやほかの人が書き込めず、ほかの人が変更できないディレクトリにある、ということです。検査に通らないファイルは `refused: <理由>` と表示され、kjak が飛ばした `PATH` の要素は `not searched:` の行に出ます。plugin が見つかっただけでは何も起きません。plugin を指定した feather があるときだけ実行されます。

plugin の名前は、小文字の ASCII 英字、数字、`_`、`-`（48 文字まで）で、組み込みの type の名前（`slack`、`email`、`http`、`os` など）にはできません。

## 4. 最小構成

完全な `~/.config/kjak/config.toml` です。`file` にはあなたの絶対パスを書いてください（TOML は `~` を展開しません。相対パスは plugin の作業ディレクトリからの相対になり、その作業ディレクトリは実行のたびに新しく作られ、終わると削除されます）。

```toml
# ~/.config/kjak/config.toml
[feathers.log]
type = "external"
plugin = "log"
settings = { file = "/home/me/kjak-notifications.log" }
timeout = "5s"
```

| キー | 意味 |
|---|---|
| `plugin` | 名前。kjak は絶対パスの `PATH` ディレクトリから `kjak-feather-log` を実行する |
| `settings` | request で plugin に渡す平文の文字列（キーは小文字の英字、数字、`_`） |
| `timeout` | 1 回の実行にかけてよい時間（既定 `10s`、最大 `10m`）。過ぎると kjak が止める |

## 5. CLI での設定

`kjak feather add` は external feather を作らず、次のように案内します。

```
kjak: feather add: an external feather is configured in the file ([feathers.log] type = "external", plugin = "NAME", docs/plugins.md); kjak feather plugins lists what PATH has
```

feather はファイルに書き（4 章）、CLI で確認します。

```sh
kjak config validate
kjak feather plugins
kjak feather show log
kjak feather plugins -o json
```

- `config validate` はキーを検査します（予約された plugin 名、誤った `settings` のキー、`path must be absolute` など）。
- `feather plugins` の `FEATHERS` 列に `[log]` が出るようになります。`-o json` では各 plugin のパスと SHA-256 がわかります（固定に使います。7 章）。
- `feather show log` は plugin の名前と秘密情報の読み取り先を表示し、値は表示しません。

`kjak feather disable log`、`enable`、`remove` は、どの type でも同じように使えます。

## 6. 最初の通知

plugin を実行せずに、すべてを解決します。

```sh
kjak send --to log --dry-run "test"
```

```
plan  log (external)
dry run: 1 target(s), 0 skipped; nothing was sent
```

dry run は実行ファイルを見つけて検査し、秘密情報を解決しますが、plugin は起動しません。次に送ります。

```sh
kjak send --to log --title "Deploy" --level success "api 1.4.2 is live"
```

```
ok    log (external) delivered in 37ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

ファイルには、通知ごとに 1 行が追記されています。

```
{"time":"2026-10-30T12:16:14Z","idempotency_key":"38e2a504ce9431dd8eaf9620c500b156","message":{"title":"Deploy","body":"api 1.4.2 is live","level":"success","url":""}}
```

`-o json` は、kjak が response をどう理解したか（`status`、`acceptance`、エラーとそのコード）を示します。

```sh
kjak send --to log -o json "second"
```

### plugin を手で動かす

plugin はふつうのプログラムです。標準入力に request を渡し、答えを読みます。`env -i` を使うと、kjak と同じくらい小さな環境で動かせます。

```sh
printf '%s' '{"protocol_version":1,"feather":"log","plugin":"log","attempt":1,"idempotency_key":"k1","message":{"title":"","body":"hello","level":"info","url":""},"settings":{"file":"/tmp/kjak-plugin-test.log"},"secrets":{}}' | env -i PATH=/usr/bin:/bin KJAK_PLUGIN_PROTOCOL=1 ~/.local/bin/kjak-feather-log; echo "exit $?"
```

```
{"protocol_version":1,"status":"delivered"}
exit 0
```

`"file":"relative.log"` にすると `failed` / `rejected`（`the setting file must be an absolute path`）と答え、kjak はそれを「確実に何も届けていない失敗」として報告します。

## 7. 応用的な設定

### request（protocol v1）

kjak は plugin の標準入力に JSON のオブジェクトを 1 つ書き、閉じます（実際は 1 行の詰めた形です。ここでは字下げして示します）。

```json
{
  "protocol_version": 1,
  "feather": "log",
  "plugin": "log",
  "attempt": 1,
  "deadline": "2026-10-30T12:28:35.742961Z",
  "timeout_ms": 5000,
  "idempotency_key": "1d14879dc6d597eb7a2fdb25d6ce2856",
  "message": {"title": "Deploy", "body": "api 1.4.2 is live", "level": "success", "url": "https://ci.example/7"},
  "settings": {"file": "/home/me/kjak-notifications.log"},
  "secrets": {"token": "dummy-token"}
}
```

- `attempt` は 1 回の送信の中での試行の番号です。`idempotency_key` は 1 回の送信のすべての試行で同じで、次の送信では新しくなります（再試行で 2 回届けないために使います）。
- `deadline` は kjak が plugin を止める時刻です（その `timeout` と送信の期限の早いほう）。それより前に終えてください。
- `message` には常に 4 つの文字列があります（設定されていなければ空）。`level` は `info`、`success`、`warning`、`error`、`critical` のいずれかです。
- `secrets` には、feather の `secrets` の表にある秘密情報 **だけ** が入ります（8 章）。v1 のうちにフィールドが増えることがあります。知らないものは無視してください。

### response と終了コード

標準出力に JSON のオブジェクトを 1 つ書き、終了コード 0 で終わります。

```json
{"protocol_version": 1, "status": "delivered"}
```

```json
{"protocol_version": 1, "status": "failed", "acceptance": "rejected", "retryable": true,
 "retry_after_ms": 2000, "status_code": 429,
 "error": {"code": "rate_limited", "message": "too many requests"}}
```

| plugin が | kjak の報告 |
|---|---|
| 終了コード 0 で `"status": "delivered"` | 配信済み（`acceptance: confirmed`） |
| 終了コード 0 で `"failed"` と `"acceptance": "rejected"` | 失敗、確実に届いていない。`retry.policy = "plugin"` かつ `"retryable": true` のときだけ再試行 |
| 終了コード 0 で `"failed"` と `"acceptance": "unknown"`（または指定なし） | 失敗、**届いた可能性がある**（`????`）。再試行しない |
| 0 以外の終了コード、クラッシュ、kill | 何を出力していても、失敗、届いた可能性がある。stderr の末尾がエラーに入る（`the plugin exited with status 3; it may have delivered; stderr: ...`） |
| JSON を出さない、2 つ出す、1 MiB を超える、`protocol_version` がないか違う | protocol エラー、届いた可能性がある |
| 起動できない | 失敗、届いていない（何も動いていない） |

`rejected` は、メッセージが確実に届いていないときだけ使います。迷ったら `unknown` にします。標準エラー出力は診断用です（kjak は 64 KiB まで保持し、末尾の 300 文字を整えて秘密情報を伏せたうえで表示します）。`error.code` の値とすべてのフィールドは [plugins.md](../plugins.md#response-standard-output) にあります。

### timeout、cancel、後片付け

| | |
|---|---|
| timeout | feather の `timeout`（既定 10s）と送信の期限（`--timeout`）。過ぎると kjak が plugin を kill し、結果は `the plugin did not finish within its timeout of 5s; it was stopped and may have delivered` |
| cancel | kjak への Ctrl-C（SIGINT）、SIGTERM、SIGHUP でも plugin を kill する。kjak は 130 で終わり、結果を報告する（`interrupted while the plugin ran; ...`） |
| 後片付け | Unix では plugin は専用のプロセスグループで動き、kjak はグループ全体を kill する。timeout、cancel のときに加えて、plugin が終了した後にも kill するので、plugin が起動したものは残らない。Windows では plugin は専用の Job Object で動き、同じ場面で Job Object ごと終了させる |
| 対象外 | Unix で kjak 自身が SIGKILL で殺された（またはクラッシュした）とき、グループを kill する者はいない。グループを抜けた子（`setsid`）や、別のサービス（`launchd`、`systemd --user`、`at`）経由で起動したものは残る |
| 作業ディレクトリ | 試行ごとに新しく作る空のディレクトリで、あなただけが書き込め、試行の後に削除される。プロジェクトでも、`/tmp` そのものでもない |
| 環境変数 | `PATH`（絶対パスのディレクトリだけ）、`HOME`、`TMPDIR`、`TEMP`、`TMP`、`LANG`、`LC_ALL`、`LC_CTYPE`、`TZ`、`SYSTEMROOT`、`USERPROFILE`（設定されているもの）、feather の `env` に挙げた名前、`KJAK_PLUGIN_PROTOCOL=1` だけ |
| 並行実行 | 別々の feather は同時に動くので、1 つの plugin が同時に何回か動くことがある。1 つの feather の試行が重なることはない |

バックグラウンドで起動した子プロセスが、plugin の標準出力や標準エラー出力を開いたままにしてはいけません（`cmd >/dev/null 2>&1 &`）。kjak はそれらが閉じるまで読み、plugin の終了後は最大 2 秒待ちます。

### 完全な設定

```toml
# ~/.config/kjak/config.toml
[feathers.log]
type = "external"
plugin = "log"
# 実行ファイルを固定する: このファイルだけを、この内容のときだけ実行する（kjak feather plugins -o json で両方わかる）
path = "/home/me/.local/bin/kjak-feather-log"
sha256 = "85abe0775af1f9fb009b8743c73b1dcab8e600c937abc7d3ccc91bc9c77700c8"
settings = { file = "/home/me/kjak-notifications.log" }
env = ["TZ"]                       # 最小限の環境変数のほかに見せる変数
timeout = "5s"
levels = ["warning", "error", "critical"]

[feathers.log.retry]
policy = "plugin"                  # "rejected" + "retryable" の response を再試行する
max_attempts = 3

[feathers.pager]
type = "external"
plugin = "pagerduty"
settings = { service = "api", severity_map = "error=high" }
secrets = { routing_key = { secret = "pagerduty.routing_key" } }
timeout = "5s"

[groups.alerts]
feathers = ["log", "pager"]
```

- **固定**: `path` があると、kjak は `PATH` を探しません。`sha256` があると、実行のたびにファイルのハッシュを計算し、内容が変わったものを拒否します。どちらもなくても、kjak は feather を組み立てるときにファイルの同一性を記録し、実行の前に差し替えられたファイルを拒否します。固定した plugin を更新したら、`sha256` も更新します（`shasum -a 256 FILE`、`sha256sum FILE`）。
- **再試行**: 既定（`retry.policy = "never"`）では、plugin は送信 1 回につき 1 回だけ動きます。起動しただけで何かが起きているかもしれないからです。`"plugin"` にすると、`rejected` + `retryable` の response をエンジンが再試行します（バックオフ、`retry_after_ms`、期限、`max_retries`）。`attempt` は増え、`idempotency_key` は同じです。
- **`env`** には、`KJAK_*`、`LD_*`、`DYLD_*`、`GODEBUG`、`GOTRACEBACK`、feather の秘密情報の読み取り先になっている変数を挙げられません（その秘密情報は request で渡ります）。

### 自分で書く

[kjak-feather-log](../examples/plugins/kjak-feather-log) をひな形として読んでください。このスクリプトは、動く前に request 全体を読み、`protocol_version` を確かめ、設定を検査し（絶対パスで、JSON のエスケープがないこと）、ファイルを開きます。ここまでは何も書き込んでいない段階なので、ここでの失敗は `rejected` です。その後で書き込み、書き込みに失敗したら `unknown` と答えます。request そのものは書かないので、request の秘密情報がログに入ることはありません。JSON の解析は、JSON の文字列を考慮した `sed` のパターンで行っていて、必要な値を取り出すには足ります。もっと多くを扱う plugin では、本物の JSON パーサー（Go のプログラム、Python、`jq`）を使ってください。そのほかの指針は [plugin の書き方](../plugins.md#writing-a-plugin)にあります。

### Windows

Windows では plugin は `kjak-feather-<name>.exe` です。kjak はそれ以外のファイルを拒否し（`not an .exe file`）、スクリプトは実行しません。[plugins.md](../plugins.md#writing-a-plugin) の Go の plugin（標準ライブラリだけ。これも設定 `file` のファイルにメッセージを書きます）を、feather が使う名前でビルドします。

```powershell
go build -o kjak-feather-log.exe .
```

置き場所は、プロファイルの下（たとえば `%LOCALAPPDATA%\Programs\kjak-plugins`）か `Program Files` の下の、`PATH` に入っているディレクトリにします。`C:\` の直下に作ったフォルダーは `Authenticated Users: Modify` を継承するので、そこにある plugin を kjak は拒否します。ファイルとそのディレクトリは、あなた、SYSTEM、Administrators、TrustedInstaller のいずれかが所有し、ほかの principal が変更できない状態にします。設定は同じで、`settings` には Windows のパスを書きます（`file = 'C:\Users\me\kjak-notifications.log'`。バックスラッシュをそのまま残すため、TOML のリテラル文字列にします）。

## 8. 秘密情報

plugin が秘密情報を受け取るのは、feather の `secrets` がそれを、plugin が知っている名前で指定したときだけです。

```toml
# ~/.config/kjak/config.toml
[feathers.pager]
type = "external"
plugin = "pagerduty"
settings = { service = "api" }
secrets = { routing_key = { secret = "pagerduty.routing_key" }, api_token = { env = "PAGERDUTY_TOKEN" } }
```

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[pagerduty]
routing_key = "R0ut1ngK3y-replace-me"
```

- 値はどれも認証情報の参照（`secret`、`env`、`keychain`）で、送信のときに解決され、request の `secrets` オブジェクトに入ります（`{"routing_key": "...", "api_token": "..."}`）。
- kjak は秘密情報を plugin の環境変数や引数に入れません。secrets file、環境変数、キーチェーンのほかのものを渡すこともありません。
- kjak は、渡した秘密情報を plugin のメッセージと stderr から伏せてからレポートに載せます。ただし、plugin がそこから作ったもの（エンコードした形、一部分）は伏せられません。**plugin は秘密情報を決して出力してはいけません。**
- 解決できない秘密情報があると、plugin を動かす前に送信を止めます（終了コード 2）。
- 例の `kjak-feather-log` は秘密情報を使いません。渡しても無視し、書き出すことはありません。

## 9. トラブルシューティング

| メッセージ | 原因 | 対処 |
|---|---|---|
| `plugin error: kjak-feather-log is not on PATH (absolute directories only)`（終了コード 2） | インストールされていない、または相対パスや空の `PATH` 要素にしかない | 絶対パスの `PATH` ディレクトリにインストールする（3 章）か、`path =` を設定する |
| `not searched: PATH element "bin" (a relative directory)`（`feather plugins`） | `PATH` に相対パスの要素がある | `PATH` には絶対パスのディレクトリを使う |
| `plugin error: ...kjak-feather-log: writable by group or others (mode 0775)` | ファイル（またはディレクトリ）をほかの人が変更できる | ファイルを `chmod 755`、ディレクトリを `chmod go-w` |
| `... is found in several PATH directories as different files` | 2 か所にインストールされている | 一方を削除するか、`path =` で固定する |
| `plugin name "slack" is reserved` | 組み込みの type と同じ名前の plugin | plugin の名前を変える |
| `feather add: an external feather is configured in the file` | `feather add --type external` | ファイルに `[feathers.NAME]` を書く（4 章） |
| `plugin: the setting file must be an absolute path` | log plugin が相対パスの `file` を拒否した | 絶対パスを書く |
| `the plugin did not finish within its timeout of 5s; it was stopped and may have delivered`（`????`） | plugin が遅い、または入力を待っている | `timeout` を延ばす。送り直す前に届け先を確認する |
| `the plugin exited with status 3; it may have delivered; stderr: ...` | plugin が失敗またはクラッシュした | メッセージの stderr を読み、plugin を直す |
| `protocol error: no response on stdout; the plugin may have delivered` | 何も出力しなかった、または JSON のオブジェクト 1 つではなかった | JSON のオブジェクトを 1 つ返す（7 章）。診断は stderr に出す |
| `plugin error: ... has SHA-256 ..., the configuration pins ...`（終了コード 2） | ファイルが `sha256` と一致しない（更新した、または別のファイル） | ファイルを確認する。更新したなら `sha256` を更新する |
| `the plugin was not run: ... changed since it was checked`（`blocked`） | 検査と実行の間にファイルが差し替えられた | 何が差し替えたかを調べ、もう一度実行する |
| `its plugin log is not the executable that was trusted`（`untrusted_project`） | プロジェクトの plugin が更新または差し替えられた | 確認してから `kjak config trust` |
| `not an .exe file`（Windows） | スクリプト、または `.exe` でないファイル | `.exe` をビルドする（7 章の *Windows*） |

詳しくは [トラブルシューティング](../troubleshooting.md) を参照してください。

```sh
kjak feather plugins
kjak config validate
kjak send --to log --dry-run "test"
kjak send --to log -o json "test"
```

## 10. セキュリティ上の注意

- **sandbox はありません。** plugin はあなたの権限で動き、あなたにできることは何でもできます。kjak が制御するのは、どのファイルを動かすか、何を渡すか、実行の終わりに plugin のプロセスを止めることです。実行中に plugin が何をするかは制限しません。
- **見つかることは許可ではありません。** `PATH` にある `kjak-feather-*` は、それを指定した feather があるときだけ実行されます。`kjak feather plugins` は何も実行しません。`type = "slack"`（やほかの組み込みの type）は常に組み込みの feather を動かします。
- **PATH の乗っ取り。** 探すのは絶対パスの `PATH` ディレクトリだけです。同じ名前が別のファイルとして 2 つ見つかるとエラーです。拒否された候補はエラーになり、後ろの候補に飛ばすことはしません。
- **プロジェクトは Trust で固定されます。** `.kjak.toml` で external feather を登録できますが、`kjak config trust` が各 plugin を解決し、そのパス、SHA-256、settings、秘密情報、環境変数を表示して、パスと SHA-256 を記録します。その後、plugin が別のファイルに解決されたり内容が変わったりすると、実行はその feather を拒否します（`untrusted_project`）。更新したら `kjak config trust` をやり直します。Trust するときに plugin がインストールされている必要があります。プロジェクトの plugin の `env` と `keychain` の秘密情報、および `env` の変数は、名前ごとに許可する必要があります。
- **settings は入力です。** プロジェクトの `settings` はリポジトリから来ます。plugin はそれを検査しなければなりません（例の plugin は相対パスやエスケープを含むパスを拒否します）。
- **TOCTOU の限界。** kjak はファイルを検査してからパスで実行します。その一瞬にファイルを差し替えるには、検査によってあなたと root だけに限られている書き込み権限が必要です。
- 詳細: [セキュリティ: external plugin](../security.md#external-plugins) と [OS ごとの違い](../security.md#plugin-processes-and-files-per-os)。

## 11. 関連資料

- [plugin](../plugins.md) — protocol v1、名前、探索、プロセス（このチュートリアルのリファレンス）
- [protocol v1](../plugins.md#protocol-v1) と [プロセス](../plugins.md#the-process)
- [プロジェクトと Trust](../plugins.md#projects)
- [設定: external](../configuration_ja.md#external) — キーの表
- [セキュリティ: external plugin](../security.md#external-plugins)
- [設定例: plugin-only.toml](../examples/plugin-only.toml) と [kjak-feather-log](../examples/plugins/kjak-feather-log)
- [トラブルシューティング](../troubleshooting.md)
