// nav4neil — WezTerm4Neil 侧栏导航 TUI（左栏 20% 自研）。
//
// 用法：
//
//	nav4neil [--start-dir <path>] [--no-ws] [--ws-port <n>] [--section <s>]
//	         [--menu] [--no-menu] [--list] [--version] [--help]
//
//	--start-dir <path>   起始目录（默认 $HOME）
//	--ws-port <n>        ws 起始端口（默认 39771，被占用自动 +1；环境变量 NEILWZ_NAV_TUI_WS 覆盖）
//	--no-ws              禁用内嵌 websocket 服务（用于最小化场景）
//	--section <s>        渲染区段：both（默认）/ servers / files / all
//	--menu               强制先显示一级选择菜单（servers/files/all）
//	--no-menu            禁止一级选择菜单（无 --section 时直接按 both 运行）
//	--list               解析服务器列表并以易读文本打印到 stdout，退出
//	                     （无 DISPLAY/无 TTY 的 CI 环境仍可验证数据通路）
//	--version            打印版本
//	--help               帮助
//
// 三种使用模式：
//
//  0. 手动呼出（M11）—— 裸敲 `nav4neil`，即完全没有 --section：先显示一级
//     选择菜单（servers / files / all，默认高亮 servers），↑/↓ 或 j/k 移动、
//     Enter 确认、Esc/q 退出程序；选定后等同对应的 --section。布局/脚本一律
//     显式传 --section，因此不会弹菜单（显式 `--section both` 同样不弹）；
//     --menu 强制显示、--no-menu 显式抑制。无 TTY 或 --list 不弹菜单。
//  1. 侧栏嵌入模式 —— 带 --section servers|files 启动（wznav 布局左右两个
//     实例）。点服务器会 `zellij action new-tab --name <tab> -- ssh …`
//     开一个全新 Zellij tab（localhost 新 tab 跑本地 shell），nav 继续留在
//     左栏；点文件仍走 wz-open.sh 悬浮窗 nvim/vim。
//  2. 独立模式 —— `--section both`（或 --no-menu，或在菜单里选 all）显示
//     上下两区（all 模式，底部常驻 `Tab: 切换 服务器/文件` 提示行，Tab/1/2
//     切换两个区的焦点）。选中服务器或文件后 TUI 先自行退出（bubbletea
//     还原终端），再把当前进程 syscall.Exec 成目标命令（ssh / 本地 shell /
//     nvim、vim），在当前 pane 继续运行；ssh 退出后回到 shell，nav 不复活。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aceneil/nav4neil/internal/servers"
	"github.com/aceneil/nav4neil/internal/ui"
	"github.com/aceneil/nav4neil/internal/ws"
)

// sectionFlag records both the parsed --section value and whether the flag
// was supplied at all. M11's launch menu only appears for a bare `nav4neil`,
// so an explicit `--section both` must be distinguishable from the default.
type sectionFlag struct {
	value string
	set   bool
}

func (s *sectionFlag) String() string { return s.value }
func (s *sectionFlag) Set(value string) error {
	s.value = value
	s.set = true
	return nil
}

// options holds one parsed command line.
type options struct {
	fs *flag.FlagSet

	startDir string
	wsPort   int
	noWS     bool
	listMode bool
	section  sectionFlag

	menu     bool // --menu: force the M11 launch menu
	noMenu   bool // --no-menu: never show it
	showVer  bool
	showHelp bool
}

