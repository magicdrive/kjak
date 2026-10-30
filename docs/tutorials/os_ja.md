# OS の通知（macOS、Windows、Linux）

[English](os.md) · [チュートリアル一覧](README_ja.md) · [リファレンス: os.md](../os.md) · [設定](../configuration_ja.md)

このチュートリアルでは `os` feather（v1.1.0 で追加）を設定します。kjak は、実行中の OS の通知としてメッセージを表示します。macOS・Windows・Linux のそれぞれについて、実際に画面に表示されるかを左右する OS 側の設定と、何も表示されないときの確認手順を説明します。

## 1. 概要

`os` feather には、アカウントも Webhook も秘密情報も要りません。

```toml
[feathers.desktop]
type = "os"
```

kjak は署名のない単一の実行ファイルで、macOS や Windows の通知サービスが知っているアプリではありません。そこで、OS に付属するプログラムに通知の表示を頼みます。

| OS | kjak が実行するプログラム | 通知の差出人として表示される名前 |
|---|---|---|
| macOS | `/usr/bin/osascript`（JavaScript for Automation の `display notification`） | 通常は **スクリプトエディタ**（Script Editor） |
| Windows | Windows PowerShell 5.1（`powershell.exe`）と WinRT の toast API。Windows PowerShell のアプリケーション ID（AppUserModelID）を借ります | **Windows PowerShell** |
| Linux | D-Bus セッション経由の `notify-send`（libnotify） | kjak（`--app-name=kjak`） |

メッセージの文字列はデータとして渡され、コードにはなりません。シェルが解釈するスクリプトやコマンドラインには入りません。kjak はシステムに何も変更を加えません（ショートカット、レジストリキー、権限のどれも作りません）。

**ここでの「delivered」の意味。** kjak が `acceptance: confirmed` と報告するのは、OS のプログラムが通知を**受け付けた**ときです。通知が画面に表示されたことや、誰かが見たことは意味しません。集中モードや応答不可、スクリプトエディタや Windows PowerShell の通知設定、画面のロック、通知サーバーによって、受け付けられた通知が見えないままになることがあります。macOS と Linux はこれらの設定を kjak に知らせません。Windows は、Windows PowerShell の通知がオフになっていればそれを知らせます（9 章）。

## 2. 前提条件

- kjak v1.1.0 以降（`kjak version` で確認）。インストールは[導入ガイド](../getting-started_ja.md)を参照してください。
- kjak を実行するマシンの**デスクトップセッション**。そのマシンの画面でログインしていて、kjak がそのセッションの中で動いていること（デスクトップ上のターミナル、そこから起動したスクリプトなど）。

| OS | 条件 | 表示できない場合 |
|---|---|---|
| macOS（amd64、arm64） | GUI（Aqua）セッション。`launchctl managername` が `Aqua` を表示する | SSH 経由、launchd のデーモン、ログイン中のセッションの外で動く CI ジョブ |
| Windows（amd64） | 対話型のデスクトップセッション。`%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe` に Windows PowerShell 5.1 があること（Windows 10 と 11 に標準で入っています） | サービス、「ユーザーがログオンしているかどうかにかかわらず実行する」タスク、デスクトップのないセッション |
| Linux | D-Bus セッション（`DBUS_SESSION_BUS_ADDRESS`、または `$XDG_RUNTIME_DIR/bus`）、`PATH` 上の `notify-send`、通知サーバー（デスクトップ環境には必ずあります） | デスクトップのセッションバスがない SSH、cron、コンテナ、CI |

PowerShell 7（`pwsh`）は使いません。WinRT が使えないためです。kjak は Windows PowerShell 5.1 を絶対パスで実行します。

Linux で `notify-send` がなければインストールします。パッケージ名は Debian・Ubuntu では `libnotify-bin`、Fedora・Arch では `libnotify` です。

## 3. 準備

サービス側で作るものはありません。受け付けられた通知が実際に表示されるよう、OS 側を一度確認します。

### macOS

