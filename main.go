package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"hd2-mygo-cpv/internal/bridge"
	"hd2-mygo-cpv/internal/uiapp"
)

func main() {
	// Steam launch bridge mode: `"path/to/hd2-mygo-cpv.exe" --steam-bridge %command%`
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
			Title:     "绝地潜兵 2 · 自定义星球变种指挥台 (MyGo GPU)",
			Width:     1380,
			Height:    860,
			MinWidth:  1080,
			MinHeight: 680,
			Content:   ui.View(app.View),
		})
		app.AttachWindow(win)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
