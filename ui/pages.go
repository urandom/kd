package ui

import (
	"github.com/rivo/tview"
)

const (
	pageK8sError = "error-k8s"
	pagePods     = "pods"
)

func (ui *UI) setupPages() {
	ui.errorModal = tview.NewModal()
	ui.errorModal.SetTitle("Error")
	ui.namespaceDropDown = tview.NewDropDown().SetLabel("Namespace [Enter[]: ")
	ui.podsTree = tview.NewTreeView().SetTopLevel(1)
	ui.podsTree.SetBorder(true).SetTitle("Pods")
	ui.podsDetails = tview.NewFlex()
	ui.podData = tview.NewTextView().SetWrap(false).SetDynamicColors(true)
	ui.podData.SetBorder(true).SetTitle("Details")
	ui.podEvents = tview.NewTable().SetBorders(true)
	ui.podEvents.SetBorder(true).SetTitle("Events")
	ui.statusBar = NewStatusBar()
	ui.actionBar = NewActionBar()

	ui.pages.AddPage(pagePods,
		tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(ui.namespaceDropDown, 1, 0, false).
			AddItem(
				tview.NewFlex().
					AddItem(ui.podsTree, 0, 1, false).
					AddItem(ui.podsDetails.AddItem(ui.podData, 0, 1, false), 0, 1, false),
				0, 1, true).
			AddItem(ui.statusBar, 1, 0, false).
			AddItem(ui.actionBar, 1, 0, false),
		true, false)

	ui.pages.AddPage(pageK8sError, ui.errorModal, true, false)

}
