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

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "PrAImate",
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
		},
	})
	if err != nil {
		panic(err)
	}
}
