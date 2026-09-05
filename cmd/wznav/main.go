// wznav — WezTerm4Neil 侧栏导航 TUI（左栏 20% 自研）。
//
// 用法：
//
//	wznav [--start-dir <path>] [--no-ws] [--ws-port <n>] [--section <s>] [--list]
//	        [--version] [--help]
//
//	--start-dir <path>   起始目录（默认 $HOME）
//	--ws-port <n>        ws 起始端口（默认 39771，被占用自动 +1；环境变量 WZNAV_WS 覆盖）
//	--no-ws              禁用内嵌 websocket 服务（用于最小化场景）
//	--section <s>        渲染区段：both（默认）/ servers / files。
//	                     单段模式便于把 wznav 拆到两个 Zellij pane，
//	                     通过 Alt+h/j/k/l 在 pane 间挪焦点，
//	                     每个 pane 内只用 j/k/Enter/… 即可。
//	--list               解析服务器列表并以易读文本打印到 stdout，退出
//	                     （无 DISPLAY/无 TTY 的 CI 环境仍可验证数据通路）
//	--version            打印版本
//	--help               帮助
//
// 与 Zellij 集成：在 zellij pane 内运行时，选择「服务器」会触发
// `zellij action new-tab --name <tab> -- ssh <host>`。每次都开新 tab，
// 不动现有布局；不在 zellij 时降级为提示。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/wezterm4neil/wznav/internal/servers"
	"github.com/wezterm4neil/wznav/internal/ui"
	"github.com/wezterm4neil/wznav/internal/ws"
)

// 版本由 -ldflags "-X main.version=…" 注入；CI 留空时回落到 "dev"。
var version = "dev"

func main() {
	var (
		startDir  string
		wsPort    int
		noWS      bool
		listMode  bool
		sectionIn string
		showVer   bool
		showHelp  bool
	)
	flag.StringVar(&startDir, "start-dir", envOr("HOME", ""), "起始目录（默认 $HOME）")
	flag.IntVar(&wsPort, "ws-port", ws.DefaultPort, "websocket 起始端口（默认 39771）")
	flag.BoolVar(&noWS, "no-ws", false, "禁用内嵌 websocket 服务")
	flag.BoolVar(&listMode, "list", false, "打印解析后的服务器列表后退出")
	flag.StringVar(&sectionIn, "section", "both", "渲染区段：both|servers|files（单段模式便于把 wznav 拆到两个 Zellij pane）")
	flag.BoolVar(&showVer, "version", false, "打印版本并退出")
	flag.BoolVar(&showHelp, "help", false, "打印帮助并退出")
	flag.Parse()

	section, err := ui.ParseSection(sectionIn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "wznav:", err)
		os.Exit(2)
	}

	if showHelp {
		flag.Usage()
		fmt.Fprintln(os.Stderr, "\n环境变量:")
		fmt.Fprintln(os.Stderr, "  WZNAV_WS=<port>   覆盖 --ws-port")
		fmt.Fprintln(os.Stderr, "  HOME              起始目录 + ssh config 路径")
		fmt.Fprintln(os.Stderr, "  XDG_CONFIG_HOME   servers.txt 所在 wezterm4neil/ 子目录根")
		fmt.Fprintln(os.Stderr, "\n示例（Zellij 双 pane 拆分):")
		fmt.Fprintln(os.Stderr, "  wznav --section servers   # 左上：服务器列表")
		fmt.Fprintln(os.Stderr, "  wznav --section files     # 左下：文件浏览")
		os.Exit(0)
	}
	if showVer {
		fmt.Printf("wznav %s\n", version)
		os.Exit(0)
	}

	if listMode {
		runList(os.Stdout)
		return
	}

	// 启动本地 ws server（best-effort）。失败也不影响 TUI。
	var wss *ws.Server
	if !noWS {
		wss = ws.New("local")
		if err := wss.Start(wsPort); err != nil {
			log.Printf("wznav: ws disabled: %v", err)
			wss = nil
		} else if wss != nil {
			// 优雅退出时关掉。
			defer wss.Stop()
		}
	}

	// bubbletea 入口。
	model := ui.NewModel(startDir, wss, section)
	prog := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// 退出后归还终端模式 + 关闭 ws（兜底；defer 在 SIGINT 下不保证执行）。
	defer func() {
		_ = prog.ReleaseTerminal()
		if wss != nil {
			wss.Stop()
		}
	}()

	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "wznav: TUI error: %v\n", err)
		os.Exit(1)
	}
}

// runList 是 --list 模式的实现：把解析后的服务器列表以可读文本打到 w，
// 同时打印 servers.txt / ssh config 路径，让脚本/CI 能快速验证。
func runList(w io.Writer) {
	entries := servers.Load()
	fmt.Fprintf(w, "wznav %s\n", version)
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
	if v := strings.TrimSpace(os.Getenv("WZNAV_WS")); v != "" {
		fmt.Fprintf(w, "\nenv WZNAV_WS=%s (active in TUI mode only)\n", v)
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
