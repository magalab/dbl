package app

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run initializes and starts the desktop application.
func Run() {
	workbench := newWorkbench()
	setAppearanceSource(workbench.appearance)
	mygo.App.SetName("DBL")
	mygo.App.SetMenu(mygo.NewMenu(appMenuItems(workbench, runtime.GOOS)))
	mygo.App.WhenReady(func() {
		workbench.window = mygo.NewWindow(mygo.WindowOptions{
			Title:         "DBL · 数据工作台",
			Width:         1280,
			Height:        820,
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Content:       ui.View(workbench.view),
		})
		workbench.window.OnClosed(workbench.shutdown)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
