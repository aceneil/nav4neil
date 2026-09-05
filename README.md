# wznav — WezTerm4Neil 侧栏导航 TUI

**R2（2026-09-06）**：支持 `--section both|servers|files` 单段模式，
便于把 wznav 拆到 Zellij 的两个 stacked pane，左右各管一件事，
焦点切换走 Zellij 原生 `Alt+h/j/k/l`。CLI flag 默认 `both`，完全
向后兼容。

## 两种运行模式

### 模式 1：`--section both`（默认；一期行为）

单个 wznav 进程里把服务器列表 + 文件浏览上下叠起来，Tab / 1 / 2 在
两个 pane 之间切焦点：

```
┌─ wznav ────────────────────────────────────────────────┐
│ ▶ Servers                                              │
│   github.com                                           │
│   root@db1.example.com          (prod 数据库)          │
│   k8s-master                    (local k8s 控制面)     │
├─────────────────────────────────────────────────────────┤
│ ▶ Files (~/Projects)                                   │
│   ..                                                   │
│   wezterm4neil/                                         │
│   docs.md                                              │
├─────────────────────────────────────────────────────────┤
│ ctx=local pane=1 ws=ws://127.0.0.1:39771  |  → github.com (zellij=zellij) │
└─────────────────────────────────────────────────────────┘
```

### 模式 2：`--section servers` / `--section files`（R2 新增）

把 wznav 拆成两个独立进程，分别跑在两个 Zellij pane 里。
每个 pane 自适应满高（不留另一半的占位行），Tab / 1 / 2 在
单段模式下静默忽略（避免在过滤模式里误触），状态行显示
`mode=servers|files` 而不是 `pane=1|2`：

```
┌─ wznav — servers ──────────────────────────────────────┐
│ ▶ Servers                                              │
│   github.com                                           │
│   root@db1.example.com          (prod 数据库)          │
│   k8s-master                    (local k8s 控制面)     │
│                                                         │
│ ctx=local mode=servers ws=ws://127.0.0.1:39771          │
└─────────────────────────────────────────────────────────┘
```

```
┌─ wznav — files ────────────────────────────────────────┐
│ ▶ Files (~/Projects)                                   │
│   ..                                                   │
│   wezterm4neil/                                         │
│   docs.md                                              │
│                                                         │
│ ctx=local mode=files ws=ws://127.0.0.1:39772           │
└─────────────────────────────────────────────────────────┘
```

## 编译

```bash
./nav/build.sh              # 默认：tidy+vet+build+strip+ sanity check
./nav/build.sh --test       # 加跑单测
./nav/build.sh --no-binary  # 只跑 tidy/vet/test
./nav/build.sh --debug      # 保留可调试符号（不推荐）
```

Go 工具链按下列顺序查找：

1. `/tmp/gotool/go`        （按任务书约定自下载的官方 tar.gz）
2. `/usr/local/go/bin/go`  （系统包）
3. `PATH` 上的 `go`

找不到时脚本会打印安装指引并退出码 2。

产物：`bin/wznav`（仓库根目录下），CGO 关闭、`-trimpath`、strip 符号、
静态链接，Linux amd64；当前大小 ~7.9 MB。

## 运行

```bash
# 默认：单进程两段（向后兼容）
./bin/wznav

# R2：服务器段独立进程（用于 Zellij 左上 pane）
./bin/wznav --section servers

# R2：文件段独立进程（用于 Zellij 左下 pane）
./bin/wznav --section files

# 调试用：仅打印解析后的服务器列表（无 TTY 也可）
./bin/wznav --list

# 关闭内嵌 websocket（最小化场景，例如 CI）
./bin/wznav --no-ws

# 覆盖起始端口（环境变量也可：WZNAV_WS）
./bin/wznav --ws-port 40000
```

环境变量：

| 变量           | 作用                                  | 默认          |
|----------------|---------------------------------------|---------------|
| `HOME`         | 起始目录 + `~/.ssh/config` 解析路径   | 当前用户      |
| `XDG_CONFIG_HOME` | `~/.config/wezterm4neil/servers.txt` 根 | `~/.config`   |
| `WZNAV_WS`     | 覆盖 `--ws-port`（`:0` 表系统分配）   | `39771`       |
| `ZELLIJ`       | 由 zellij 注入；存在则用新 tab 启动   | （无）        |

## 快捷键

