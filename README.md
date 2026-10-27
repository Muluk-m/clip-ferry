# Clip Share

在 SSH 到另一台机器上用 Claude Code 等 CLI 时，本机剪贴板里的图片粘贴不过去。Clip Share 常驻在 macOS 菜单栏，记录本机复制过的图片；在列表里点某张图的「发送」，它通过 SSH 把这一张图传到远端，再把**远端文件路径**写进本机剪贴板，到远端 CLI 里直接粘贴路径即可读图。

- 只走 SSH：不要求同一局域网，跳板机、Tailscale、公网都可以，复用 `~/.ssh/config` 和 ssh-agent。
- 按需发送：历史只保存在本机（最近 100 张），只有点过「发送」的那张图会离开本机。
- 不监听任何端口。

## 使用

```sh
go tool mygo dev      # 开发运行（自动重载）
go tool mygo build    # 打包 .app / .dmg 到 build/
go test ./...         # 测试
```

需要 Go 1.27.3+（`go` 会按 `go.mod` 自动下载工具链）。

1. 点菜单栏的剪贴板图标打开面板，右上角选择远端主机（列表来自 `~/.ssh/config`，会记住上次选择）。
2. 截图或复制图片，它会自动出现在列表顶部。
3. 点「发送」，状态栏显示 `已复制远端路径：…` 后，到远端 CLI 粘贴即可。

远端主机需要能免交互登录（密钥或 agent），因为应用以 `BatchMode=yes` 调用 `/usr/bin/ssh`。

## 细节

| 项 | 说明 |
| --- | --- |
| 本机存储 | `~/Library/Application Support/Clip Share/`（`images/` 原图，`thumbs/` 缩略图） |
| 远端存储 | `~/.cache/clip-share/<hash>.png`，每次发送时清理 7 天前的图片 |
| 连接复用 | `ControlMaster=auto`、`ControlPersist=10m`，socket 在 `/tmp/clip-share-%C` |
| 监听方式 | 每 0.7 秒比较 `NSPasteboard.changeCount`，变化且含图片时才读取；Finder 里复制的文件（带图标）会被忽略 |
| 主机列表 | 跟随 `Include`，忽略 `*` 等通配和 GitHub/GitLab/Bitbucket 别名 |

基于 [MyGo](https://github.com/egoist/mygo) 原生 UI（纯 Go，无 webview）。
