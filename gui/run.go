package gui

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// Run 启动 env-warden 桌面窗口（Wails / WebView2，Win11 自带运行时）。
// 前端为无构建步骤的原生 HTML/JS/CSS，直接内嵌自 frontend/dist。
func Run() error {
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("gui: embed frontend: %w", err)
	}
	app := NewApp()
	return wails.Run(&options.App{
		Title:     "env-warden",
		Width:     1180,
		Height:    820,
		MinWidth:  940,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: frontend,
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	})
}