> 单段模式（`--section servers` / `--section files`）下 `Tab / 1 / 2`
> 静默忽略（不能切换到不存在的 pane）；`r` 只刷新当前段；
> `?` 显示的提示文本会相应收窄。其它键一致。

| 键                         | 行为                                                            |
|----------------------------|-----------------------------------------------------------------|
| `j` / `k` / `↑` / `↓`      | 上下移动当前 pane 的高亮行                                       |
| `g` / `G`                  | 跳到列表首 / 末（仅服务器 pane）                                 |
| `Enter`                    | 服务器：开新 zellij tab 跑 ssh；文件：目录进入 / 文件用 `wz-open.sh` 打开 |
| 双击（500ms 内同位置）      | 等价于 `Enter`（mouse 用户）                                     |
| `h` / `←`                  | 文件 pane：上一级目录                                            |
| `l` / `→`                  | 文件 pane：进入高亮目录                                          |
| `Tab` / `1` / `2`          | 仅 `both` 模式：在「服务器 ↔ 文件」pane 间切焦点                  |
| `/`                        | 当前 pane 进入过滤模式（再按 `Enter` 退出过滤）                  |
| `Esc`                      | 退出过滤、还原完整列表                                           |
| `Backspace`                | 过滤模式：删一个字符                                             |
| `r`                        | `both` 模式：重读 `~/.ssh/config` + `servers.txt`，刷新目录；<br>`servers` 模式：仅重读服务器；<br>`files` 模式：仅刷新目录 |
| `?`                        | 在状态行显示一次完整快捷键提示（按 section 自适应）              |
| `q` / `Ctrl+C`             | 退出 wznav（zellij pane 回到 shell）                            |

## 鼠标

- 单击 pane：把焦点切到该 pane + 把高亮移到点击行
- 双击（500ms 内同坐标）：立即执行 `Enter` 动作
- 滚轮：在当前 pane 内上下移动

## 设计取舍（why `--section`）

`both` 模式下两段共享一个进程，节省 ~4 MB 内存、一份 ws 端口；但
Zellij 的 stack pane 布局（左右两栏摞起来）天然就是把两个 widget
并排放——把 wznav 拆成两个进程后：

- 焦点切换走 Zellij 原生 `Alt+h/j/k/l`，不用抢 `Tab` 键；
- 每个 pane 只关注一件事，按键冲突面更小（`j/k/Enter` 在
  服务器段就是连接、在文件段就是移动）；
- ws 端口独立可观测（`39771` vs `39772`），出问题容易排查；
- 二期把文件段换成「远端 SFTP 浏览器」时，可以无痛替换而不影响
  服务器段。

每个进程仍然内嵌独立的 ws server（端口自动递增），所以将来两段
要互相通信也只需走 127.0.0.1，不需要新 IPC。

## 数据源

### 服务器列表

两份来源合并去重：

1. `~/.ssh/config`（按 `Host` 解析；`Host *` 通配 + `Host x y z`
   多别名都正确处理；`#` 注释行剔除；空 pattern 跳过；首胜去重）。
2. `~/.config/wezterm4neil/servers.txt`（每行 `别名` 或 `别名|说明`）。
   `#` 开头 / 空行忽略。

合并规则：ssh 先，extra 后；同 alias 不重复出现（ssh 优先）。

### 文件浏览

- 起始目录：`--start-dir` 或 `$HOME`。
- 列表项：目录在前，文件在后；同名大小写不敏感排序；隐藏文件下沉到组尾。
- 上一级 `..` 项：当前目录为 `/` 时不显示，避免无效操作。
- 文件打开：优先 `wz-open.sh`（仓库已有），其次 `xdg-open`；都没有
  就在状态行报错。

### 上下文（context）

TUI 内部维护一个 `ctx` 字段：

- 启动 = `"local"`
- 选中服务器后 = 该 host 的 tab 名（`servers.TabName(e)`）
- 写入内嵌 websocket 的 `context` 字段（也通过 `GET/POST /context` 暴露）

二期规划：外部 daemon 接 `POST /context` 把当前激活 tab 的机器名推给 TUI，
TUI 据此渲染远端文件树、跑 ssh exec 后远程 ls 等。一期不做。

### WebSocket 端点

`wznav` 启动时在同进程内起一个本地 ws 服务（best-effort；端口冲突自动
`+1` 走完）。所有端点**只在 127.0.0.1** 上接受连接。

