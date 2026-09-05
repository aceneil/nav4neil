// wznav-ws-smoke is a small driver that proves the websocket server can
// bind, serve /health and round-trip /context over plain HTTP. NOT shipped
// in the final binary; used by nav/build.sh tests when network is available.
//
//	go run ./cmd/wznav-ws-smoke
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/wezterm4neil/wznav/internal/ws"
)

func main() {
	s := ws.New("local")
	if err := s.Start(ws.DefaultPort + 100); err != nil {
		fmt.Println("start fail:", err)
		os.Exit(1)
	}
	defer s.Stop()
	url := s.URL()
	httpURL := "http" + strings.TrimPrefix(url, "ws")
	time.Sleep(150 * time.Millisecond)

	resp, err := http.Get(httpURL + "/health") //nolint:gosec
	if err != nil {
		fmt.Println("GET /health fail:", err)
		os.Exit(1)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Println("GET /health ->", resp.Status, string(body))

	resp2, err := http.Post(httpURL+"/context", "application/json",
		strings.NewReader(`{"context":"prod"}`))
	if err != nil {
		fmt.Println("POST fail:", err)
		os.Exit(1)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	fmt.Println("POST /context ->", resp2.Status, string(body2))

	resp3, err := http.Get(httpURL + "/context") //nolint:gosec
	if err != nil {
		fmt.Println("GET /context fail:", err)
		os.Exit(1)
	}
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	fmt.Println("GET /context ->", resp3.Status, string(body3))
}
