package main

import (
	"embed"
	"os"
	"path/filepath"
	"runtime"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"loris-tunnel/internal/traytext"
	"loris-tunnel/internal/uilocale"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIconWindows []byte

//go:embed build/macos-systray.png
var trayIconMacOS []byte

//go:embed build/appicon.png
var trayIconFallback []byte

func main() {
	// Create an instance of the app structure
	app := NewApp()
	configDir := "."
	if app.storage != nil {
		configDir = filepath.Dir(app.storage.Path())
	}
	localeTag := uilocale.Resolve(configDir)
	trayLabels := traytext.ForLocale(localeTag)

	startTray, endTray := systray.RunWithExternalLoop(func() {
		showMainWindow := func() {
			if app == nil || app.ctx == nil {
				return
			}
			wailsruntime.Show(app.ctx)
			wailsruntime.WindowShow(app.ctx)
			wailsruntime.WindowUnminimise(app.ctx)
		}

		iconBytes := trayIconFallback
		switch runtime.GOOS {
		case "windows":
			if len(trayIconWindows) > 0 {
				iconBytes = trayIconWindows
			}
			if len(iconBytes) > 0 {
				systray.SetIcon(iconBytes)
			}
		case "darwin":
			if len(trayIconMacOS) > 0 {
				iconBytes = trayIconMacOS
			}
			// macOS menu bar icon prefers template icons.
			if len(iconBytes) > 0 {
				systray.SetTemplateIcon(iconBytes, iconBytes)
			}
		default:
			if len(iconBytes) > 0 {
				systray.SetIcon(iconBytes)
			}
		}
		if runtime.GOOS != "darwin" {
			systray.SetTitle(trayLabels.AppTitle)
		}
		systray.SetTooltip(trayLabels.IconTooltip)

		showWinItem := systray.AddMenuItem(trayLabels.ShowMainTitle, trayLabels.ShowMainTooltip)
		showWinItem.Click(showMainWindow)
		quitMenu := systray.AddMenuItem(trayLabels.QuitTitle, trayLabels.QuitTooltip)
		quitMenu.Click(func() {
			if app != nil && app.ctx != nil {
				app.PrepareForQuit()
				wailsruntime.Quit(app.ctx)
				return
			}
			// Fallback for edge cases where Wails context isn't ready yet.
			os.Exit(0)
		})

		// 点击图标弹出菜单（与 energye/systray 示例一致）。
		// macOS：CreateMenu 把菜单挂到 NSStatusItem，系统负责左键出菜单（在 Wails 下比 SetOnClick 可靠）。
		// Windows / Linux：用 ShowMenu 弹出同一套菜单项。
		popupTrayMenu := func(menu systray.IMenu) {
			if menu != nil {
				_ = menu.ShowMenu()
			}
		}
		switch runtime.GOOS {
		case "darwin":
			systray.CreateMenu()
		default:
			systray.SetOnClick(popupTrayMenu)
			systray.SetOnRClick(popupTrayMenu)
		}

		app.SetTrayMenuItems(showWinItem, quitMenu)
	}, func() {})
	startTray()
	defer endTray()

	// Create application with options
	err := wails.Run(&options.App{
		Title:         "Loris Tunnel",
		Width:         1024,
		Height:        768,
		DisableResize: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:  &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:         app.startup,
		OnBeforeClose:     app.beforeClose,
		OnShutdown:        app.shutdown,
		HideWindowOnClose: runtime.GOOS == "darwin",
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
	// Ensure tray resources are released when the window exits directly.
	systray.Quit()
}