| Method | Path        | 行为                                                         |
|--------|-------------|--------------------------------------------------------------|
| GET    | `/health`   | `{"ok":true,"pid":N,"context":"<ctx>","port":N,"version":"wznav-v1"}` |
| GET    | `/context`  | `{"context":"<ctx>"}`                                        |
| POST   | `/context`  | body `{"context":"<value>"}`，写后回 `{"context":"<value>"}`  |

ws 故障**绝不**影响 TUI。日志里能看到 `ws: serving on ws://127.0.0.1:39771`。

## 依赖与许可

| 依赖                                       | 版本       | 用途                  | 许可            |
|--------------------------------------------|------------|-----------------------|-----------------|
| github.com/charmbracelet/bubbletea         | v1.3.4     | TUI 框架              | MIT             |
| github.com/gorilla/websocket               | v1.5.3     | 本地 ws server        | BSD-2-Clause    |

间接依赖（bubbletea 拉入）：lipgloss / termenv / x/ansi / x/term / go-osc52 /
erikgeiser/coninput / lucasb-eyer/go-colorful / mattn/go-isatty / go-localereader /
go-runewidth / muesli/ansi / muesli/cancelreader / rivo/uniseg / golang.org/x/sync
/syscrypto/wait / text —— 全部 MIT 或 BSD-*，无 AGPL/SSPL 等传染 / 网络强 copyleft。

## 仓库布局

```
nav/
├── README.md                        # 本文件
├── go.mod / go.sum
├── build.sh                         # 编译入口
├── cmd/
│   ├── wznav/                       # 主二进制（产物：bin/wznav）
│   └── _smoke/                      # 人工调试用，非交付
│       ├── wznav-ws-smoke/          # ws 端到端冒烟（cd nav && go run ./cmd/_smoke/wznav-ws-smoke）
│       └── wznav-plan-smoke/        # 计划构造冒烟
└── internal/
    ├── servers/  (servers.go + _test.go)   # ssh config + servers.txt 解析
    ├── fs/      (fs.go)                     # 文件浏览器
    ├── action/  (action.go + _test.go)      # zellij 计划构造 / 执行
    ├── ws/      (ws.go)                     # 内嵌 ws server
    └── ui/      (ui.go + _test.go)          # bubbletea 视图/键盘/鼠标（含 Section/R2）
```

## 集成

- Zellij 布局：`config/zellij/layouts/sidebar.kdl`（R2 起）
  左栏 20% 横向拆成两个 pane，各跑一个 wznav：
  - 顶 50%：`wznav --section servers`
  - 底 50%：`wznav --section files`
  缺 wznav 时回退到 fish/bash 提示，不拖累右栏主终端。
- 安装：`install.sh` 若 `bin/wznav` 存在就复制到 `~/.local/bin/wznav`。
- Debian 打包：`packaging/linux/build-deb.sh` 新增 `--wznav-bin` 可选参数；
  缺省不装（向后兼容）。

## 单测

```bash
cd nav && go test ./...
# ok  github.com/wezterm4neil/wznav/internal/action     3 tests
# ok  github.com/wezterm4neil/wznav/internal/servers    6 tests
# ok  github.com/wezterm4neil/wznav/internal/ui        12 tests
# 21 passing  (R1=14, R2 新增 7：ParseSection / Section.String / 单段 view ×2 / Tab 静默 ×2 / 单段刷新隔离)
```

## 二期 / 遗留

- **远端文件浏览**：当 `ctx != "local"` 时把文件操作路由到 daemon；SFTP over
  ws；本地 `fs.Browser` 已为切到 `RemoteBrowser` 留好接口点。
- **真正的 ws 客户端**：把 server 部分抽成 `internal/ws/server`，新增
  `internal/ws/client` 给将来 daemon 用。
- **Zellij 焦点回写**：当用户切到某个 ssh tab，把 `ctx` 反向推到 TUI；今天
  TUI 只主动写 `ctx`，等 daemon 反向连过来再补 POST。
- **持久化**：当前 `~/.ssh/config` 是只读源；servers.txt 是手写列表。
  可以加「最近连接 / 最常用」本地索引（`~/.local/share/wznav/state.json`）。
- **插件/扩展层**：明确**不**做。`internal/ws` 已经提供足够的可观测点，
  任何"插件"想法都应当作为 zellij pane 里**第二个** Go 进程去实现，
  而不是给 wznav 加热加载。
