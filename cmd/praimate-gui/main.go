// praimate-gui is the Wails desktop shell around internal/core. It is
// a separate Go module from the root so `go build ./...` at the repo
// root never needs webkit2gtk headers; build this binary with:
//
//	cd cmd/praimate-gui
//	(cd frontend && pnpm install && pnpm build)
//	go build -tags webkit2_41 -o praimate-gui .
//
// On macOS/Windows the webkit2_41 tag is ignored; on Linux it selects
// webkit2gtk-4.1 (modern distros no longer ship 4.0).
package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the window/taskbar icon. On Linux Wails sets it at
// runtime via linux.Options.Icon; on Windows the icon comes from the
// exe's embedded resource (rsrc_windows_amd64.syso, generated with
// go-winres from this same PNG — see build.sh).
//
//go:embed frontend/src/assets/praimate.png
var appIcon []byte

func main() {
	// Hidden mode: when claude spawns this same binary as the MCP
	// approval shim (`praimate-gui -mcp-approve <url> -mcp-token <tok>`),
	// run the stdio server and exit WITHOUT touching Wails — no window,
	// no webview, just stdin/stdout JSON-RPC.
	if len(os.Args) >= 3 && os.Args[1] == "-mcp-approve" {
		endpoint := os.Args[2]
		token := ""
		for i := 3; i+1 < len(os.Args); i++ {
			if os.Args[i] == "-mcp-token" {
				token = os.Args[i+1]
			}
		}
		os.Exit(runApprovalShim(os.Stdin, os.Stdout, endpoint, token))
	}

	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "PrAImate GUI",
		Width:  1280,
		Height: 820,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 16, G: 18, B: 24, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			ProgramName: "praimate",
			Icon:        appIcon,
		},
	})
	if err != nil {
		panic(err)
	}
}
