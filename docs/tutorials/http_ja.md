# HTTP API と Webhook

[English](http.md) · [チュートリアル一覧](README_ja.md) · [リファレンス: HTTP feather](../http.md)

このチュートリアルでは、`http` feather を使って、kjak のメッセージを任意の HTTP API や Webhook に送ります。リクエスト（メソッド、URL、ヘッダー、認証、本文）の組み立て、ローカルの mock server での最初のテスト、実際の API への送信、再試行・テンプレート・プライベートネットワークのオプションを説明します。規則のすべてはリファレンス [http.md](../http.md) にあります。このページは、実際に使えるようになるための手順です。

## 1. 概要

`http` feather は、1 回の試行ごとに 1 つの HTTP リクエストを送ります。リクエストの形はあなたが決めます。

```toml
[feathers.api]
type = "http"
url = "https://api.example.com/v1/notifications"

[feathers.api.auth]
type = "bearer"
token = { secret = "api.token" }
```

- **リクエスト**: `method`（既定は `POST`）、`url`、`headers`、`query`、`[auth]`（bearer、basic、ヘッダー）、`[body]`。本文の既定はメッセージの JSON `{"title", "body", "level", "url"}` で、独自の JSON やテキストのテンプレートにもできます。
- **応答**: `success_status`（既定は `2xx`）に含まれるステータスなら `delivered` です。429、408、5xx は一時的な拒否です。502 と 504 は、リクエストが届いた可能性があることを意味します（`acceptance: unknown`。再試行しません）。
- **再試行** はメソッドで決まります。`GET`、`PUT`、`DELETE` は再試行します。`POST` と `PATCH` は `idempotency_key = true` のときだけ再試行し、そのとき 1 回の送信のすべての試行に同じ `Idempotency-Key` を送ります。
- **宛先の制限**（SSRF ガード）: feather で許可しない限り（`allow_http`、`allow_networks`、`allow_hosts`）、`https` だけ、公開アドレスだけに接続します。リダイレクトはたどらず、プロキシの環境変数も使いません。
- `slack` や `discord` の type とは別物です。あちらはサービス固有の本文を知っています。それ以外（Teams、Google Chat、Mattermost、PagerDuty、自前のサービスなど）には `http` を使います。

## 2. 前提条件

