# チュートリアル

[English](README.md) · [導入ガイド](../getting-started_ja.md) · [設定例](../examples/README.md) · [設定リファレンス](../configuration_ja.md)

各チュートリアルは、1 つの feather を、何もない状態から最初の通知までたどります。サービスや OS での準備、そのまま使える `config.toml`、コマンドラインでの設定、最初の送信、そしてオプション、秘密情報、トラブルシューティング、セキュリティ上の注意です。キーと規則の一覧は[リファレンス](../configuration_ja.md)にあります。チュートリアルは「実際に動かす」ための手順です。

| feather | チュートリアル | 必要なもの |
|---|---|---|
| `os` | [OS の通知（macOS、Windows、Linux）](os_ja.md) | デスクトップのセッション |
| `discord` | [Discord](discord_ja.md) | Webhook を作れる Discord サーバー |
| `slack` | [Slack](slack_ja.md) | アプリを追加できる Slack ワークスペース |
| `email` | [Gmail](gmail_ja.md) | 2 段階認証を有効にした個人の Google アカウント（アプリ パスワード） |
| `email` | [任意の SMTP サーバー](smtp_ja.md) | SMTP サーバーとそのアカウント |
| `http` | [HTTP API](http_ja.md) | HTTP のエンドポイント（ローカルのテスト用サーバーで可） |
| `external` | [プラグイン](plugin_ja.md) | 自作の実行ファイル（チュートリアルで 1 つ作ります） |

複数の宛先にまとめて送るには、[グループ](../configuration_ja.md)と[複数チャンネルの設定例](../examples/multi-channel.toml)を参照してください。
