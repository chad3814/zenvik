// Command zenvik-gui is the desktop front end for zenvik: browse disc images
// and folders, tick titles, and queue rips that run one at a time.
package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// singleInstanceID names the lock that keeps a second Zenvik from running
// (and ripping the same saved queue); it must never change between releases.
const singleInstanceID = "dev.cwalker.zenvik.gui-5d0b7c62-3c1e-4c8e-9a43-2f6f1b0f7a91"

// version is set at build time with -ldflags "-X main.version=<tag>".
var version = "dev"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	deps, err := defaultDeps()
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
	app := NewApp(deps)
	err = wails.Run(&options.App{
		Title:         "Zenvik",
		Width:         1200,
		Height:        800,
		MinWidth:      900,
		MinHeight:     560,
		AssetServer:   &assetserver.Options{Assets: assets},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				app.secondInstance(d.Args)
			},
		},
		// The web view keeps its own drop handling: Windows only reports
		// file paths through it, Linux needs the web view to stay a drag
		// destination, and on macOS WebKit only delivers the queue rows'
		// HTML5 drops when it handles the drop too. Wails' runtime already
		// prevents the default for any drag carrying files, so a dropped
		// file never replaces the page.
		DragAndDrop: &options.DragAndDrop{EnableFileDrop: true},
		Bind:        []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}