1. kjak を使うターミナルでセッションを確認します。

   ```sh
   launchctl managername        # Aqua なら通知を表示できる
   ```

   `Background` などそれ以外の値（たとえば SSH のセッション）なら、そこからは kjak の通知を表示できません。
2. **システム設定 → 通知** を開きます。kjak が `osascript` 経由で出す通知は、通常は **スクリプトエディタ** の項目に表示されます（この名前は kjak が選べるものではなく、すべての macOS のバージョンで確かめたものではありません）。項目は最初の通知の後に現れることがあります。その場合は一度送信してから（6 章）戻ってきてください。
3. スクリプトエディタの項目で **通知を許可** をオンにし、通知のスタイルを選びます。**バナー**（自動で消える）か **通知パネル**（閉じるまで残る）です。**なし** にすると通知センターにだけ入ります。
4. **集中モード**（コントロールセンター、またはシステム設定 → 集中モード）を確認します。「おやすみモード」などの集中モードの間は、スクリプトエディタを許可したアプリに入れていない限り通知は表示されません。

### Windows

1. **設定 → システム → 通知** を開き、**通知** がオンであることを確認します。
2. アプリの一覧で **Windows PowerShell** を探し、オンにします。kjak は Windows PowerShell のアプリケーション ID を借りるため、通知は Windows PowerShell からのものとして表示されます。項目は最初の通知の後に現れることがあります。これをオフにすると、同じ ID を使うあなた自身の PowerShell スクリプトの通知もオフになります。
3. **応答不可**（Windows 11）または **集中モード**（Windows 10）を確認します。オンの間、通知はバナーを出さずに通知センターに入ります。
4. 管理者がグループポリシーで通知をオフにしていることがあります。その場合、kjak はその設定を報告します（9 章）。

### Linux

1. kjak を使うターミナルで、セッションとプログラムを確認します。

   ```sh
   echo "$DBUS_SESSION_BUS_ADDRESS"   # デスクトップセッションなら値がある
   command -v notify-send              # libnotify のクライアント
   ```

2. デスクトップの通知設定を確認します（GNOME: 設定 → 通知、**おやすみモード**。KDE Plasma: システム設定 → 通知）。デスクトップ環境がなければ、通知サーバー（dunst、mako など）を動かします。

## 4. 最小構成

設定ファイルは `~/.config/kjak/config.toml`（`$XDG_CONFIG_HOME/kjak/config.toml`。Windows では `%AppData%\kjak\config.toml`）です。kjak が使うファイルは `kjak config path` で確認できます。次のファイルをそのままコピーしてください。

```toml
# ~/.config/kjak/config.toml
[feathers.desktop]
type = "os"
```

`--to` なしで送れるようにするには、既定の送信先にします。

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["desktop"]

