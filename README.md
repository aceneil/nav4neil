# nav4neil

`nav4neil` 是 WezTerm4Neil 的独立导航 TUI。它把 SSH 服务器列表和
本地文件浏览组合在一个可嵌入的 Go 程序中，也可以拆成两个 Zellij pane，
分别运行 `servers` 与 `files` 区段。

## 功能

- 解析 `~/.ssh/config` 与 `~/.config/wezterm4neil/servers.txt`，在服务器区段
  显示别名、可选说明和连接目标。
- 服务器区段可直接管理 `servers.txt`：`servers` 单区顶部为 `serv4neil` 标题行，
  其下是一行合并的 `[NEW] [EDIT]` 操作项（左右并列，不再换行分列）。
  ↑/↓、j/k 循环到达该行，`←`/`→` 或 `h`/`l` 选择（armed）其中一项，Enter 或
  鼠标单击对应半个区域触发；`n`/`N` 新建。`e`/`E` 进入/退出 **编辑模式**
  （M6）：进入后操作行显示为 `<NEW> <EDIT>`、光标变绿并跳到列表最上面的
  服务器，编辑模式下 Enter 或 `→` 打开当前服务器/文件夹的编辑表单
  （内置 `localhost` 与 `~/.ssh/config` 条目只读，会给出提示）。
  表单支持 Tab/方向键切换字段、Enter 保存（Enable）、Esc 取消（Cancel）。
- 保存的 `servers.txt` 新行格式为
  `<name>|<user>@<host>:<port>|<desc>|<group>|<password>`（仍兼容旧
  `<name>|<desc>`）；保存后自动刷新列表，内置 `localhost` 恒在最前且
  不允许被新增条目占用。
- 带密码的条目用 `sshpass -p '<pw>' ssh [-p <port>] <user>@<host>` 连接；
  系统缺少 `sshpass` 时状态栏提示安装；无密码走 `ssh [-p <port>] <user>@<host>`。
- `Group` 有值的条目按组渲染为 `▾ group/` 文件夹（Enter 或双击折叠成 `▸ group/`，
  组内服务器为子行，名称缩进在组名之下、打开方式与平铺行相同）；`Group` 为空
  的条目平铺在列表顶部，内置 `localhost` 恒在最前、永不进组。持久化格式不变。
- 服务器行按「指针 → 状态小方块 → 名称」排布：小方块紧贴名称左侧（不是隔在
  指针前面），颜色含义见下文「服务器状态方块」；分组文件夹行不画小方块
  （保留一个空格列保持对齐），组名与子行名称对齐。
- 文件区段支持目录进入/返回、过滤、刷新、鼠标点击与 Nerd Font 文件图标。
- 所有服务器（含 ssh 服务器）都以 **当前 Zellij tab 右侧主区域里的新建窗格**
  使用，不再开新 tab 全屏、也不再向已有窗格打字（M6 替换 M5 的 write 注入）：
  先 `zellij action move-focus right` 把焦点带到右主区，再
  `zellij action new-pane -- ssh …` 让新窗格直接运行该 ssh（命令复用既有
  ssh/sshpass 逻辑，作为 argv 传入、不经 shell）。每点一次（Enter/双击/
  同服务器重复点）都会在右主区多开一个窗格。
- 内置 `localhost` 使用同一套 new-pane 语义：在右主区新开一个窗格运行
  本地默认 shell（fish 优先，其次 `$SHELL`；都没有就让 Zellij 用其默认
  shell），每次点击 = 一个新的本机会话。
- 不在 Zellij 内时，打开任何服务器 / localhost 都只在状态栏提示，
  不执行任何命令。
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
`serv4neil` 标题行开头，紧接一行 `[NEW] [EDIT]` 合并操作项（不再使用
分开的装饰操作行）。服务器行排布为「指针 → 状态小方块 → 名称」：
聚焦行形如 `▶ ▮localhost`，未聚焦行用空格占位保持方块列对齐
（`  ▮web01`）；分组文件夹行省略小方块、形如 `  ▾ dc/`，子行缩进
对齐在组名之下（`  ▮  db1`）；文件行在名称前显示 Nerd Font 图标。

## 服务器状态方块

每行服务器名称的左侧紧贴一个彩色小方块（顺序：指针 → 方块 → 名称），
指示该服务器此刻的连接状态：

| 方块 | 含义 |
| --- | --- |
| ▮ 绿 | Zellij 布局里正开着该服务器对应的 tab（约 2.5 s 轮询一次 `zellij action dump-layout`；仅当本进程运行在 Zellij 内时启用） |
| ▮ 黄 | 无对应 tab / 尚未打开（默认） |
| ▮ 红 | 最近一次打开该服务器失败或异常；下一次成功打开、或对应 tab 重新出现后自动清除 |

M6 起打开服务器不再创建新 tab（ssh 以新建窗格的方式在右主区运行），所以常规
操作下 ssh 行不会凭空变绿：绿表示布局里仍存在与该服务器同名的 tab（旧版本遗留、
或手动打开的 tab）。服务器条目到 tab 名的映射规则不变：内置 `localhost`
对应 tab 名 `local`（该行本身只新开本地 shell 窗格、不输入命令），ssh 条目用别名，
servers.txt 条目取 `user@host` 中 `@` 之后的部分（`root@db1` → `db1`）。
分组文件夹行不画小方块（保留一列空格保持对齐）。

