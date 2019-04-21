package ui

import (
	"github.com/rivo/tview"
)

const (
	PageK8sError = "error-k8s"
	PagePicker   = "picker"
	PagePods     = "pods"
)

func (ui *UI) setupPages() {
	ui.ErrorModal = tview.NewModal()
	ui.ErrorModal.SetTitle("Error")
	ui.NamespaceDropDown = tview.NewDropDown().SetLabel("Namespace [CTRL-N[]: ")
	ui.Picker = NewModalList()
	ui.Picker.List().SetBackgroundColor(tview.Styles.ContrastBackgroundColor).SetBorder(true)
	ui.PodsTree = tview.NewTreeView().SetTopLevel(1).SetRoot(tview.NewTreeNode("."))
	ui.PodsTree.SetBorder(true).SetTitle("Pods")
	ui.PodsDetails = tview.NewFlex()
	ui.PodData = tview.NewTextView().SetWrap(false)
	ui.PodData.SetBorder(true).SetTitle("Details")
	ui.PodEvents = tview.NewTable().SetBorders(true)
	ui.PodEvents.SetBorder(true).SetTitle("Events")
	ui.StatusBar = NewStatusBar()
	ui.ActionBar = NewActionBar()

	ui.Pages.AddPage(PagePods,
		tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(ui.NamespaceDropDown, 1, 0, false).
			AddItem(
				tview.NewFlex().
					AddItem(ui.PodsTree, 0, 1, false).
					AddItem(ui.PodsDetails.AddItem(ui.PodData, 0, 1, false), 0, 1, false),
				0, 1, true).
			AddItem(ui.StatusBar, 1, 0, false).
			AddItem(ui.ActionBar, 1, 0, false),
		true, false).
		AddPage(PagePicker, ui.Picker, true, false).
		AddPage(PageK8sError, ui.ErrorModal, true, false)

}