[feathers.desktop]
type = "os"
```

確認します。

```sh
kjak config validate
kjak feather show desktop
```

`os` feather が受け付けるキーは `type`、`enabled`、`levels` だけです。Webhook・メール・http・plugin のキー、`allow_networks`、`allow_hosts`、`timeout`、`retry` は設定エラーになります。`native`、`macos`、`linux`、`windows` という type はありません。`os` を案内するメッセージとともに拒否されます。

## 5. CLI での設定

ファイルを編集する代わりに、kjak に feather を書かせることもできます。

```sh
kjak config init                      # ファイルを作る（既存のファイルは置き換えない）
kjak feather add desktop --type os
kjak feather list
```

`feather add --type os` が受け付けるのは `--levels`、`--disabled`、`--scope`、`--force` です。`os` feather には秘密情報がないので、secret のオプションはありません。例を示します。

```sh
kjak feather add desktop_alerts --type os --levels warning,error,critical
kjak feather add desktop --type os --force          # 既存の feather を置き換える
kjak feather add desktop --type os --scope project  # 代わりに ./.kjak.toml に書く
kjak feather disable desktop
kjak feather enable desktop
```

## 6. 最初の通知

まずドライランです。送信先を解決するだけで何も送りません（OS のプログラムも実行しません）。

```sh
kjak send --to desktop --dry-run "Hello from kjak"
```

次に通知を出します。

```sh
kjak send --to desktop "Hello from kjak"
kjak send --to desktop --title "Build complete" --level success "All tests passed"
```

```
ok    desktop (os) delivered in 420ms (1 attempt)
success: 1 delivered, 0 failed, 0 skipped
```

PowerShell の場合:

```powershell
kjak send --to desktop --title "Build complete" --level success "All tests passed"
```

表示される内容:

| | macOS | Windows | Linux |
|---|---|---|---|
| タイトル | メッセージのタイトル。なければ `kjak` | 同じ | タイトルと level（`Build complete · success`） |
| その後 | サブタイトルに level、続いて本文 | 本文、続いて level | 本文 |

メッセージの URL（`--url`）は、本文の後に 1 行で付きます。音もアクションもありません。通知をクリックしても何も開きません。

### JSON で結果を見る

`-o json` はレポートを JSON で出力します。`status`、`acceptance`、失敗したときの `error.code` を見ます。

```sh
kjak send --to desktop -o json "Hello from kjak"
kjak send --to desktop -o json "Hello from kjak" | jq '.results[0] | {status, acceptance, error}'
```

OS のプログラムが通知を受け付けた場合:

```json
{"status": "delivered", "acceptance": "confirmed", "error": null}
```

同じコマンドを Mac に SSH で入って実行すると、終了ステータス 1 で失敗します（`managername:` の後のセッション名は kjak の起動のされ方によって変わります。`Background` は一例です）。

```json
{
  "status": "failed",
  "acceptance": "rejected",
  "error": {
    "code": "unavailable",
    "message": "OS notifications are not available here: kjak does not run in a GUI session (launchctl managername: Background; over SSH or in a background job, a notification cannot be shown)"
  }
}
```

| OS の状態 | `status` | `acceptance` | `error.code` | 終了ステータス |
|---|---|---|---|---|
| 通知を受け付けた | `delivered` | `confirmed` | — | 0 |
| このシステムには kjak の OS 通知がない（FreeBSD など） | `failed` | `rejected` | `unsupported` | 1 |
| ここでは表示できない: GUI や D-Bus のセッションがない、プログラムがない、プログラムが失敗した | `failed` | `rejected` | `unavailable` | 1 |
| Windows: Windows PowerShell の通知がオフ | `failed` | `rejected` | `rejected` | 1 |
| プログラムの実行中に期限切れ、または Ctrl-C | `failed` | `unknown` | `deadline` / `canceled` | 1 / 130 |

`os` feather は再試行されません。再試行しても同じマシンの同じ状態に当たるだけだからです。

## 7. 応用的な設定

開発マシン向けの完全な設定です。すべてのメッセージをデスクトップに出し、既定の level、後で送信先を増やせる group、長いコマンドの終了を知らせる template を持ちます。

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["local"]
level = "info"
timeout = "15s"

[feathers.desktop]
type = "os"

[groups.local]
feathers = ["desktop"]

[templates.done]
title = "{{.task}} finished"
body = "{{.task}} exited with status {{.status}}"
level = "info"
```

```sh
kjak send "Backup finished"
kjak send --template done --var task=backup --var status=0
kjak template render done --var task=backup --var status=0
```

長いコマンドの終わりに（終了ステータスはシェルが埋めます）:

```sh
make test; kjak send --template done --var task="make test" --var status=$?
```

### 一部の level だけ

`levels` は feather が受け付ける level を絞ります。`--to` で直接指定した feather がその level を受け付けなければエラーです（終了ステータス 2、何も送られません）。**group のメンバー**が受け付けない場合は skip されます。

```toml
# ~/.config/kjak/config.toml
[feathers.desktop]
type = "os"
levels = ["warning", "error", "critical"]

[groups.alerts]
feathers = ["desktop"]
```

```sh
kjak send --to alerts --level error "Disk almost full"
kjak send --to alerts "Nightly job done"
```

2 つ目の送信は `desktop` を skip し（`skip  desktop (os): level_filter (group alerts)`）、送信全体は成功します。

### デスクトップとリモートの送信先

