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
		Services: []application.Service{
			application.NewService(newChatService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Frameless 去掉系统标题栏与边框，画出来的窗口控制交给界面自己
	// （frontend/src/components/WindowControls.tsx）。
	//
	// Linux 上只有"有装饰"和"完全没装饰"两种：gtk_window_set_decorated(false)
	// 会把关闭、最小化一起收走，所以那三个按钮必须由前端提供，否则窗口
	// 只能靠 Alt+F4 关闭。拖拽区由 CSS 的 --wails-draggable 指定。
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "neutron-star",
		Width:            900,
		Height:           640,
		Frameless:        true,
		MinWidth:         560,
		MinHeight:        420,
		BackgroundColour: application.NewRGB(255, 255, 255),
	})

	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
