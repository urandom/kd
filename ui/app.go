package ui

import (
	"github.com/rivo/tview"
)

type UI struct {
	app               *tview.Application
	pages             *tview.Pages
	errorModal        *tview.Modal
	namespaceDropDown *tview.DropDown
	picker            ModalList
	podsTree          *tview.TreeView
	podsDetails       *tview.Flex
	podData           *tview.TextView
	podEvents         *tview.Table
	statusBar         StatusBar
	actionBar         ActionBar
}

func New() *UI {
	app := tview.NewApplication()
	pages := tview.NewPages()

	app.SetRoot(pages, true)

	ui := UI{app: app, pages: pages}

	ui.init()

	return &ui
}

func (ui *UI) init() {
	ui.setupPages()
}