画面の通知は、席を離れていると見逃します。リモートの feather にも同時に送りましょう。feather はそれぞれ独立して配送されるので、デスクトップが `unavailable`（SSH 経由など）でも、もう一方は止まりません（outcome は `partial`、終了ステータス 1）。

```toml
# ~/.config/kjak/config.toml
[defaults]
targets = ["notify"]

[feathers.desktop]
type = "os"

[feathers.discord]
type = "discord"
webhook_url = { secret = "discord.webhook_url" }

[groups.notify]
feathers = ["desktop", "discord"]
```

Webhook の作り方は [Discord のチュートリアル](discord_ja.md)、設定例は [desktop-and-discord.toml](../examples/desktop-and-discord.toml) を参照してください。

## 8. 秘密情報

`os` feather には秘密情報がありません。secrets file にも環境変数にもキーチェーンにも、置くものはありません。`kjak feather add --type os` は `--secret-*` のオプションを受け付けません。

`os` feather をほかの feather と同じ設定で使う場合（上の例）、それらの秘密情報はいつもどおり secrets file に置きます。

```toml
# ~/.config/kjak/secrets.toml（モード 0600。値はダミー）
[discord]
webhook_url = "https://discord.com/api/webhooks/000000000000000000/replace-me"
```

通知の文字列は画面に表示されます。メッセージに秘密情報を入れないでください（10 章）。

## 9. トラブルシューティング

### `error.code: unavailable`

| メッセージに含まれる文字列 | 原因 | 対処 |
|---|---|---|
| `kjak does not run in a GUI session (launchctl managername: ...)`（macOS） | SSH、GUI セッションの外の launchd デーモンやエージェント、CI ジョブ | ログイン中のデスクトップセッションで kjak を実行する。SSH や CI からはリモートの feather を使う |
| `cannot tell the session (launchctl managername: ...)`（macOS） | `/bin/launchctl` を実行できなかった | 通常のユーザーセッションで実行する |
| `osascript failed: ...`（macOS） | スクリプトが拒否された（後に続くのは osascript のメッセージ） | 同じターミナルで `osascript -e 'display notification "test"'` を試す |
| `SystemRoot is not set` / `Windows PowerShell (...) is not installed`（Windows） | 環境変数が削られている、または Windows PowerShell 5.1 が削除されている | 通常のデスクトップセッションから実行する。Windows PowerShell を戻す |
| `Windows PowerShell failed: ...`（Windows） | PowerShell か WinRT が toast を拒否した（後に続くのは PowerShell のメッセージ） | サービスではなく、対話型のデスクトップセッションで実行する |
| `no D-Bus session (...)`（Linux） | SSH、cron、コンテナ、CI | デスクトップセッションで実行する。cron は下を参照 |
| `notify-send is not installed`（Linux） | libnotify のクライアントがない | `libnotify-bin`（Debian、Ubuntu）か `libnotify` をインストールする |
| `notify-send failed: ...`（Linux） | 通知サーバーが応答しなかった | デスクトップの通知サーバー、または dunst / mako を起動する |

Linux では、デスクトップのユーザーの cron ジョブにバスのアドレスを渡せば、セッションバスに届きます。ジョブの中で `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus` を設定します（デスクトップにログインしている間だけ有効です）。

### Windows で `error.code: rejected`

```
FAIL  desktop (os) failed (1 attempt): notifications are turned off for Windows PowerShell (Settings, System, Notifications): notifications are off: DisabledForApplication
```

Windows が、Windows PowerShell の通知がオフだと報告しています。末尾の語は Windows の設定です。`DisabledForApplication`（Windows PowerShell の項目がオフ）、`DisabledForUser`（あなたの通知全体がオフ）、`DisabledByGroupPolicy`（管理者のポリシー）。**設定 → システム → 通知** でオンにするか（3 章）、管理者に相談してください。

### `error.code: unsupported`

このシステム（BSD など）には kjak の OS 通知がありません。リモートの feather を使ってください。

### 「ok」なのに何も表示されない

`confirmed` は、OS のプログラムが通知を受け付けたという意味です。次の順に確認します。

