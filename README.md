# nav4neil

`nav4neil` 是 WezTerm4Neil 的独立导航 TUI。它把 SSH 服务器列表和
本地文件浏览组合在一个可嵌入的 Go 程序中，也可以拆成两个 Zellij pane，
分别运行 `servers` 与 `files` 区段。

## 功能

- 解析 `~/.ssh/config` 与 `~/.config/wezterm4neil/servers.txt`，在服务器区段
  显示别名、可选说明和连接目标。
- 服务器区段可直接管理 `servers.txt`：`servers` 单区顶部为
  `serv4neil` + `[NEW] [EDIT]`，`n`/`N` 新建、`e`/`E` 编辑当前行
  （内置 `localhost` 与 `~/.ssh/config` 条目只读，会给出提示）。
  表单支持 Tab/方向键切换字段、Enter 保存（Enable）、Esc 取消（Cancel），
  也可鼠标点击操作行与表单按钮。
- 保存的 `servers.txt` 新行格式为
  `<name>|<user>@<host>:<port>|<desc>|<group>|<password>`（仍兼容旧
  `<name>|<desc>`）；保存后自动刷新列表，内置 `localhost` 恒在最前且
  不允许被新增条目占用。
- 带密码的条目用 `sshpass -p <pw> ssh -p <port> <user>@<host>` 连接；
  系统缺少 `sshpass` 时状态栏提示安装；无密码走原有 ssh 逻辑
  （`zellij action new-tab` / 直连）。
- 服务器行首显示连接状态小方块（Zellij tab 轮询 + 失败记账，见下文
  「服务器状态方块」）。
- 文件区段支持目录进入/返回、过滤、刷新、鼠标点击与 Nerd Font 文件图标。
- 在 Zellij 内选择服务器时执行 `zellij action new-tab --name <tab> -- ssh <target>`，
  不替换当前 pane。
- 默认启动一个仅绑定 `127.0.0.1` 的 HTTP/WebSocket 上下文服务；服务不可用时
  TUI 仍会继续运行。
- 默认布局为上下两个 pane；`-section` 可只运行一个区段，便于嵌入布局。

## 构建与安装

需要 Go 1.27 或更高版本：

```bash
./build.sh
# 或
CGO_ENABLED=0 go build -o bin/nav4neil ./cmd/nav4neil

./build.sh --test          # gofmt + go vet + go test + build
./build.sh --no-binary     # 只运行 gofmt、go vet、go test
./build.sh --debug         # 保留调试符号
```

产物是 `bin/nav4neil`（默认不链接 CGO）。仓库不提交 `bin/`，可按需安装：

```bash
install -Dm755 bin/nav4neil ~/.local/bin/nav4neil
```

## 使用

```text
nav4neil [options]

-start-dir <path>   起始目录（默认 $HOME）
-ws-port <n>        WebSocket 起始端口（默认 39771，被占用后递增）
-no-ws              禁用内嵌 WebSocket 服务
-section <s>        both（默认）、servers 或 files；也接受 --section
-list               解析服务器列表并以文本输出后退出
-version            输出版本
-help               输出帮助
```

默认模式在单个进程中显示两个区段：

```bash
nav4neil
```

`both` 模式下可用 `Tab` 在 pane 间切换，`1`/`2` 直接切换服务器/文件区段。
单区段模式不会加载另一个不相关的数据源，适合放进 Zellij 的 stacked pane：

```kdl
layout {
  pane split_direction="vertical" {
    pane split_direction="horizontal" {
      pane size="50%" { command "nav4neil -section servers" }
      pane size="50%" { command "nav4neil -section files --start-dir ~/Documents" }
    }
    pane { command "nvim" }
  }
}
```

文件区段以 `~` 表示 `$HOME`，超长路径从左侧截断。`both` 布局中
`neilwz-servers` 是服务器 pane 头行；`servers` 单区模式则以
`serv4neil` 标题行 + `[NEW] [EDIT]` 操作行开头，不使用装饰性虚线。
服务器行以状态小方块开头，选中行保留 `▶`/`▷` 指示（聚焦行显示为
`▮ ▶ 名称…`，未聚焦/未选中行以空格占位保持名称列对齐）；文件行在
名称前显示 Nerd Font 图标。

## 服务器状态方块

服务器列表每行最前面有一个小方块，指示该服务器此刻的连接状态：

| 方块 | 含义 |
| --- | --- |
| ▮ 绿 | Zellij 中正开着该服务器对应的 tab（约 2.5 s 轮询一次 `zellij action dump-layout`；仅当本进程运行在 Zellij 内时启用） |
| ▮ 黄 | 无对应 tab / 尚未打开（默认） |
| ▮ 红 | 最近一次打开该服务器失败或异常；下一次成功打开、或对应 tab 重新出现后自动清除 |

