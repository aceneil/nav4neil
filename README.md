# nav4neil

`nav4neil` 是 WezTerm4Neil 的导航 TUI。它把 SSH 服务器列表和本地文件浏览
组合在一个可嵌入的 Go 程序中，并区分两种使用模式（M8）：**独立模式**
（直接运行 `nav4neil`，默认 `both`）在当前 pane 里执行所选目标；**侧栏
嵌入模式**（`--section servers|files` 的两个实例放进 Zellij 左栏）点选后
开全新 Zellij tab 或悬浮编辑器。

## 功能

- 解析 `~/.ssh/config` 与 `~/.config/wezterm4neil/servers.txt`，在服务器区段
  显示别名、可选说明和连接目标。
- 服务器区段可直接管理 `servers.txt`：`servers` 单区顶部为 `serv4neil` 标题行，
  其下是一行合并的 `[NEW] [EDIT]` 操作项（左右并列，不再换行分列）。
  ↑/↓、j/k 循环到达该行，`←`/`→` 或 `h`/`l` 选择（armed）其中一项，Enter 或
  鼠标单击对应半个区域触发；`n`/`N` 新建。`e`/`E` 进入/退出 **编辑模式**
  （M6）：进入后操作行显示为 `<NEW> <EDIT>`、光标变绿并跳到列表最上面的
  服务器，编辑模式下 Enter 或 `→` 打开当前服务器/文件夹的编辑表单
  （内置 `localhost` / `herdr` 与 `~/.ssh/config` 条目只读，会给出
  「内置项不可编辑」提示）。表单支持 Tab/方向键切换字段、Enter 保存
  （Enable）、Esc 取消（Cancel）。
- 保存的 `servers.txt` 新行格式为
  `<name>|<user>@<host>:<port>|<desc>|<group>|<password>`（仍兼容旧
  `<name>|<desc>`）；保存后自动刷新列表。内置行 **`localhost` → `herdr`**
  恒在最前且不允许被新增条目占用（大小写不敏感），用户数据源里同名行会被丢弃。
- **内置 `herdr`（Agent 工作台，M9）**：排在 `localhost` 之后、其它服务器之前；
  与 `localhost` 一样不可编辑/不可删除、永不进组、状态方块恒为绿（不参与轮询）。
  点击 herdr（侧栏或独立模式行为一致）= 在当前 Zellij 会话弹 96% 悬浮窗运行
  `~/.local/bin/wz-herdr.sh`
  （`zellij action new-pane --floating --close-on-exit --name herdr
  --width 96% --height 96% --x 2% --y 2% -- ~/.local/bin/wz-herdr.sh`；
  脚本会临时锁 Zellij 键，herdr 内 `Ctrl+b q` 退出后自动解锁并关浮窗）。
  不在 Zellij 内或脚本缺失时只给状态栏提示；nav 进程始终存活（herdr 从不
  exec 当前 pane）。
- 带密码的条目用 `sshpass -p '<pw>' ssh [-p <port>] <user>@<host>` 连接；
  系统缺少 `sshpass` 时状态栏提示安装；无密码走 `ssh [-p <port>] <user>@<host>`。
- `Group` 有值的条目按组渲染为 `▾ group/` 文件夹（Enter 或双击折叠成 `▸ group/`，
  组内服务器为子行，名称缩进在组名之下、打开方式与平铺行相同）；`Group` 为空
  的条目平铺在列表顶部，内置 `localhost` 与 `herdr` 恒在最前、永不进组。持久化格式不变。
- 服务器行按「指针 → 状态小方块 → 名称」排布：小方块紧贴名称左侧（不是隔在
  指针前面），颜色含义见下文「服务器状态方块」；分组文件夹行不画小方块
  （保留一个空格列保持对齐），组名与子行名称对齐。
- 文件区段支持目录进入/返回、过滤、刷新、鼠标点击与 Nerd Font 文件图标。
- 默认启动一个仅绑定 `127.0.0.1` 的 HTTP/WebSocket 上下文服务；服务不可用时
  TUI 仍会继续运行。
- 默认布局为上下两个 pane；`--section servers|files` 只运行一个区段
  （侧栏嵌入模式）。

## 两种使用模式（M8 / M9）

模式由 `--section` 决定：**有 `--section servers|files` = 侧栏嵌入**；
**不带 `--section`（默认 `both`）= 独立模式**。二者对「打开」的语义不同
（内置 `herdr` 例外：两种模式都只弹 Zellij 悬浮窗，见下表与下节）：