1. **セッション。** 通知は kjak が動いているセッションに出ます。複数のユーザーがログインしている場合や、リモートデスクトップ・画面共有の場合は、kjak のセッション側に表示されます。
2. **通知センター。** macOS はメニューバーの時計をクリック、Windows は Win+N（Windows 11）か Win+A（Windows 10）。バナーが出ずに、ここにだけ入っていることがあります。
3. **集中モード / 応答不可。** macOS の集中モード、Windows の応答不可・集中モード、GNOME のおやすみモード。
4. **アプリの設定。** macOS はシステム設定 → 通知のスクリプトエディタ（通知を許可、スタイルが「なし」でない）。Windows は設定 → システム → 通知の Windows PowerShell（Windows が知らせた場合、kjak はこれを `rejected` と報告します）。Linux はデスクトップの kjak に関する設定。
5. **画面のロックやスクリーンセーバー。** ロックを解除するまで通知が保留されたり隠されたりすることがあります。
6. **OS のプログラムだけで試す。** 同じターミナルで実行します。

   ```sh
   osascript -e 'display notification "test" with title "test"'     # macOS
   notify-send "test" "from the terminal"                            # Linux
   ```

   これでも何も表示されなければ、原因は kjak ではなく OS の設定です。

### 送信前のエラー（終了ステータス 2）

| メッセージ | 対処 |
|---|---|
| `type "native": OS notifications are type = "os"`（`macos`、`linux`、`windows` も同様） | `type = "os"` と書く |
| `os feathers take no webhook_url, username or email settings ...` | それらのキーを削除する |
| `os feathers take no timeout or retry of their own (use [defaults])` | `timeout` を `[defaults]` に移すか、`--timeout` を使う |
| `feather "desktop" does not accept level info` | 合う `--level` を指定するか、group に送る |

詳しくは [os.md](../os.md) と [troubleshooting.md](../troubleshooting.md) を参照してください。

## 10. セキュリティ上の注意

- **画面は人に見えます。** 通知はロック画面や画面共有・プレゼンテーション中にも表示されることがあり、通知センターにも残ります。タイトルや本文に秘密情報や個人情報を入れないでください。ロック画面でプレビューを隠す OS の設定も確認してください。
- **文字列はデータです。** タイトルや本文はどこから来てもかまいません（コミットメッセージ、ログ）。kjak はそれらを、シェルやインタープリターが解釈するスクリプトやコマンドラインに入れません（macOS は標準入力の JSON、Windows は環境変数の base64、Linux は `--` の後の引数）。
- **実行するプログラム。** kjak は `/usr/bin/osascript` と Windows PowerShell を絶対パスで実行します（`SystemRoot` は環境変数から読みます。kjak は環境を設定と同じように信頼します）。Linux では `notify-send` を `PATH` から探します。他人が書き込めるディレクトリを `PATH` に入れないでください。
- **プロジェクトの `os` feather**（`.kjak.toml`。trust した後に使われます）にできるのは、あなたの画面に通知を出すことだけです。秘密情報を読まず、接続もしません。
- **Windows での借り物の ID。** 通知は Windows PowerShell からのものとして表示されます。見た人には、kjak からのものか別の PowerShell スクリプトからのものか区別できません。

## 11. 関連資料

- [os.md](../os.md) — `os` feather のリファレンス（キー、表示内容、結果、対応プラットフォーム）
- [OS 通知の設計](../design/os-notifications.md)
- [設定](../configuration_ja.md) — feather、group、template、defaults
- [CLI](../cli.md) — `kjak send`、`kjak feather add`、JSON のレポート
- [配送](../delivery.md) — acceptance、再試行、outcome
- [トラブルシューティング](../troubleshooting.md)
- 設定例: [desktop-only.toml](../examples/desktop-only.toml)、[desktop-and-discord.toml](../examples/desktop-and-discord.toml)、[development.toml](../examples/development.toml)
- ほかのチュートリアル: [Discord](discord_ja.md)、[Slack](slack_ja.md)、[Gmail](gmail_ja.md)