服务器条目到 tab 名的映射与打开时 `zellij action new-tab --name` 完全
一致：内置 `localhost` 对应 tab 名 `local`，ssh 条目用别名，servers.txt
条目取 `user@host` 中 `@` 之后的部分（`root@db1` → `db1`）。

不在 Zellij 内或轮询失败时自动退回本地记账：打开成功记绿、打开失败
（命令无法启动或非零退出）记红。轮询只读、静默失败、不阻塞 UI，并带
2 s 超时。小方块用 256 色 ANSI 渲染（truecolor 终端同样兼容）；最窄
环境（`TERM=dumb`、`NO_COLOR` 或未设置 `TERM`）下退化为无色 `·`。

## 快捷键

| 按键 | 行为 |
| --- | --- |
| `j` / `k` / `↑` / `↓` | 上下移动 |
| `Enter` | 打开服务器、目录或文件 |
| `n` / `N` | 服务器区段：新建服务器（悬浮表单） |
| `e` / `E` | 服务器区段：编辑当前选中的 `servers.txt` 条目 |
| `h` / `l` / `←` / `→` | 文件区段返回上级/进入目录 |
| `1` / `2` / `Tab` | `both` 模式切换 pane |
| `/` | 过滤当前区段；`Esc` 清除过滤 |
| `r` | 刷新服务器、文件或当前区段 |
| `?` | 显示快捷键提示 |
| `q` / `Ctrl+C` | 退出 |

服务器悬浮表单内：`Tab` / 方向键切换字段，`Enter` 保存（Enable），
`Esc` 取消（Cancel）。

## WebSocket（ws）端点

默认地址是 `ws://127.0.0.1:39771`，端口占用时自动递增；可用
`NEILWZ_NAV_TUI_WS=<port>` 覆盖（包括 `:0` 请求系统分配端口），或用
`--no-ws` 关闭。服务仅用于本地上下文同步：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 返回 PID、context、端口和 `nav4neil-v1` 版本 |
| `GET` | `/context` | 读取当前服务器 context |
| `POST` | `/context` | 写入 `{"context":"..."}` |

## Nerd Font 图标

文件图标按 Yazi 默认主题的文件名、目录名和扩展名映射重写为 Go 表；当前包含
692 个文件名映射、506 个扩展名映射，以及目录/链接/常规文件兜底字形。名称匹配
大小写不敏感，完整文件名优先于扩展名。需要安装 **Nerd Font** 才能显示这些
Private Use 区字形；本环境/套件使用 CaskaydiaCove。

图标映射来源：Yazi 主仓库 `sxyazi/yazi` 的 MIT 许可默认主题（Yazi 的图标源许可
为 `LICENSE-ICONS`，作者 `nvim-tree`）。Yazi 映射仅作为数据参考，本项目将
其整理为 Go map，不依赖 Yazi 运行时、配置目录或 Lua 插件。

## 许可与依赖

本项目 Go 代码的许可以仓库维护者发布页面为准。第三方依赖许可如下：

| 依赖 | 版本 | 用途 | 许可 |
| --- | --- | --- | --- |
| `github.com/charmbracelet/bubbletea` | v1.3.4 | TUI 框架 | MIT |
| `github.com/gorilla/websocket` | v1.5.3 | 本地 WebSocket 服务 | BSD-2-Clause |

Bubbletea 的间接依赖（`lipgloss`、`termenv`、`x/ansi`、`x/term`、
`go-osc52`、`coninput`、`go-colorful`、`go-isatty`、`go-localereader`、
`go-runewidth`、`muesli/ansi`、`cancelreader`、`uniseg`、`x/sync`、`x/sys`、
`x/text`）分别遵循其上游 MIT/BSD 许可；具体版本以 `go.sum` 和上游仓库为准。
Yazi 图标数据的完整 MIT 许可见 `LICENSE-ICONS`。

## 仓库布局

```text
.
├── build.sh                         # 编译、vet、test 入口
├── go.mod / go.sum
├── cmd/nav4neil/              # 主程序
├── cmd/_smoke/                      # ws 与 action 冒烟驱动
└── internal/
    ├── icon/                        # Yazi 同规模 Nerd Font 图标表
    ├── servers/                     # SSH 配置与 servers.txt 解析
    ├── fs/                          # 文件浏览器
    ├── action/                      # Zellij action 构造
    ├── ui/                          # Bubble Tea 视图、键盘和鼠标
    └── ws/                          # 本地 HTTP/WebSocket 服务
```

`--list`、`-section servers` 和 `-section files` 均可无 DISPLAY/无 TTY 启动
（命令行数据检查不进入交互 TUI）。在 CI 中建议同时使用 `--list` 与
`go test ./...` 验证非交互路径。
