package ui

import (
	"github.com/gdamore/tcell/v2"
	"gitlab.com/tslocum/cview"
)

const (
	PageModal = "modal"
	PagePods  = "pods"
)

func (ui *UI) setupPages() {
	ui.NamespaceDropDown = cview.NewDropDown()
	ui.NamespaceDropDown.SetLabel("Namespace [CTRL-N[]: ")

	ui.PodsTree = cview.NewTreeView()
	ui.PodsTree.SetTopLevel(1)
	ui.PodsTree.SetRoot(cview.NewTreeNode("."))
	ui.PodsTree.SetBorder(true)
	ui.PodsTree.SetTitle("Pods")

	ui.PodsDetails = cview.NewFlex()
	ui.PodData = cview.NewTextView()
	ui.PodData.SetWrap(false)
	ui.PodData.SetDynamicColors(true)
	ui.PodData.SetText("[lightgreen]<- Select an object")
	ui.PodData.SetBorder(true)
	ui.PodData.SetTitle("Details")
	ui.PodsDetails.AddItem(ui.PodData, 0, 1, false)

	ui.PodEvents = cview.NewTable()
	ui.PodEvents.SetBorders(true)
	ui.PodEvents.SetBorder(true)
	ui.PodEvents.SetTitle("Events")

	ui.StatusBar = NewStatusBar(ui.App)
	ui.ActionBar = NewActionBar(ui.InputEvents)

	contentFlex := cview.NewFlex()
	contentFlex.AddItem(ui.PodsTree, 0, 1, false)
	contentFlex.AddItem(ui.PodsDetails, 0, 1, false)

	flex := cview.NewFlex()
	flex.SetDirection(cview.FlexRow)
	flex.AddItem(ui.NamespaceDropDown, 1, 0, false)
	flex.AddItem(contentFlex, 0, 1, true)
	flex.AddItem(ui.StatusBar, 1, 0, false)
	flex.AddItem(ui.ActionBar, 1, 0, false)
	ui.Pages.AddPage(PagePods, flex, true, false)

	ui.PodsTree.SetMouseCapture(func(action cview.MouseAction, e *tcell.EventMouse) (cview.MouseAction, *tcell.EventMouse) {
		if action&cview.MouseLeftDown == 0 || e.Buttons()&tcell.Button1 == 0 || !ui.PodsTree.InRect(e.Position()) {
			return action, e
		}
		_, y, _, _ := ui.PodsTree.GetInnerRect()
		offset := ui.PodsTree.GetScrollOffset()
		_, evY := e.Position()
		parent := ui.PodsTree.GetRoot()

		var (
			count      int
			countNodes func(parent *cview.TreeNode)
		)
		countNodes = func(parent *cview.TreeNode) {
			if !parent.IsExpanded() {
				return
			}
			for _, n := range parent.GetChildren() {
				count++

				if evY-y+offset+1 == count {
					ui.PodsTree.SetCurrentNode(n)
					return
				}
				countNodes(n)
			}
		}

		countNodes(parent)

		return action, nil
	})
}