| | 服务器 / localhost | 文件 | 内置 herdr |
| --- | --- | --- | --- |
| 独立模式 `nav4neil`（both） | 当前 pane 内执行：先退出 TUI（bubbletea 还原终端），再把本进程 `syscall.Exec` 成 `ssh …`（localhost → 本地 shell fish/`$SHELL`）。ssh 退出后回到外层 shell，nav 不复活 | 当前 pane `exec nvim/vim <路径>`（检测同 wz-open：nvim 优先、vim 兜底） | 同侧栏：仍要求 Zellij，弹悬浮窗跑 `~/.local/bin/wz-herdr.sh`，nav 不退出（herdr 从不 exec 当前 pane） |
| 侧栏嵌入 `--section servers` / `--section files` | 开一个**全新 Zellij tab**：`zellij action new-tab --name <tab> -- ssh …`（localhost → 新 tab 跑本地 shell），nav 继续留在左栏 | 走 `~/.local/bin/wz-open.sh` 悬浮窗 nvim/vim（Zellij 内为 floating pane） | 弹悬浮窗：`zellij action new-pane --floating --close-on-exit --name herdr --width 96% --height 96% --x 2% --y 2% -- ~/.local/bin/wz-herdr.sh`，nav 继续留在左栏 |

要点：

- 独立模式在**任何终端**都可用（不要求 Zellij）：回车/双击/右键同普通语义，
  但动作 = 退出 + exec。exec 前 `prepareExec` 先停掉内嵌 ws 服务并记录 argv
  （`syscall.Exec` 不会回卷 defer，所以不能等 main 的收尾），再由 bubbletea
  正常退出清理 alt-screen / 还原终端，最后 main() 里 `syscall.Exec(argv[0],
  argv, os.Environ())` 替换进程。argv[0] 一律解析为绝对路径（execve 不做
  $PATH 查找）。缺依赖（无 sshpass、无 ssh、无编辑器、无本地 shell）时留在
  TUI 内给状态栏提示。
- 侧栏模式必须跑在 Zellij 会话里（wznav 布局的两个 pane 即如此）：点服务器
  = 一条 `zellij action new-tab --name <tab> -- ssh …`（不再 move-focus /
  new-pane / 向已有窗格打字，M8 恢复为整屏新 tab）；新 tab 以该 ssh 为初始
  命令直接执行、不经 shell。不在 Zellij 内时侧栏只给状态栏提示，不执行。
- 侧栏模式点文件仍走 wz-open.sh 悬浮窗（整屏 nvim/vim，退出自动关回布局）；
  独立模式点文件是当前 pane exec 编辑器。
- **内置 `herdr`（M9）两种模式共用一条路径**：仍在 Zellij 会话内就弹悬浮窗
  （`zellij action new-pane --floating --close-on-exit --name herdr
  --width 96% --height 96% --x 2% --y 2% -- ~/.local/bin/wz-herdr.sh`），
  与 ssh/localhost 不同它**不 exec、不开 tab**——nav 进程始终存活。
  依赖 `~/.local/bin/wz-herdr.sh`（随安装包提供，临时锁 Zellij 键、退出
  `Ctrl+b q` 后自动解锁并关浮窗）；脚本缺失或不在 Zellij 内时仅状态栏提示。
- `sshpass` 密码逻辑两种模式一致：有密码必须 `sshpass` 在 PATH，否则状态栏
  提示安装，什么都不执行。


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

**独立模式**：不加 `-section`（默认 `both`）时单个进程显示上下两个区段。
在该模式下回车/双击服务器或文件 = 退出 nav 并在当前 pane 执行目标
（ssh / 本地 shell / nvim、vim），nav 不再回来（内置 `herdr` 除外——
它只弹 Zellij 悬浮窗，见下）：

```bash
nav4neil
```

`both` 模式下可用 `Tab` 在 pane 间切换，`1`/`2` 直接切换服务器/文件区段。

**侧栏嵌入模式**：`-section servers|files` 只运行一个区段（也不会加载另一个
不相关的数据源），适合放进 Zellij 的 stacked pane（wznav 布局）。此时点
服务器开全新 Zellij tab、点文件走 wz-open 悬浮编辑器：

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

侧栏模式（`servers` 单区）打开服务器 = 开一个同名新 tab，所以点完通常
几秒内变绿；独立模式（`both`）把 ssh 直接 exec 到当前 pane、不建 tab，
ssh 行一般不会凭空变绿（除非别处手动开着同名 tab）。服务器条目到 tab 名
的映射规则：内置 `localhost` 对应 tab 名 `local`，内置 `herdr` 对应
`herdr`，ssh 条目用别名，
servers.txt 条目取 `user@host` 中 `@` 之后的部分（`root@db1` → `db1`）。
分组文件夹行不画小方块（保留一列空格保持对齐）。

