package main

import (
	"embed"
	"log"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "neutron-star",
		Description: "开发者的全栈工作台",
		LogLevel:    slog.LevelDebug,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "neutron-star",
		Width:  600,
		Height: 400,
	})

	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
