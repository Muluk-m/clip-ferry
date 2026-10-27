# Clip Share

SSH 到另一台机器上跑 Claude Code（cc）这类 CLI 时，本机剪贴板里的截图过不去。Clip Share 留在 macOS 菜单栏里，记下本机复制过的图片；点某一张的「发送」，通过 SSH 把这一张传到远端，再把**远端文件的绝对路径**写进本机剪贴板。回到远端终端里粘贴这段路径，CLI 就能按本地文件读图。

窗口标题、托盘提示和右键菜单写的是 **Clip Share**。仓库和 Go module 的名字是 `clip-share`。

它基于 [MyGo](https://github.com/egoist/mygo) v0.7.1。界面、窗口、托盘、剪贴板、全局快捷键和 macOS 上的打包、签名都由 MyGo 完成。本仓库自己写的是剪贴板历史和经 SSH 发送。

## 功能

- 监视本机剪贴板，把 PNG 图片留在本机（最多 100 张，同一张图再复制会回到列表顶部，不另存一份）。
- 按需发送：只有点过「发送」的那一张会离开本机。不监听端口，也不起服务。
- 主机列表来自 `~/.ssh/config`，记住上次选择。传输用系统自带的 `/usr/bin/ssh`，沿用这份配置和密钥。
- 发送成功后，本机剪贴板变成远端绝对路径。图片仍留在历史里。
- 界面有简体中文和英文。系统偏好是简体中文时用中文，繁体中文和其他语言用英文。

## 界面

面板是一块 380×520 的无边框浮动窗口，不抢当前应用的前台（终端还在前面）。

- 左上角标题 **Clip Share**，下面一行是「⌃⌥V 打开 · 发送后远端路径写入剪贴板」。
- 右上角是主机下拉框。`~/.ssh/config` 里没有可用主机时，这里是红字「~/.ssh/config 里没有主机」。
- 中间是图片列表：缩略图、复制时间、原图像素（如 `1920 × 1080`）。这一次运行里已经发到当前主机的，像素后面会多一个「已发送」。每行一个「发送」按钮，传输过程中按钮变成百分比。
- 还没有图片时，中间是「还没有图片」和「截图或复制图片后，会自动出现在这里」。
- 底部平时是「N 张图片，仅保存在本机」。发送或出错后换成状态，失败是红字。
- 再下面是「开机启动」、`清空`、`退出`。

截图和演示还没放进仓库。录好后放到这两个路径：

- `docs/screenshot.png` — 菜单栏弹窗
- `docs/demo.gif` — 从本机复制图片，到远端终端粘贴路径

## 基于 MyGo

[MyGo](https://github.com/egoist/mygo) 用 Go 写桌面应用。一种窗口是系统 webview 里的网页，另一种是它自己绘制的原生界面。Clip Share 用原生界面：代码在 `github.com/egoist/mygo/ui`，没有 HTML / JavaScript，窗口里也不启动 webview。MyGo 文档写明，原生界面在 macOS 上用 Metal 绘制，在 Windows 上用 Direct3D 11，在 Linux 上用 OpenGL。

本仓库里 MyGo 做这些事：

- 面板上的列表、按钮、下拉框、复选框，来自 `ui`。
- 应用外壳：菜单栏托盘（`mygo.NewTray`）、浮动面板（`WindowOptions.Panel`）、全局快捷键、剪贴板、单实例、开机启动、按系统语言在 `locales/zh-Hans.json` 和 `locales/en.json` 之间选择。
- 命令行 `go tool mygo`：开发时热重载，构建时编出 `.app`，把 `resources/icon.png` 做成 icns，按 `mygo.json` 签名，并可选做 DMG 和公证。

剪贴板是否变化，是本仓库用 `purego` 读的 `NSPasteboard.changeCount`。第一次把托盘图标放靠右，也是直接调 AppKit。SSH 上传不经过 MyGo。

库和命令行都是 `github.com/egoist/mygo` v0.7.1。项目配置是根目录的 `mygo.json`，没有 `mygo.config.ts`，也没有网页前端，不需要 Bun 或 Node。

## 开发环境

- Go **1.27.3**，写在 `go.mod` 的 `go` 行。从 Go 1.21 起，默认的 `GOTOOLCHAIN=auto` 会下载这个工具链再执行。`go tool` 是 Go 1.24 才有的子命令；切到 1.27.3 之后才能用。更早的 Go 能不能读这份 `go.mod`，没有在这里试过。
- MyGo 的命令作为 Go tool 引进来：`go.mod` 里有一行 `tool github.com/egoist/mygo/cmd/mygo`，版本跟上面的 `require` 一样。在仓库根目录执行 `go tool mygo` 即用 v0.7.1，不必再 `go install` 到 `PATH`。
- 依赖不需要 cgo。`mygo dev` 和 `mygo build` 在环境变量 `CGO_ENABLED` 未设置时，自己把它当作 `0`。
- 菜单栏应用在 Mac 上跑。签名用系统的 `/usr/bin/codesign`，做 DMG 用系统自带的 `hdiutil`，这两步都要在 macOS 上执行。图标的 `.icns` 由 MyGo 在 Go 里从 `resources/icon.png` 生成。本仓库没有设置 `macos.icon`，构建不调用 Xcode 的 `actool`，因此不需要完整的 Xcode。
- 只有公证会调用 `xcrun notarytool` 和 `xcrun stapler`。`xcrun` 来自 Xcode 或 Command Line Tools。当前配置不做公证，日常构建用不到。
- `go tool mygo doctor` 检查本机有没有 Go、macOS 是不是 12 或更新，并统计钥匙串里的 Developer ID。它在 macOS 那一行提到 WKWebView，那是 MyGo 带网页的应用要用的；这个项目的窗口不加载网页。

## 开发、构建、DMG 和公证

### 开发运行

```sh
go tool mygo dev
```

它编译并启动应用。改了 `.go`、`mygo.json`、`locales` 里的文案或 `resources/icon.png` 之后会重新编译再启动；编译失败时继续留着上一份。终端里输入 `r` 然后回车也会重新编译，`q` 然后回车退出。开发包在 `.mygo/dev`，显示名是 Clip Share Dev，bundle id 是 `com.muluk.clipshare.dev`，可以和安装版同时开着。数据目录仍然是 `~/Library/Application Support/Clip Share/`，因为 `main.go` 启动时调用了 `SetName("Clip Share")`。

`mygo dev` 的 `-sign` 默认是 `-`，也就是 ad-hoc 签名，不读 `mygo.json` 的 `signingIdentity`。这次签名不带 hardened runtime，也不加时间戳（MyGo 把开发构建的 production 参数设为 false）。想用别的证书时：

```sh
go tool mygo dev -sign "Apple Development: you@example.com (TEAMID)"
```

界面测试不开窗口：

```sh
go test ./...
```

改了 `locales/*.json` 之后，`mygo dev` 和 `mygo build` 会重新生成 `locales/catalog_gen.go`。只跑 `go test` 不会生成它。

### 构建并装到本机

```sh
go tool mygo build -skip-dmg -skip-notarize
ditto "build/darwin-arm64/Clip Share.app" "/Applications/Clip Share.app"
open "/Applications/Clip Share.app"
```

- `-skip-dmg`：不做磁盘映像。MyGo 的说明是 “do not create a disk image for macOS”。
- `-skip-notarize`：就算配置了 `macos.notarize` 也不提交公证。本仓库没有这项配置，加不加这个参数，构建结果一样。
- `-sign <身份>`：盖过 `mygo.json` 里的 `signingIdentity`。不写就用配置文件里的那一项。
- `-platform`：逗号分隔的 `GOOS/GOARCH`。不写时用当前机器，所以 Apple Silicon 上目录是 `build/darwin-arm64/`，Intel Mac 上是 `build/darwin-amd64/`。
- `-o`：换输出目录。不写时用 `mygo.json` 的 `out`，也就是 `build/`。

```sh
go tool mygo build -platform darwin/arm64 -skip-dmg
go tool mygo build -platform darwin/universal -skip-dmg
```

`darwin/universal` 的目录是 `build/darwin-universal/`。签名和做 DMG 都要在 macOS 上跑。在其他系统上构建时，MyGo 会跳过 DMG；签名身份又不是 `-` 时，它也不调用 `codesign`，只留下 Go 链接器自己的 ad-hoc 签名，并在日志里说明签名需要 macOS。

正式构建（`mygo build`）在身份不是 `-` 时，给 `.app` 加上 hardened runtime（`--options runtime`）和时间戳（`--timestamp`）。装好的应用没有 Dock 图标（`LSUIElement`）。自己打开时弹出面板；由登录项拉起时面板先藏着。

### 打 DMG

在 Mac 上去掉 `-skip-dmg`：

```sh
go tool mygo build -skip-notarize
```

Apple Silicon 上除了 `.app`，还有 `build/darwin-arm64/clip-share-0.1.0-arm64.dmg`。文件名里的 `clip-share` 是 MyGo 从应用名算出的 slug，`0.1.0` 来自 `version`。`darwin/universal` 的映像不带架构，叫 `clip-share-0.1.0.dmg`。卷名没有单独设置，用应用名 “Clip Share”（`macos.dmgTitle` 的默认）。身份不是 `-` 时，这个 DMG 也会用同一身份 `codesign`。

### 公证

本仓库的 `mygo.json` 没有 `macos.notarize`，所以现在的构建不会公证。下面按 MyGo v0.7.1 的源码说明它怎么做，这个仓库里没有实际跑过一次公证。

`mygo build` 同时满足下面几条才会提交公证：

- `macos.notarize.keychainProfile` 非空
- 命令行没有 `-skip-notarize`
- 命令行没有 `-skip-dmg`（公证写在生成 DMG 的函数里，跳过 DMG 就不会公证）
- 签名身份不是 `-`。身份是 `-` 又配了公证时，构建在开始前就失败，错误是需要 Developer ID 签名身份

先把公证账号存进钥匙串（需要 Xcode 或 Command Line Tools）：

```sh
xcrun notarytool store-credentials notary
```

在 `macos` 里加上公证，并把 `signingIdentity` 换成自己的 Developer ID（见下一节）。`infoPlist` 等原有字段保留，下面只写出要改的两处：

```json
"macos": {
  "signingIdentity": "Developer ID Application: Your Name (TEAMID)",
  "notarize": { "keychainProfile": "notary" }
}
```

然后：

```sh
go tool mygo build
```

MyGo 执行的命令是 `xcrun notarytool submit <dmg> --keychain-profile notary --wait --output-format json`。状态为 `Accepted` 之后，再 `xcrun stapler staple` 到 DMG 和 `.app`。可选字段 `macos.notarize.keychain` 会变成 notarytool 的 `--keychain`。MyGo 的文档和配置注释要求这张证书是 **Developer ID Application**。代码本身只拒绝 ad-hoc，不检查身份名字里有没有 “Developer ID”。

## mygo.json

原生界面项目用这份 JSON 配置 MyGo，字段和 `mygo.config.ts` 相同。本仓库写了这些：

| 字段 | 当前值 | 作用 |
| --- | --- | --- |
| `name` | `Clip Share` | `.app` 的名字、里面的可执行文件名，以及菜单、安装包上的名字。`main.go` 里又 `SetName("Clip Share")`，用户数据目录因此也是这个名字 |
| `identifier` | `com.muluk.clipshare` | bundle id，开机启动和单实例用它。`mygo dev` 会接上 `.dev` |
| `version` | `0.1.0` | 写入 `Info.plist`，并出现在 DMG 文件名里 |
| `out` | `build` | `mygo build` 的输出目录。不写的话 MyGo 默认是 `dist` |
| `macos.signingIdentity` | 作者的 `Apple Development: …` 证书 | `mygo build` 传给 `codesign --sign` 的身份。`mygo dev` 不读这一项 |
| `macos.infoPlist.LSUIElement` | `true` | 菜单栏应用，不在 Dock 显示图标 |
| `macos.infoPlist.LSApplicationCategoryType` | `public.app-category.developer-tools` | 写进 `Info.plist` 的应用分类 |
| `i18n.dir` | `locales` | 文案目录。构建时 MyGo 在这里生成 `catalog_gen.go` |
| `i18n.source` | `en` | 源语言。系统语言对不上简体中文时用英文 |

没写、打包时仍会用到的默认值：`macos.minimumSystemVersion` 为 `12.0`；`macos.dmgTitle` 为应用名；存在 `resources/icon.png` 时就用它做图标。没有 `macos.notarize`。完整字段表在 [MyGo v0.7.1 的配置文档](https://github.com/egoist/mygo/blob/v0.7.1/docs/configuration.md)。

### 换成自己的证书

现在的 `signingIdentity` 是作者钥匙串里的一张 Apple Development 证书。fork 之后这张证书不在你的钥匙串里，`mygo build` 会在 `codesign` 那一步失败。

查看本机身份：

```sh
security find-identity -v -p codesigning
```

把 `mygo.json` 的 `macos.signingIdentity` 改成输出里某一行的完整名字。也可以不改文件，用 `-sign` 盖过它：

```sh
go tool mygo build -sign "Apple Development: you@example.com (TEAMID)" -skip-dmg
```

只在编译的这台 Mac 上打开，用 ad-hoc。把身份设成 `"-"`，或：

```sh
go tool mygo build -sign - -skip-dmg
```

MyGo 文档的说法是：ad-hoc 签名的应用能在编译它的那台 Mac 上运行，换一台会被 Gatekeeper 拦住。`mygo dev` 默认已经是 ad-hoc。

要发给别人，MyGo 文档要求改用 **Developer ID Application**（[Apple Developer Program](https://developer.apple.com/programs/) 的证书），并按上一节配置 `macos.notarize`。当前仓库没有配公证，也没有 provisioning profile。Apple Development 证书能在哪些机器上打开，取决于这张证书本身；MyGo 不会把它当成 Developer ID。

## 首次配置

1. 在 `~/.ssh/config` 里写好要连的机器，并确认终端里 `ssh 主机名` 能免密登录。应用调用 `/usr/bin/ssh`，并带 `BatchMode=yes`，不会弹出密码，也不会询问是否信任主机密钥。
2. 打开面板，在右上角选主机。选择会写进设置。
3. 远端目录、保留天数、发送时的长边在界面上改不了，在设置文件里改。应用只在启动时读它。下拉框一切换，就会把当时的设置写出来；只有一个主机、还没切换过时，文件可能还不存在，自己建一份即可。例如选过 `macmini2` 之后：

`~/Library/Application Support/Clip Share/settings.json`

```json
{
  "host": "macmini2",
  "remoteDir": "~/.cache/clip-share",
  "keepDays": 7,
  "maxEdge": 1568,
  "autoHide": true
}
```

| 字段 | 默认 | 作用 |
| --- | --- | --- |
| `host` | 空。空着或已经不在配置里时，用主机列表的第一个 | 上次选的 `Host`。在界面里改主机时，把内存里的整份设置写回文件 |
| `remoteDir` | `~/.cache/clip-share` | 远端目录。以 `~/` 开头时由远端的 shell 展开。空字符串用回默认值 |
| `keepDays` | `7` | 每次发送时，删掉该目录**第一层**里修改时间超过这么多天的 `.png` 和 `.jpg`。`0` 不删 |
| `maxEdge` | `1568` | 发出去的图，长边超过这个值就缩小到这么长。`0` 或 `-1` 发剪贴板里的原 PNG，文件名不带边长 |
| `autoHide` | `true` | 会写进文件。当前代码不读它。面板在失焦、Esc、⌘W 时都会隐藏 |

运行中改这个文件要重启才生效。重启前如果又在界面里换了主机，写回去的是启动时读入的那份，磁盘上还没被读到的修改会被盖掉。

`keepDays` 清的是整个 `remoteDir` 里的 png/jpg，不限于本应用上传的文件。这个目录里不要放别的图。

主机列表只读用户的 `~/.ssh/config`（不读 `/etc/ssh/ssh_config`）：

- 跟着 `Include` 走，相对路径相对当前这份配置所在的目录，支持 `~/` 和 glob。从 `~/.ssh/config` 再往下最多 8 层，更深的不再读。
- 跳过带 `*`、`?`、`!` 的模式（例如 `Host *`）。
- 跳过 `github.com`、`gitlab.com`、`bitbucket.org`，以及 `github.com-work` 这种前缀、`foo.github.com` 这种后缀。`HostName` 指到这些站点的别名也会去掉。

## 使用

1. 按 **⌃⌥V** 唤出面板。也可以点菜单栏图标，或在 Spotlight / 启动台里再打开一次 Clip Share（已经在跑时，第二次打开会把面板叫出来）。菜单栏太挤、图标被系统收起来时，快捷键和再次打开仍然可用。托盘提示是「Clip Share — ⌃⌥V 打开」。第一次启动会尽量把图标放在靠右的位置；之后可以在菜单栏里拖动，系统按托盘 id `clip-share` 记住位置。图标被刘海或屏幕边缘挡住时，面板出现在鼠标所在的那块屏幕上。
2. 右上角确认远端主机。
3. 把图片放进剪贴板（按住 Ctrl 的区域截图，或从别的应用复制图片）。轮询间隔是 0.7 秒，新图会出现在列表顶部。只存成文件、没有进剪贴板的截图不会出现。
4. 点「发送」。按钮上会显示进度。底部出现「已复制远端路径：…」后，剪贴板里就是那段路径。
5. 面板点外面、按 Esc 或 ⌘W 就会隐藏，键盘回到原来的终端。在远端会话里粘贴。
6. 需要的话勾上「开机启动」。若系统要求先批准，底部会显示「设置开机启动失败：…」，按提示到「系统设置 → 通用 → 登录项」里允许。`清空` 只删本机的图，底部变成「历史已清空」，远端文件还在。⌘Q、面板上的「退出」或图标右键菜单都可以退出。右键菜单里有「打开 Clip Share」。

快捷键注册失败时，底部会显示「快捷键 ⌃⌥V 已被占用：…」。

| 操作 | 作用 |
| --- | --- |
| ⌃⌥V | 全局唤出或隐藏面板 |
| Esc、⌘W | 隐藏面板 |
| ⌘Q | 退出 |
| 点菜单栏图标 | 唤出或隐藏 |
| 右键菜单栏图标 | 打开或退出 |
| 面板失焦 | 隐藏 |

上传要点那一行的「发送」。没有选中主机时按钮是灰的。

## 工作原理

剪贴板没有变更通知。macOS 上每 0.7 秒比较一次 `NSPasteboard` 的 `changeCount`，计数变了并且内容里有 PNG、又不是 Finder 的文件列表（复制文件时系统会带上图标）时，才把图片读出来。应用启动时，剪贴板里已经有的图片也算一条。

本机文件在 `~/Library/Application Support/Clip Share/`：

- `images/<id>.png`：剪贴板里的 PNG 原样保存，权限 `0600`
- `thumbs/<id>.png`：列表用的缩略图（宽最多 240）
- `settings.json`

`id` 是这段 PNG 的 SHA-256 前 8 字节，16 位十六进制。超过 100 张时丢掉最旧的原图和缩略图。

发送时读的是本机原图，不把缩略图传走。默认长边上限 1568，与 Claude 读图前的缩放上限一致。长边超过它就缩到 1568，短边按比例。不透明的图会再压一张质量 90 的 JPEG：缩小过的图，JPEG 更小就用 JPEG；没缩小的图，JPEG 不到 PNG 一半大才用。有透明通道的保持 PNG。远端文件名是 `<id>-<maxEdge>.png` 或 `.jpg`。`maxEdge` 为 0（设置里写 `-1` 同样会变成 0）时，原 PNG 原样上传，文件名是 `<id>.png`。

列表上的像素是原图尺寸，不是发出去之后的尺寸。

SSH 固定走 `/usr/bin/ssh`：图形应用的 `PATH` 里往往没有 Homebrew，而系统 ssh 和终端读的是同一份配置和 agent。参数是 `BatchMode=yes`、`ConnectTimeout=8`、`ControlMaster=auto`、`ControlPath=/tmp/clip-share-%C`、`ControlPersist=30m`、`ServerAliveInterval=30`。套接字放在 `/tmp`，是因为 macOS 的套接字路径最长大约 104 字节。打开面板或更换主机时会在后台先接通这条主连接（`ssh -O check`，没有的话再执行一次 `ssh 主机 true`）。预连接失败不写状态栏，要点发送才看得到错误。单次发送最长 5 分钟。

远端执行的是 `sh -c`，登录 shell 是 fish、nushell 也没关系。脚本 `mkdir -p` 目标目录，把 stdin 写入文件，按 `keepDays` 清理旧的 png/jpg，然后打印 `pwd` 加文件名。登录横幅会被丢掉，只认最后一行；这一行必须以 `/文件名` 结尾，否则算失败，剪贴板不会被改掉。

「已发送」只记在当前这次运行的内存里，而且按主机分开。重启后面板上的标记会消失，远端文件还在。

界面控件来自 MyGo，见上面的「基于 MyGo」。

## 在远端 CLI 里用这段路径

发送成功后，本机剪贴板里是远端的绝对路径，例如：

```text
/home/me/.cache/clip-share/3f2a1b0c9d8e7f6a-1568.jpg
```

家目录在远端是什么，路径就是什么；Mac 上常常是 `/Users/me/.cache/clip-share/...`。

面板藏起来之后，键盘还在原来的终端上。SSH 会话里的粘贴走的是本机剪贴板，所以这段路径会进到远端的命令行。别的远程终端只要也是把本机剪贴板贴进去，同样适用。文件已经在那台机器上。在 Claude Code 的输入里把路径贴进去，让它读这个本地文件即可。同一张图、同一个 `maxEdge`，再发一次还是这个路径，远端文件被盖掉，修改时间也会更新，从而重新计算 `keepDays`。

## 常见问题

**发送失败，提示连接被拒绝、超时或 Permission denied。**  
先在终端用同一个主机名 `ssh 主机名` 走一遍。必须免密，并且主机密钥已经在 `known_hosts` 里。应用不会交互。从启动台打开时，继承不到终端里临时 `export` 的环境变量；密钥放在钥匙串里（`~/.ssh/config` 的 `AddKeysToAgent`、`UseKeychain`）时，`/usr/bin/ssh` 能自己读到。配置里如果用了只有新版 OpenSSH 才懂的选项，系统自带的 ssh 可能会拒绝。

**主连接僵住。**  
控制套接字是 `/tmp/clip-share-%C`，空闲保持 30 分钟。可以关掉再发：

```sh
ssh -o ControlPath=/tmp/clip-share-%C -O exit 主机名
```

**下拉里没有我的主机。**  
看上面的过滤规则。`Host *`、通配、GitHub / GitLab / Bitbucket 的别名都不会出现。改完 `~/.ssh/config` 要重启应用。

**复制了图，列表没变化。**  
剪贴板里要有 PNG。Finder 里复制文件会被忽略。只落在桌面上的截图，需要再复制一次（或用拷贝到剪贴板的截图快捷键）。

**快捷键没反应。**  
看面板底部是不是写着快捷键已被占用。图标被菜单栏收起来时，用 Spotlight 打开 Clip Share。

**远端找不到文件，或图比列表上的尺寸小。**  
以状态栏「已复制远端路径」那一行为准。默认文件名带 `-1568`，扩展名可能是 `.jpg`。超过 7 天没再发送的 png/jpg 会在下一次发送时被删掉。把 `keepDays` 设为 `0` 可以留下。

**开机启动打开的不是预期的那个包，或勾选失败。**  
登录项绑的是勾选时正在跑的那个 `.app`。在 `mygo dev` 里勾选，登录时打开的是 Clip Share Dev。macOS 13 及以后用系统登录项，macOS 12 用 `~/Library/LaunchAgents/com.muluk.clipshare.plist`（开发版的 id 后面多一个 `.dev`）。没有打成 `.app` 时勾选会失败。第一次注册后，macOS 可能还要在「系统设置 → 通用 → 登录项」里允许。

**构建时 codesign 失败。**  
`mygo.json` 里的 `signingIdentity` 是作者自己的开发证书。换成 `security find-identity -v -p codesigning` 里有的身份，或用 `-sign -` 做 ad-hoc。详见「换成自己的证书」。

## 系统要求

- 本机：macOS 12.0 或更新。`mygo.json` 没有设置 `macos.minimumSystemVersion`，打包时用 MyGo 的默认值 12.0。Apple Silicon 和 Intel 都可以，看构建目标。
- 构建：Go 1.27.3，以及 Mac 上的签名工具（`codesign`，做 dmg 时还要系统自带的 `hdiutil`）。
- 远端：能用 SSH 非交互登录的机器，有 `sh` 以及 `mkdir`、`cat`、`find`、`rm`。不必是 Mac。

当前版本 `0.1.0`，bundle id `com.muluk.clipshare`。

## 许可证

仓库里没有许可证文件。

## English

Clip Share is a macOS menu bar app (repo and Go module: `clip-share`). While you SSH into another machine and run a CLI such as Claude Code, screenshots on the Mac clipboard cannot cross over. The app keeps a local history of clipboard PNGs (up to 100). Send copies that one image over SSH and puts the **remote absolute path** on the Mac clipboard, so you can paste it into the remote CLI.

The panel opens with **⌃⌥V** (Ctrl+Option+V), a click on the menu bar icon, or by launching the app again. Esc, ⌘W, or clicking away hides it; the terminal stays frontmost and gets the keyboard back. Pick a host from `~/.ssh/config`. The app runs `/usr/bin/ssh` with `BatchMode=yes`, so the host must already accept a key. Defaults live in `~/Library/Application Support/Clip Share/settings.json`: remote directory `~/.cache/clip-share`, png/jpg in that directory older than 7 days removed on each send, long edge capped at 1568. Filename: `<id>-1568.png` or `.jpg`, where `id` is the first 8 bytes of the PNG's SHA-256 in hex.

```sh
go tool mygo build -skip-dmg -skip-notarize
ditto "build/darwin-arm64/Clip Share.app" "/Applications/Clip Share.app"
```

On an Intel Mac the bundle is under `build/darwin-amd64/`. The app is a [MyGo](https://github.com/egoist/mygo) v0.7.1 native UI program: `go tool mygo` (pinned in `go.mod`) builds, signs, and optionally makes the DMG. The signing identity in `mygo.json` is the author's Apple Development certificate; change it or pass `-sign -` for an ad-hoc signature. macOS 12 or later. Go 1.27.3. There is no license file. Screenshots belong in `docs/screenshot.png` and `docs/demo.gif` when they are recorded. The Chinese sections above cover MyGo, `mygo.json`, DMG, and notarization.
