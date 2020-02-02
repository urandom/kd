package ui

import (
	"github.com/urandom/kd/ui/event"
	"gitlab.com/tslocum/cview"
)

type UI struct {
	App               *cview.Application
	Pages             *cview.Pages
	NamespaceDropDown *cview.DropDown
	Picker            ModalList
	PodsTree          *cview.TreeView
	PodsDetails       *cview.Flex
	PodData           *cview.TextView
	PodEvents         *cview.Table
	StatusBar         StatusBar
	ActionBar         *ActionBar

	InputEvents *event.Input
}

func New() *UI {
	app := cview.NewApplication()
	pages := cview.NewPages()

	ui := UI{App: app, Pages: pages, InputEvents: event.NewInput()}

	ui.init()

	return &ui
}

func (ui *UI) init() {
	ui.App.SetInputCapture(ui.InputEvents.GetInputCapture())
	ui.App.EnableMouse()
	ui.App.SetRoot(ui.Pages, true)

	ui.setupPages()
}
