package ui

import (
	"github.com/rivo/tview"
)

type UI struct {
	App               *tview.Application
	Pages             *tview.Pages
	ErrorModal        *tview.Modal
	NamespaceDropDown *tview.DropDown
	Picker            ModalList
	PodsTree          *tview.TreeView
	PodsDetails       *tview.Flex
	PodData           *tview.TextView
	PodEvents         *tview.Table
	StatusBar         StatusBar
	ActionBar         ActionBar
}

func New() *UI {
	app := tview.NewApplication()
	pages := tview.NewPages()

	app.SetRoot(pages, true)

	ui := UI{App: app, Pages: pages}

	ui.init()

	return &ui
}

func (ui *UI) init() {
	ui.setupPages()
}
