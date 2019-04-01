package ui

import "github.com/rivo/tview"

const (
	pageK8sError = "error-k8s"
	pagePods     = "pods"
	pageLoading  = "loading"
)

func (ui *UI) setupPages() {
	ui.errorModal = tview.NewModal()
	ui.errorModal.SetTitle("Error")
	ui.namespaceDropDown = tview.NewDropDown().SetLabel("Namespace [Enter[]: ")
	ui.podsTree = tview.NewTreeView()
	ui.podsTree.SetBorder(true).SetTitle("Pods")
	ui.podsDetails = tview.NewTextView()
	ui.podsDetails.SetBorder(true).SetTitle("Details")

	ui.pages.AddPage(pagePods,
		tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(ui.namespaceDropDown, 1, 0, true).
			AddItem(
				tview.NewFlex().
					AddItem(ui.podsTree, 0, 1, true).
					AddItem(ui.podsDetails, 0, 1, true),
				0, 1, true),
		true, false)

	ui.pages.AddPage(pageLoading, tview.NewGrid().
		SetColumns(0, 0, 0).
		SetRows(0, 0, 0).
		AddItem(tview.NewTextView().
			SetText("Loading ..."), 1, 1, 1, 1, 0, 0, false), true, false)

	ui.pages.AddPage(pageK8sError, ui.errorModal, true, false)

}