// parseOptions registers and parses the flags. It is a function rather than
// inline flag.XxxVar calls inside main so tests can pin the M11 launch-menu
// policy against real flag parsing — in particular that an explicit
// `--section both` counts as provided and therefore suppresses the menu.
// Errors (including flag.ErrHelp for -h) are returned after the FlagSet has
// already printed its message.
func parseOptions(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("nav4neil", flag.ContinueOnError)
	fs.SetOutput(stderr)

	o := &options{fs: fs}
	fs.StringVar(&o.startDir, "start-dir", envOr("HOME", ""), "起始目录（默认 $HOME）")
	fs.IntVar(&o.wsPort, "ws-port", ws.DefaultPort, "websocket 起始端口（默认 39771）")
	fs.BoolVar(&o.noWS, "no-ws", false, "禁用内嵌 websocket 服务")
	fs.BoolVar(&o.listMode, "list", false, "打印解析后的服务器列表后退出")
	// Use the short spelling requested by the Zellij workflow; Go flag also
	// accepts the conventional long spelling (--section).
	o.section = sectionFlag{value: "both"}
	fs.Var(&o.section, "section", "渲染区段：both|servers|files|all（默认 both=两区布局；servers|files=侧栏嵌入模式）")
	fs.BoolVar(&o.menu, "menu", false, "启动时先显示一级选择菜单（servers/files/all）")
	fs.BoolVar(&o.noMenu, "no-menu", false, "不显示启动菜单（无 --section 时直接按 both 运行）")
	fs.BoolVar(&o.showVer, "version", false, "打印版本并退出")
	fs.BoolVar(&o.showHelp, "help", false, "打印帮助并退出")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return o, nil
}

// 版本由 -ldflags "-X main.version=…" 注入；CI 留空时回落到 "dev"。
var version = "dev"

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		// The FlagSet already printed the parse error / usage text.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	section, err := ui.ParseSection(opts.section.value)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nav4neil:", err)
		os.Exit(2)
	}

	if opts.showHelp {
		opts.fs.Usage()
		fmt.Fprintln(os.Stderr, "\n环境变量:")
		fmt.Fprintln(os.Stderr, "  NEILWZ_NAV_TUI_WS=<port>   覆盖 --ws-port")
		fmt.Fprintln(os.Stderr, "  HOME              起始目录 + ssh config 路径")
		fmt.Fprintln(os.Stderr, "  XDG_CONFIG_HOME   servers.txt 所在 wezterm4neil/ 子目录根")
		fmt.Fprintln(os.Stderr, "\n示例:")
		fmt.Fprintln(os.Stderr, "  nav4neil                    # 手动呼出：先弹一级菜单（servers/files/all），选完按对应模式运行")
		fmt.Fprintln(os.Stderr, "  nav4neil --no-menu          # 跳过菜单，直接进 all（两区）独立模式")
		fmt.Fprintln(os.Stderr, "  nav4neil --section servers   # 侧栏嵌入：左上服务器列表，点选=开新 Zellij tab（不弹菜单）")
		fmt.Fprintln(os.Stderr, "  nav4neil --section files     # 侧栏嵌入：左下文件浏览，Enter=wz-open 悬浮编辑器（不弹菜单）")
		os.Exit(0)
	}
	if opts.showVer {
		fmt.Printf("nav4neil %s\n", version)
		os.Exit(0)
	}

	if opts.listMode {
		runList(os.Stdout)
		return
	}

	// Bubble Tea needs a terminal. Keep headless invocations (for example a
	// cron job or CI -section smoke check) graceful instead of leaking a
	// low-level open /dev/tty error. A missing TTY also means no menu.
	interactive := hasTerminal()
	if !interactive {
		fmt.Fprintln(os.Stderr, "nav4neil: interactive TUI requires a terminal; use --list or run inside a terminal/Zellij")
		return
	}

	// M11 launch menu: shown only for a bare `nav4neil` (no --section), i.e.
	// when layout scripts and callers cannot be surprised by it. --menu
	// forces it, --no-menu suppresses it, and an explicit --section (even
	// `--section both`) always wins.
	policy := ui.MenuPolicy{
		SectionProvided: opts.section.set,
		Force:           opts.menu,
		Suppress:        opts.noMenu,
	}
	if ui.ShouldShowMenu(policy, interactive) {
		chosen, ok, err := ui.RunMenu()
		if err != nil {
			fmt.Fprintf(os.Stderr, "nav4neil: menu: %v\n", err)
			os.Exit(1)
		}
		if !ok {
			// Esc/q: the user aborted the launch — exit without a TUI.
			return
		}
		section = chosen
	}

	// 启动本地 ws server（best-effort）。失败也不影响 TUI。
	var wss *ws.Server
	if !opts.noWS {
		wss = ws.New("local")
		if err := wss.Start(opts.wsPort); err != nil {
			log.Printf("nav4neil: ws disabled: %v", err)
			wss = nil
		} else if wss != nil {
			// 优雅退出时关掉。
			defer wss.Stop()
		}
	}

	// bubbletea 入口。
	model := ui.NewModel(opts.startDir, wss, section)
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// 退出后归还终端模式 + 关闭 ws（兜底；defer 在 SIGINT 下不保证执行；
	// 独立模式的 exec 走 syscall.Exec 替换进程、不会执行到这里，所以
	// prepareExec 里已经提前停掉 ws）。
	defer func() {
		_ = prog.ReleaseTerminal()
		if wss != nil {
			wss.Stop()
		}
	}()

	final, err := prog.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nav4neil: TUI error: %v\n", err)
		os.Exit(1)
	}

	// 独立模式（all / both）打开服务器/文件后：TUI 已正常退出（bubbletea 已还原
	// 终端、离开 alt screen），ws 也在 prepareExec 中停掉。此时用
	// syscall.Exec 把当前进程替换成目标命令（ssh / 本地 shell / nvim），
	// 让 ssh 退出后直接回到外层 shell —— nav 不会复活。
	if fm, ok := final.(*ui.Model); ok {
		if argv := fm.ExecArgv(); len(argv) > 0 {
			if err := syscall.Exec(argv[0], argv, os.Environ()); err != nil {
				fmt.Fprintf(os.Stderr, "nav4neil: exec %v: %v\n", argv, err)
				os.Exit(1)
			}
		}
	}
}

func hasTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// runList 是 --list 模式的实现：把解析后的服务器列表以可读文本打到 w，
// 同时打印 servers.txt / ssh config 路径，让脚本/CI 能快速验证。
func runList(w io.Writer) {
	entries := servers.Load()
	fmt.Fprintf(w, "nav4neil %s\n", version)
	fmt.Fprintf(w, "ssh config: %s\n", servers.SSHConfigPath())
	fmt.Fprintf(w, "extra list : %s\n", servers.ExtraListPath())
	fmt.Fprintf(w, "count      : %d\n\n", len(entries))
	for i, e := range entries {
		desc := ""
		if e.Desc != "" {
			desc = "  (" + e.Desc + ")"
		}
		tab := servers.TabName(e)
		fmt.Fprintf(w, "  %2d) %-32s ssh=%-32s tab=%s [%s]%s\n",
			i+1, e.Alias, servers.SSHArg(e), tab, e.Source, desc)
	}
	if err := servers.Validate(entries); err != nil {
		fmt.Fprintf(w, "\nwarn: %v\n", err)
	}

	// 顺手验证一下主机的文件浏览起点可读。
	if h, err := os.UserHomeDir(); err == nil {
		if abs, err := filepath.Abs(h); err == nil {
			if _, err := os.ReadDir(abs); err != nil {
				fmt.Fprintf(w, "\nwarn: start-dir %q not readable: %v\n", abs, err)
			}
		}
	}

	// ws port 提示（不实际启动，避免 --list 模式占端口）。
	if v := strings.TrimSpace(os.Getenv("NEILWZ_NAV_TUI_WS")); v != "" {
		fmt.Fprintf(w, "\nenv NEILWZ_NAV_TUI_WS=%s (active in TUI mode only)\n", v)
	}

	// 保留上下文，避免 linter 警告。
	_ = context.Background
}

// envOr returns os.Getenv(key) if non-empty, else fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
