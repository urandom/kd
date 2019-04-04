package ui

import (
	"fmt"

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
	ui.podsDetails = tview.NewTextView().SetWrap(false)
	ui.podsDetails.SetBorder(true).SetTitle("Details")
	ui.statusBar = tview.NewTextView().SetTextColor(tview.Styles.SecondaryTextColor).SetWrap(false)
	ui.statusBar.SetBorderPadding(0, 0, 1, 1)
	ui.buttonBar = tview.NewForm().
		AddButton(buttonEmpty, nil).
		AddButton(buttonEmpty, nil).
		AddButton(buttonEmpty, nil).
		AddButton(buttonEmpty, nil).
		AddButton(buttonEmpty, nil).
		AddButton(buttonEmpty, nil).
		AddButton(buttonLabel("10", buttonQuit), func() { ui.app.Stop() })
	ui.buttonBar.SetBorder(false).SetBorderPadding(0, 0, 0, 0)

	ui.pages.AddPage(pagePods,
		tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(ui.namespaceDropDown, 1, 0, false).
			AddItem(
				tview.NewFlex().
					AddItem(ui.podsTree, 0, 1, false).
					AddItem(ui.podsDetails, 0, 1, false),
				0, 1, true).
			AddItem(ui.statusBar, 1, 0, false).
			AddItem(ui.buttonBar, 1, 0, false),
		true, false)

	ui.pages.AddPage(pageK8sError, ui.errorModal, true, false)

}

func buttonLabel(number, text string) string {
	return fmt.Sprintf("[black]%s[maroon]%s", number, text)
}
