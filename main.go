package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"hd2-strain-lab/assets"
	"hd2-strain-lab/internal/bridge"
	"hd2-strain-lab/internal/uiapp"
)

func main() {
	// Steam launch bridge mode: `"path/to/hd2-strain-lab.exe" --steam-bridge %command%`
	if len(os.Args) >= 3 && os.Args[1] == "--steam-bridge" {
		os.Exit(bridge.RunSteamBridge(os.Args[2:]))
	}

	app, err := uiapp.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	mygo.Theme.SetSource(mygo.ThemeDark)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "HD2 Strain Lab · 绝地潜兵 2 星球变种工坊",
			Width:     1380,
			Height:    860,
			MinWidth:  1080,
			MinHeight: 680,
			Content:   ui.View(app.View),
		})
		_ = win.SetIcon(assets.AppIconPNG)
		app.AttachWindow(win)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