**内置行（`localhost` / `herdr`）的状态方块恒为绿**：它们不参与
`dump-layout` 轮询、也不读打开失败记账——两个本机入口永远视为可用。

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
  内置 `localhost` 与 `herdr` 恒在首位、不进组。
- 打开服务器（平铺或组内）行为按模式：**侧栏（servers 单区，Zellij 内）** =
  一条 `zellij action new-tab --name <tab> -- ssh …` 开全新整屏 tab（命令复用
  ssh/sshpass 逻辑、作为 argv 直接执行、不经 shell；不在 Zellij 内只提示）；
  **独立（both）** = 退出 nav 并在当前 pane exec 该 ssh（localhost 同理 exec
  本地 shell）。
- 内置 `localhost`：独立模式 exec 本地默认 shell（fish 优先，其次 `$SHELL`）；
  侧栏模式 `zellij action new-tab --name local [-- <shell>]` 开一个本机 shell
  新 tab。两种模式都没有可用 shell 时仅状态栏提示。
- 内置 `herdr`（紧随 localhost 的第二行）：两种模式都不 exec/不开 tab，而是
  弹悬浮窗跑 `~/.local/bin/wz-herdr.sh`（见上文「两种使用模式」与功能列表）；
  `wz-herdr.sh` 缺失或不在 Zellij 内时仅状态栏提示。EDIT 模式选中 `localhost`
  或 `herdr`（Enter/`→`/双击）只提示「内置项不可编辑」，不打开表单；二者也
  不能被改名、分组或从 `servers.txt` 中删除（保存永远只写 extra 条目）。
- M6 编辑模式：普通模式按 `e`/`E` 或单击操作行的 `[EDIT]` 半区进入——操作行
  显示为 `<NEW> <EDIT>`，聚焦指针变绿，光标自动跳到列表最上面的服务器。
  此时上下移动照常；`Enter` 对服务器 = 打开该服务器的 EDIT 表单、对文件夹 =
  折叠/展开；`→`/`l` 对服务器 = 打开 EDIT 表单、对文件夹 = 打开「改组名」
  表单（改名 = 把该组下所有服务器的 `Group` 字段更新为新名；改成空 =
  解散回平铺；折叠状态随新组名保留），对操作行 = armed `[EDIT]`。
  `Esc`（或再次 `e`/`E`）退出编辑模式，未确认的改动丢弃。编辑模式下双击
  服务器同样打开 EDIT 表单（避免误连），双击文件夹仍是折叠/展开。

> M8 起打开语义按模式拆分：侧栏（`--section servers|files`）恢复为开**全新
> Zellij tab**——单条 `zellij action new-tab --name <tab> -- <命令>`（内置
> localhost 用 `--name local`，shell 可省略交给 Zellij 默认），不再
> move-focus / new-pane / 向已有窗格打字（M6/M7 的右主区分窗流程作废）。
> 独立模式（默认 both、无 `--section`）则不再驱动 Zellij：Update 返回
> tea.Quit 之前由 `prepareExec` 停掉内嵌 ws 并记录 argv，bubbletea 正常退出
> 清理 alt-screen 后，main() 用 `syscall.Exec` 把当前进程替换成目标命令
> （ssh / 本地 shell / nvim、vim），因此 ssh 退出后直接回到启动 nav 的外层
> shell、nav 不会复活。独立模式无需 Zellij，任何终端都可用。
>
> M9：列表新增第二内置行 **`herdr`（Agent 工作台）**，紧跟 `localhost` 之后。
> `localhost` 与 `herdr` 都不可编辑/不可删除、永不进组、状态方块恒绿；
> 服务器名 `localhost`/`herdr`（大小写不敏感）被保留，新增/改名的表单会拒绝、
> 数据源里的同名行会被丢弃。点击 `herdr`（侧栏与独立模式同一实现，且是独立
> 模式唯一不 exec 的行）只在 Zellij 会话里弹 96% 悬浮窗运行
> `~/.local/bin/wz-herdr.sh`（`zellij action new-pane --floating
> --close-on-exit --name herdr --width 96% --height 96% --x 2% --y 2% -- …`）；
> 脚本缺失或不在 Zellij 内时仅状态栏提示、nav 保持存活。

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