- kjak がインストールされていること（`kjak version`）。[導入ガイド](../getting-started_ja.md#2-インストール)を参照してください。
- 呼び出す API のドキュメント。URL、メソッド、認証の方式、期待する本文、成功時のステータス、idempotency key に対応しているかを確認します。
- API が必要とするなら、その認証情報（トークン、パスワード、API キー）。
- 6 章のローカルの mock server 用の `python3`（任意ですが、kjak が何を送るかを見るのにいちばん安全な方法です）。
- `https` で API に接続できること。プロキシは使えません。http feather は直接接続します。

## 3. 準備

1. API のドキュメントから、次を書き出します。

   | 必要なもの | 書く場所 |
   |---|---|
   | URL | `url`（URL 自体が認証情報のとき、たとえばパスにトークンを含む Webhook URL なら `url_secret`） |
   | メソッド | `method`（特に指定がなければ `POST`） |
   | 認証 | `[auth]`: `bearer`（`Authorization: Bearer ...`）、`basic`、`header`（`X-Api-Key: ...`） |
   | 本文 | `[body]`: 既定の JSON、JSON のテンプレート、テキスト |
   | 成功のステータス | `success_status`（既定は `["2xx"]`） |

2. 認証情報を **secrets file** に置きます。何かを書き込む前に、非公開（モード 0600）で作成します。

   ```sh
   mkdir -p ~/.config/kjak
   [ -e ~/.config/kjak/secrets.toml ] || install -m 600 /dev/null ~/.config/kjak/secrets.toml
   "${EDITOR:-vi}" ~/.config/kjak/secrets.toml
   ```

   ```toml
   # ~/.config/kjak/secrets.toml（モード 0600。ダミーの値を置き換える）
   [api]
   token = "replace-me"
   ```

   Windows ではファイルは `%AppData%\kjak\secrets.toml` です。認証情報のほかの置き場所は 8 章を参照してください。

## 4. 最小構成

bearer トークンを使う JSON API 向けの、完全な `~/.config/kjak/config.toml` です。

```toml
# ~/.config/kjak/config.toml
[feathers.api]
type = "http"
url = "https://api.example.com/v1/notifications"

[feathers.api.auth]
type = "bearer"
token = { secret = "api.token" }
```

kjak が送るリクエストは次のとおりです。

```
POST /v1/notifications HTTP/1.1
Host: api.example.com
User-Agent: kjak
Authorization: Bearer <the token>
Content-Type: application/json

{"title":"Deploy","body":"api 1.4.2 is live","level":"success"}
```

（メッセージにタイトルや URL がなければ、`title` と `url` は省かれます。）ほかの形を求める API にはテンプレートを使います（7 章）。

## 5. CLI での設定

http feather はキーが多く、コマンドラインのオプションでは表せません。`kjak feather add NAME --type http` は http feather を作らず、次のように案内します。

```
kjak: feather add: an http feather is configured in the file ([feathers.api] type = "http", docs/http.md); then run kjak config validate
```

feather はファイルに書き（どのファイルかは `kjak config path` が表示します）、CLI で確認します。

```sh
kjak config path
kjak config validate
kjak feather show api
kjak feather list
```

`kjak config validate` は誤ったキーをすべて挙げます（`url: must be https (http needs allow_http = true)`、行番号付きの未知のキーなど）。`feather show` は、メソッド、URL、認証情報の読み取り先、宛先の制限を表示し、トークンは表示しません。

```
name     api
origin   global
type     http
enabled  true
levels   all
method   POST
url      https://api.example.com/v1/notifications
auth     bearer from secret:api.token
```

ほかの feather コマンドは、どの type でも同じように使えます。

```sh
kjak feather disable api
kjak feather enable api
kjak feather remove api
```

## 6. 最初の通知

### ローカルの mock server で試す

本物の API を呼ぶ前に、kjak が何を送るかを正確に見ておきます。`python3 -m http.server` は `GET` にしか応答しません。次の mock server は、どのリクエストにも 200 を返し、その内容を表示します。

```python
# mock_server.py: answers every request with 200 and prints it
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        print(self.command, self.path)
        print(self.headers, end="")
        print(body.decode("utf-8", "replace"), flush=True)
        self.send_response(200)
        self.end_headers()

    do_PUT = do_PATCH = do_DELETE = do_GET = do_POST

HTTPServer(("127.0.0.1", 8080), Handler).serve_forever()
```

```sh
python3 mock_server.py      # 別の端末で。Ctrl-C で止まる
```

宛先の制限は、既定で `http://` とループバックアドレスを拒否します。そこでテスト用の feather では、このアドレスだけを許可します。スキームには `allow_http = true`、アドレスには `allow_networks = ["127.0.0.1/32"]` です（ループバックは `allow_networks` でしか許可できず、`allow_hosts` では許可できません）。

```toml
# ~/.config/kjak/config.toml
[feathers.mock]
type = "http"
url = "http://127.0.0.1:8080/notify"
allow_http = true
allow_networks = ["127.0.0.1/32"]
```

```sh
kjak send --to mock --title "Deploy" "api 1.4.2 is live"
```

```
ok    mock (http) delivered in 31ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

mock server は次のように表示します。

```
POST /notify
Host: 127.0.0.1:8080
User-Agent: kjak
Content-Length: 60
Content-Type: application/json
Connection: close

{"title":"Deploy","body":"api 1.4.2 is live","level":"info"}
```

`allow_networks` がなければ、同じ送信は接続の前に失敗します（`the request was not sent: address 127.0.0.1 is loopback: allow it with allow_networks`）。URL には `localhost` ではなく `127.0.0.1` を使ってください。`::1` にも解決される名前では、そのアドレスも許可する必要があります。

`allow_http` と `allow_networks` はテスト用の feather だけにとどめ、終わったら `mock` feather を削除してください。

### 本物の API に送る

4 章の設定で送ります。

```sh
kjak send --to api --dry-run "test"
kjak send --to api --title "Deploy" --level success "api 1.4.2 is live"
```

`--dry-run` は URL とトークンを解決し、何も送りません。実際の送信は、API が `success_status` のステータスを返すと `ok    api (http) delivered ...` と表示します。終了コードは、配信できれば 0、配信に失敗すれば 1、何も送らなかったなら 2 です。JSON のレポートには、試行ごとのステータスコード、待ち時間、エラーが入ります。

```sh
kjak send --to api -o json "test"
```

## 7. 応用的な設定

独自の JSON の形を求め、idempotency key があるので再試行でき、201 か 202 を返す API 向けの完全な設定です。

```toml
# ~/.config/kjak/config.toml
[defaults]
timeout = "30s"        # 送信全体の期限（すべての試行と待ち時間）
max_retries = 3

[feathers.incidents]
type = "http"
url = "https://api.example.com/v2/incidents"
method = "POST"
headers = { "X-Team" = "ops" }
query = { source = "kjak" }
success_status = ["201", "202"]
timeout = "5s"                  # 1 回の試行の期限
idempotency_key = true          # POST も再試行できる（すべての試行に同じキー）
levels = ["warning", "error", "critical"]

[feathers.incidents.auth]
type = "header"
header = "X-Api-Key"
value = { secret = "incidents.api_key" }

[feathers.incidents.body]
format = "json"
template = '''
{"summary": "[{{.level}}] {{.title}}",
 "details": "{{.body}}",
 "link": "{{.url}}",
 "source": {"tool": "kjak", "feather": "{{.feather}}"},
 "page": true}
'''

[feathers.retry_cap]
type = "http"
url = "https://status.example.com/api/components/42"
method = "PUT"
success_status = ["200", "204"]

[feathers.retry_cap.auth]
type = "basic"
username = "kjak"
password = { env = "STATUS_API_PASSWORD" }

[feathers.retry_cap.body]
template = '{"status": "{{.level}}", "message": "{{.body}}"}'

[feathers.retry_cap.retry]
max_attempts = 2
```

- **JSON のテンプレート** は、文字列の値がテンプレート（`{{.title}}`、`{{.body}}`、`{{.level}}`、`{{.url}}`、`{{.feather}}`）になっている JSON の文書です。kjak は設定を読むときに文書を解析し、送信のたびにエンコードし直します。そのため、メッセージが構造を変えることはできません（タイトルの引用符は文字列の中にとどまります）。数値、真偽値、`null` は書いたとおりに残り、キーは整列した順で出力されます。プレーンテキストの API には `format = "text"` を使います（JSON には使いません）。
- **メソッドと再試行**: 既定のポリシー `safe` は `GET`、`PUT`、`DELETE` を再試行し、`POST` / `PATCH` は `idempotency_key = true`（ヘッダー `Idempotency-Key`、または `idempotency_header`）のときだけ再試行します。`retry.policy = "never"` は 1 回だけ送ります。`"always"` はすべてのメソッドを再試行し、**`POST` が 2 回届くことがあります**。`retry.max_attempts` は試行の回数を `1 + max_retries` より少なく抑えます。
- **`timeout`** は 1 回の試行の期限です。`[defaults]` の `timeout`（または `--timeout`）が送信全体の期限です。
- **`success_status`** にはステータス（`"200"`）かクラス（`"2xx"`）を並べます。挙げていない 2xx は拒否として扱います。
- **`url_secret`** は、URL が認証情報のとき（トークンを含む Webhook URL）に `url` の代わりに使います。表示されることはありません。

```toml
# ~/.config/kjak/config.toml
[feathers.chat]
type = "http"
url_secret = { secret = "chat.webhook_url" }

[feathers.chat.body]
template = '{"text": "*{{.title}}*\n{{.body}}"}'
```

- **プライベートネットワークのサービス** は、許可するまで拒否されます。ホスト名なら `allow_hosts`（そのプライベートアドレスだけ）、CIDR の範囲なら `allow_networks` です。クラウドのメタデータのアドレス（`169.254.169.254` など）は決して許可されません。

```toml
# ~/.config/kjak/config.toml
[feathers.internal]
type = "http"
url = "https://alerts.corp.example/api"
allow_hosts = ["alerts.corp.example"]
max_redirects = 1          # リダイレクトを 1 回までたどる（URL と同じく検査する）
```

- **リダイレクト** は、`max_redirects` で許可しない限り拒否されます（`HTTP 302: a redirect that is not followed`）。リダイレクトでスキーム、ホスト、ポートが変わると、認証情報は外されます。
- **`GET`**（ハートビートの URL など）には本文がありません。`method = "GET"` だけで足ります。

## 8. 秘密情報

http feather の認証情報は、どれも読み取り先をちょうど 1 つ持つ参照です。`{ secret = "..." }`（あなたの secrets file）、`{ env = "..." }`、`{ keychain = { service = "...", account = "..." } }`（macOS、Linux）のいずれかです。

| キー | 送られるもの |
|---|---|
| `url_secret` | URL 全体（表示されず、パスとクエリーはエラーから伏せられる） |
| `auth.token`（`bearer`） | `Authorization: Bearer <token>` |
| `auth.password`（`basic`。`auth.username` は平文） | `Authorization: Basic <base64>` |
| `auth.value`（`header`。`auth.header` がヘッダー名） | `<header>: <value>` |

上の例の secrets file です。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[api]
token = "replace-me"

[incidents]
api_key = "replace-me-too"

[chat]
webhook_url = "https://chat.example.com/hooks/replace-me"
```

- 秘密情報は `[auth]` と `url_secret` が置く場所にだけ送られます。`headers`、`query`、テンプレートは平文で、テンプレートから秘密情報は使えません。キーをクエリーに入れる API には `url_secret` を使います。
- 解決した値に改行があると、何も送らずに拒否します。
- トークン、パスワード、エンコードした basic の認証情報、ヘッダーの値、秘密の URL は、すべてのエラーとレポートから伏せられます。API が送り返した内容も同じです。
- CI で `env` を使うときは、CI の secret store から変数を設定します。シェルの履歴に残る場所で `VAR=value kjak ...` と打ち込まないでください。
- `kjak config validate --strict` は `{ literal = "..." }`（非推奨）を拒否します。

## 9. トラブルシューティング

| メッセージ | 原因 | 対処 |
|---|---|---|
| `url: must be https (http needs allow_http = true)` | `http://` の URL | `https` を使う。`allow_http = true` はローカルのリレーかテストのときだけ |
| `the request was not sent: address 127.0.0.1 is loopback: allow it with allow_networks`（`blocked`） | ローカルの URL | `allow_networks = ["127.0.0.1/32"]`（6 章） |
| `the request was not sent: address 10.0.0.5 is private: allow it with allow_networks or allow_hosts` | プライベートのサービス | `allow_hosts = ["the.host"]` か `allow_networks = ["10.0.0.0/24"]` |
| `... is cloud metadata: never allowed` | URL（またはリダイレクト先）がインスタンスのメタデータを指している | 意図的にできないようにしている |
| `HTTP 302: a redirect that is not followed (max_redirects)` | API がリダイレクトする | 最終的な URL を使うか、`max_redirects = N` |
| `HTTP 401: ...` / `HTTP 403: ...` | 認証情報の誤りや失効、または `[auth]` の種類の誤り | トークンと API の認証方式を確認する。再試行はしない |
| `HTTP 200 is not in success_status` | `success_status` がほかのステータスを挙げている | そのステータスを加える |
| `(not retried: POST is not repeated without an idempotency key; retry.policy)` | 一時的なステータスで拒否された POST | API が対応していれば `idempotency_key = true`。重複してよいときだけ `retry.policy = "always"` |
| `HTTP 502 from a gateway: the API may have accepted it`（`????`） | API の代わりにゲートウェイが応答した | 送り直す前に API 側を確認する。kjak は再試行しない |
| `no response within the attempt timeout of 5s after the request was sent; the API may have accepted it` | API が遅い | `timeout` を延ばす。送り直す前に確認する |
| `auth.token: credential error: environment variable KJAK_API_TOKEN is not set` | `env` の参照 | kjak の環境で変数を export する |
| `headers: Authorization is ...` | `headers` に認証情報を書いた | `[auth]` を使う |
| `feather add: an http feather is configured in the file` | `feather add --type http` | ファイルに `[feathers.NAME]` を書く（5 章） |
| `TLS certificate verification failed` | プライベート CA、またはホスト名の誤り | [認証局](../security.md#certificate-authorities-ssl_cert_file-ssl_cert_dir)を参照 |

詳しくは [トラブルシューティング](../troubleshooting.md) と [受付状態の表](../http.md#success-errors-and-the-response) を参照してください。

```sh
kjak config validate
kjak send --to api --dry-run "test"
kjak send --to api -o json "test"
```

## 10. セキュリティ上の注意

- **宛先の制限** は、このマシンからしか届かない場所に設定ファイルが接続するのを防ぎます。ループバック、リンクローカル、プライベートのアドレスには `allow_networks` / `allow_hosts` での明示的な許可が必要で、クラウドのメタデータ、未指定、マルチキャスト、予約済みのアドレスは決して許可されません。kjak はホストを 1 回だけ解決し、検査したアドレスに接続します（DNS rebinding は効きません）。リダイレクトもすべて同じ検査を通ります。
- **プロキシなし**: http feather は `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY` を無視します（ガードが見るのが宛先ではなくプロキシになってしまうため）。プロキシ経由でしか届かない API には対応していません。
- **`allow_http = true`** は、リクエスト、トークン、応答を平文で送ります。テストかこのマシンのリレーだけで使ってください。
- **`retry.policy = "always"`** は `POST` を 2 回届けることがあります。idempotency key を優先してください。
- **証明書** は必ず検証します（TLS 1.2 以上）。検証を省くオプションはありません。
- **プロジェクトの設定ファイル**（`.kjak.toml`）: `kjak config trust` は、Trust する前に、各 http feather のメソッド、URL（またはその読み取り先）、認証情報の読み取り先、すべての `allow_*` とリダイレクトの設定を表示します。`env` と `keychain` の参照は名前ごとに許可する必要があります。
- 詳細: [セキュリティ: HTTP feather と SSRF](../security.md#http-feathers-and-ssrf)。

## 11. 関連資料

- [HTTP feather](../http.md) — すべてのキーと規則（このチュートリアルのリファレンス）
- [宛先の制限（SSRF）](../http.md#the-address-policy-ssrf)
- [再試行と idempotency](../http.md#retries-and-idempotency)
- [テンプレート](../http.md#templates)
- [設定: http](../configuration_ja.md#http) — キーの表
- [配信: HTTP feather](../delivery.md#http-feathers)
- [セキュリティ: HTTP feather と SSRF](../security.md#http-feathers-and-ssrf)
- [設定例: http-only.toml](../examples/http-only.toml)
- [トラブルシューティング](../troubleshooting.md)
