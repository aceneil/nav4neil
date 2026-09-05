# neilwz-nav-tui

`neilwz-nav-tui` 是 WezTerm4Neil 的独立导航 TUI。它把 SSH 服务器列表和
本地文件浏览组合在一个可嵌入的 Go 程序中，也可以拆成两个 Zellij pane，
分别运行 `servers` 与 `files` 区段。

## 功能

- 解析 `~/.ssh/config` 与 `~/.config/wezterm4neil/servers.txt`，在服务器区段
  显示别名、可选说明和连接目标。
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
CGO_ENABLED=0 go build -o bin/neilwz-nav-tui ./cmd/neilwz-nav-tui

./build.sh --test          # gofmt + go vet + go test + build
./build.sh --no-binary     # 只运行 gofmt、go vet、go test
./build.sh --debug         # 保留调试符号
```

产物是 `bin/neilwz-nav-tui`（默认不链接 CGO）。仓库不提交 `bin/`，可按需安装：

```bash
install -Dm755 bin/neilwz-nav-tui ~/.local/bin/neilwz-nav-tui
```

## 使用

```text
neilwz-nav-tui [options]

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
neilwz-nav-tui
```

`both` 模式下可用 `Tab` 在 pane 间切换，`1`/`2` 直接切换服务器/文件区段。
单区段模式不会加载另一个不相关的数据源，适合放进 Zellij 的 stacked pane：

```kdl
layout {
  pane split_direction="vertical" {
    pane split_direction="horizontal" {
      pane size="50%" { command "neilwz-nav-tui -section servers" }
      pane size="50%" { command "neilwz-nav-tui -section files --start-dir ~/Documents" }
    }
    pane { command "nvim" }
  }
}
```

文件区段以 `~` 表示 `$HOME`，超长路径从左侧截断。`neilwz-servers` 与
`neilwz-files` 是唯一的区段头行，不使用装饰性虚线或额外的旧名称标题。
服务器选中行保留 `▶`/`▷` 指示；文件行在名称前显示 Nerd Font 图标。

## 快捷键

| 按键 | 行为 |
| --- | --- |
| `j` / `k` / `↑` / `↓` | 上下移动 |
| `Enter` | 打开服务器、目录或文件 |
| `h` / `l` / `←` / `→` | 文件区段返回上级/进入目录 |
| `1` / `2` / `Tab` | `both` 模式切换 pane |
| `/` | 过滤当前区段；`Esc` 清除过滤 |
| `r` | 刷新服务器、文件或当前区段 |
| `?` | 显示快捷键提示 |
| `q` / `Ctrl+C` | 退出 |

## WebSocket（ws）端点

默认地址是 `ws://127.0.0.1:39771`，端口占用时自动递增；可用
`NEILWZ_NAV_TUI_WS=<port>` 覆盖（包括 `:0` 请求系统分配端口），或用
`--no-ws` 关闭。服务仅用于本地上下文同步：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 返回 PID、context、端口和 `neilwz-nav-tui-v1` 版本 |
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
├── cmd/neilwz-nav-tui/              # 主程序
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
