package ui

import (
	"github.com/gdamore/tcell"
	"gitlab.com/tslocum/cview"
)

const (
	PageModal = "modal"
	PagePods  = "pods"
)

func (ui *UI) setupPages() {
	ui.NamespaceDropDown = cview.NewDropDown().SetLabel("Namespace [CTRL-N[]: ")
	ui.PodsTree = cview.NewTreeView().SetTopLevel(1).SetRoot(cview.NewTreeNode("."))
	ui.PodsTree.SetBorder(true).SetTitle("Pods")
	ui.PodsDetails = cview.NewFlex()
	ui.PodData = cview.NewTextView().SetWrap(false).
		SetDynamicColors(true).SetText("[lightgreen]<- Select an object")
	ui.PodData.SetBorder(true).SetTitle("Details")
	ui.PodEvents = cview.NewTable().SetBorders(true)
	ui.PodEvents.SetBorder(true).SetTitle("Events")
	ui.StatusBar = NewStatusBar(ui.App)
	ui.ActionBar = NewActionBar(ui.InputEvents)

	ui.Pages.AddPage(PagePods,
		cview.NewFlex().
			SetDirection(cview.FlexRow).
			AddItem(ui.NamespaceDropDown, 1, 0, false).
			AddItem(
				cview.NewFlex().
					AddItem(ui.PodsTree, 0, 1, false).
					AddItem(ui.PodsDetails.AddItem(ui.PodData, 0, 1, false), 0, 1, false),
				0, 1, true).
			AddItem(ui.StatusBar, 1, 0, false).
			AddItem(ui.ActionBar, 1, 0, false),
		true, false)

	ui.PodsTree.SetMouseCapture(func(e *cview.EventMouse) *cview.EventMouse {
		if e.Action()&cview.MouseDown == 0 || e.Buttons()&tcell.Button1 == 0 {
			return e
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

		return e
	})
}