不在 Zellij 内或轮询失败时自动退回本地记账：打开成功记绿、打开失败
（命令无法启动或非零退出）记红。轮询只读、静默失败、不阻塞 UI，并带
2 s 超时。小方块用 256 色 ANSI 渲染（truecolor 终端同样兼容）；最窄
环境（`TERM=dumb`、`NO_COLOR` 或未设置 `TERM`）下退化为无色 `·`。

## 快捷键

| 按键 | 行为 |
| --- | --- |
| `j` / `k` / `↑` / `↓` | 上下移动（服务器列表含 `[NEW] [EDIT]` 合并行与分组行，循环可达） |
| `Enter` | 打开服务器/文件；在 `[NEW] [EDIT]` 合并行触发当前 armed（`▶` 所指）的新建/编辑；在分组文件夹行折叠/展开。编辑模式下：服务器 → 打开 EDIT 表单，文件夹 → 折叠/展开 |
| `←` / `→` / `h` / `l` | 服务器区段：光标在 `[NEW] [EDIT]` 行时在新建/编辑之间切换 armed；编辑模式下 `→`/`l` 对服务器打开 EDIT 表单、对文件夹打开「改组名」表单；文件区段返回上级/进入目录 |
| `n` / `N` | 服务器区段：新建服务器（悬浮表单） |
| `e` / `E` | 服务器区段：进入/退出编辑模式（进入后 `<NEW> <EDIT>` + 绿光标并跳到第一个服务器） |
| `Esc` | 退出编辑模式；清除过滤或取消悬浮表单 |
| `1` / `2` / `Tab` | `both` 模式切换 pane |
| `/` | 过滤当前区段 |
| `r` | 刷新服务器、文件或当前区段 |
| `?` | 显示快捷键提示 |
| `q` / `Ctrl+C` | 退出 |

服务器区段要点：

- `[NEW] [EDIT]` 合并为标题下的一行操作项：`k` 从第一个服务器往上即到达该行
  （自动 armed `[EDIT]`，与旧版「向上一步到 EDIT」手感一致），再 `k` 从列表
  顶部循环回末尾；`j` 从列表末尾循环回该行（armed `[NEW]`）。`←`/`→`（或
  `h`/`l`）在两项之间切换 armed，`▶` 指在 armed 项前，Enter 触发；鼠标单击
  操作行左半区域直接新建、右半区域进入编辑模式。
- `Group` 有值的条目收进 `▾ group/` 文件夹；在该行按 Enter 或双击折叠为
  `▸ group/`（子行隐藏），再按一次展开。`Group` 为空的条目平铺在前，
  内置 `localhost` 恒在首位、不进组。
- 打开服务器（平铺或组内）行为一致：在 Zellij 内先 `zellij action
  move-focus right` 让焦点到右主区，再 `zellij action new-pane -- ssh …`
  为这台服务器新开一个专属窗格（命令复用 ssh/sshpass 逻辑；每点一次都新开，
  重复点同一台也再开一个）；不再开新 tab、也不再向已有窗格打字。
- 内置 `localhost`：Zellij 内执行 move-focus right + `new-pane`（运行本地
  默认 shell：fish 优先，其次 `$SHELL`，都没有则让 Zellij 用默认 shell），
  每次点击新开一个本机 shell 窗格；不在 Zellij 内时仅状态栏提示。
- M6 编辑模式：普通模式按 `e`/`E` 或单击操作行的 `[EDIT]` 半区进入——操作行
  显示为 `<NEW> <EDIT>`，聚焦指针变绿，光标自动跳到列表最上面的服务器。
  此时上下移动照常；`Enter` 对服务器 = 打开该服务器的 EDIT 表单、对文件夹 =
  折叠/展开；`→`/`l` 对服务器 = 打开 EDIT 表单、对文件夹 = 打开「改组名」
  表单（改名 = 把该组下所有服务器的 `Group` 字段更新为新名；改成空 =
  解散回平铺；折叠状态随新组名保留），对操作行 = armed `[EDIT]`。
  `Esc`（或再次 `e`/`E`）退出编辑模式，未确认的改动丢弃。编辑模式下双击
  服务器同样打开 EDIT 表单（避免误连），双击文件夹仍是折叠/展开。

> 打开流程固定为两条 zellij action，无 pane id 解析：先 `move-focus right`
> 把会话焦点带到右主区（让随后的分屏发生在右侧、不碰左栏导航），再
> `new-pane -- <命令>`——该命令作为新窗格的 argv 直接执行、不经 shell，
> 不向任何已有窗格写入按键。打开的焦点会落在右侧新窗格内，返回左栏导航用
> Zellij 的 Alt+h。

服务器悬浮表单内：`Tab` / 方向键切换字段，`Enter` 保存（Enable），
`Esc` 取消（Cancel）。「改组名」表单同样支持 Tab/方向键、Enter 保存、
Esc 取消（单字段：Group）。

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
