package presenter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type podsComponent int

const (
	podsTree podsComponent = iota
	podsDetails
)

type detailsView int

const (
	detailsObject detailsView = iota
	detailsEvents
	detailsLog
)

type Pods struct {
	*Error
	details *Details
	events  *Events
	log     *Log
	editor  *Editor

	client k8s.Client
	state  struct {
		activeComponent podsComponent
		namespace       string
		object          interface{}
		details         detailsView
		fullscreen      bool
	}
	cancelWatchFn context.CancelFunc
}

func NewPods(ui *ui.UI, client k8s.Client) *Pods {
	return &Pods{
		Error:   &Error{ui: ui},
		details: NewDetails(ui, client),
		events:  NewEvents(ui, client),
		log:     NewLog(ui, client),
		editor:  NewEditor(ui, client),
		client:  client,
	}
}

func (p *Pods) populateNamespaces() error {
	log.Println("Getting cluster namespaces")
	p.ui.StatusBar.SpinText("Loading namespaces", p.ui.App)
	defer p.ui.StatusBar.StopSpin()
	p.ui.App.QueueUpdate(func() {
		p.ui.Pages.SwitchToPage(ui.PagePods)
	})

	if namespaces, err := p.client.Namespaces(); err == nil {
		p.ui.App.QueueUpdateDraw(func() {
			p.ui.NamespaceDropDown.SetOptions(namespaces, func(text string, idx int) {
				if text == p.state.namespace {
					return
				}
				p.ui.App.SetFocus(p.ui.PodsTree)
				p.onFocused(p.ui.PodsTree)
				go func() {
					p.displayError(p.populatePods(text))
				}()
			})
			found := false
			for i := range namespaces {
				if namespaces[i] == p.state.namespace {
					p.ui.NamespaceDropDown.SetCurrentOption(i)
					found = true
					break
				}
			}
			if !found {
				p.ui.NamespaceDropDown.SetCurrentOption(0)
			}
		})

		return nil
	} else {
		log.Println("Error getting cluster namespaces:", err)
		return UserRetryableError{err, p.populateNamespaces}
	}
}

func (p *Pods) populatePods(ns string) error {
	p.ui.StatusBar.SpinText("Loading pods", p.ui.App)
	defer p.ui.StatusBar.StopSpin()

	log.Printf("Getting pod tree for namespace %s", ns)
	podTree, err := p.client.PodTree(ns)
	go func() {
		c, err := p.client.PodTreeWatcher(context.Background(), ns)
		log.Println(err)
		for {
			select {
			case <-c:
			}
		}
	}()
	if err != nil {
		log.Printf("Error getting pod tree for namespaces %s: %s", ns, err)
		return UserRetryableError{err, func() error {
			return p.populatePods(ns)
		}}
	}

	controllerNames := []string{"Stateful Sets", "Deployments", "Daemon Sets", "Jobs", "Cron Jobs", "Services"}
	controllers := [][]k8s.Controller{{}, {}, {}, {}, {}, {}}
	for _, c := range podTree.StatefulSets {
		controllers[0] = append(controllers[0], c)
	}
	for _, c := range podTree.Deployments {
		controllers[1] = append(controllers[1], c)
	}
	for _, c := range podTree.DaemonSets {
		controllers[2] = append(controllers[2], c)
	}
	for _, c := range podTree.Jobs {
		controllers[3] = append(controllers[3], c)
	}
	for _, c := range podTree.CronJobs {
		controllers[4] = append(controllers[4], c)
	}
	for _, c := range podTree.Services {
		controllers[5] = append(controllers[5], c)
	}

	log.Printf("Updating tree view with pods for namespaces %s", ns)
	p.state.namespace = ns

	root := p.ui.PodsTree.GetRoot()
	clsNodes := make([]*tview.TreeNode, 0, len(controllers))
	for i, c := range controllers {
		var clsNode *tview.TreeNode
		for _, node := range root.GetChildren() {
			if i == node.GetReference() {
				if len(c) == 0 {
					// Category is empty, remove the class node
					break
				}

				clsNode = node
				break
			}
		}

		if clsNode == nil && len(c) > 0 {
			// class not found, but category not empty
			clsNode = tview.NewTreeNode(controllerNames[i]).
				SetSelectable(true).
				SetColor(tcell.ColorCoral).
				SetReference(i)
		}

		if clsNode == nil {
			continue
		}

		conNodes := make([]*tview.TreeNode, 0, len(c))
		for _, controller := range c {
			conName := controller.Controller().GetObjectMeta().GetName()
			var conNode *tview.TreeNode
			for _, node := range clsNode.GetChildren() {
				ref := node.GetReference().(k8s.Controller)
				if conName == ref.Controller().GetObjectMeta().GetName() {

					podNodes := make([]*tview.TreeNode, 0, len(controller.Pods()))
					for _, pod := range controller.Pods() {
						var podNode *tview.TreeNode
						for _, pNode := range node.GetChildren() {
							podRef := pNode.GetReference().(*cv1.Pod)
							if podRef.GetName() == pod.GetName() {
								podNode = pNode
								podNode.SetReference(pod)
								break
							}
						}

						if podNode == nil {
							podNode = tview.NewTreeNode(pod.GetObjectMeta().GetName()).
								SetReference(pod).SetSelectable(true)
						}

						podNodes = append(podNodes, podNode)
					}

					conNode = node
					conNode.SetReference(controller)
					conNode.SetChildren(podNodes)
					break
				}
			}

			if conNode == nil {
				// controller not found
				conNode = tview.NewTreeNode(conName).
					SetReference(controller).SetSelectable(true)
				for _, pod := range controller.Pods() {
					podNode := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					conNode.AddChild(podNode)
				}
			}

			conNodes = append(conNodes, conNode)
		}
		clsNode.SetChildren(conNodes)

		clsNodes = append(clsNodes, clsNode)
	}
	root.SetChildren(clsNodes)

	p.ui.App.QueueUpdateDraw(func() {
		if p.ui.PodsTree.GetCurrentNode() == nil && len(root.GetChildren()) > 0 {
			p.ui.PodsTree.SetCurrentNode(root.GetChildren()[0])
		}
	})

	p.ui.PodsTree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if _, ok := ref.(int); ok {
			node.SetExpanded(!node.IsExpanded())
			return
		}

		p.state.object = ref
		p.onFocused(p.ui.PodsTree)
		if p.cancelWatchFn != nil {
			p.cancelWatchFn()
		}
		switch p.state.details {
		case detailsObject:
			go func() {
				focused := p.details.show(ref)
				p.setDetailsView(focused)
			}()
		case detailsEvents:
			go func() {
				focused, err := p.events.show(ref)
				p.displayError(err)
				p.setDetailsView(focused)
			}()
		case detailsLog:
			go func() {
				ctx, cancel := context.WithCancel(context.Background())
				p.cancelWatchFn = cancel
				focused, err := p.log.show(ctx, ref, "")
				p.displayError(err)
				p.setDetailsView(focused)
			}()
		}
	})

	return nil
}

func (p *Pods) setDetailsView(object tview.Primitive) {
	p.ui.PodsDetails.RemoveItem(p.ui.PodData)
	p.ui.PodsDetails.RemoveItem(p.ui.PodEvents)
	p.ui.PodsDetails.AddItem(object, 0, 1, false)
	p.onFocused(object)

	switch p.state.details {
	case detailsObject:
		p.ui.PodData.SetTitle("Details")
	case detailsEvents:
	case detailsLog:
		p.ui.PodData.SetTitle("Logs")
	}
}

func (p *Pods) initKeybindings() {
	p.ui.App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			var toFocus tview.Primitive
			switch p.ui.App.GetFocus() {
			case p.ui.PodsTree:
				switch p.state.details {
				case detailsObject, detailsLog:
					toFocus = p.ui.PodData
				case detailsEvents:
					toFocus = p.ui.PodEvents
				}
			case p.ui.PodData, p.ui.PodEvents:
				toFocus = p.ui.PodsTree
			default:
				toFocus = p.ui.PodsTree
			}
			p.ui.App.SetFocus(toFocus)
			p.onFocused(toFocus)
			return nil
		case tcell.KeyCtrlN:
			p.ui.App.SetFocus(p.ui.NamespaceDropDown)
			p.ui.App.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, rune(tcell.KeyEnter), tcell.ModNone))
			return nil
		case tcell.KeyF1:
			p.ui.App.SetFocus(p.ui.PodData)
			p.onFocused(p.ui.PodData)
			if p.state.object != nil {
				go func() {
					if p.cancelWatchFn != nil {
						p.cancelWatchFn()
					}
					focused := p.details.show(p.state.object)
					p.state.details = detailsObject
					p.ui.App.SetFocus(focused)
					p.setDetailsView(focused)
				}()
				return nil
			}
		case tcell.KeyF2:
			if p.state.object != nil {
				go func() {
					if p.cancelWatchFn != nil {
						p.cancelWatchFn()
					}
					focused, err := p.events.show(p.state.object)
					p.displayError(err)
					p.state.details = detailsEvents
					p.ui.App.SetFocus(focused)
					p.setDetailsView(focused)
				}()
				return nil
			}
		case tcell.KeyF3:
			if p.state.object != nil {
				go func() {
					if p.cancelWatchFn != nil {
						p.cancelWatchFn()
					}
					ctx, cancel := context.WithCancel(context.Background())
					p.cancelWatchFn = cancel

					focused, err := p.log.show(ctx, p.state.object, "")
					p.displayError(err)
					p.state.details = detailsLog
					p.ui.App.SetFocus(focused)
					p.setDetailsView(focused)
				}()
				return nil
			}
		case tcell.KeyF4:
			if p.state.object != nil {
				switch p.state.details {
				case detailsObject:
					go func() {
						_, err := p.editor.edit(p.state.object)
						p.displayError(err)
					}()
				case detailsEvents:
				case detailsLog:
					go func() {
						p.displayError(p.editor.viewLog())
					}()
				}
				return nil
			}
		case tcell.KeyF5:
			p.refreshFocused()
			return nil
		case tcell.KeyF10:
			p.ui.App.Stop()
			return nil
		case tcell.KeyCtrlF:
			if p.state.activeComponent == podsDetails {
				p.state.fullscreen = !p.state.fullscreen
				p.ui.PodsDetails.SetFullScreen(p.state.fullscreen)
				return nil
			}
		}
		return event
	})
}

func (p *Pods) onFocused(primitive tview.Primitive) {
	p.state.activeComponent = primitiveToComponent(primitive)

	p.resetButtons()
	switch p.state.activeComponent {
	case podsTree:
		p.buttonsForPodsTree()
	case podsDetails:
		p.buttonsForPodsDetails()
	}
}

func (p Pods) resetButtons() {
	p.ui.ActionBar.Clear()
}

func (p *Pods) buttonsForPodsTree() {
	if p.state.object != nil {
		p.ui.ActionBar.AddAction(1, "Details")
		p.ui.ActionBar.AddAction(2, "Events")
		p.ui.ActionBar.AddAction(3, "Logs")
		switch p.state.details {
		case detailsObject:
			p.ui.ActionBar.AddAction(4, "Edit")
		case detailsEvents:
		case detailsLog:
			p.ui.ActionBar.AddAction(4, "View")
		}
	}
	p.ui.ActionBar.AddAction(5, "Refresh")
	p.ui.ActionBar.AddAction(10, "Quit")
}

func (p *Pods) buttonsForPodsDetails() {
	if p.state.object != nil {
		p.ui.ActionBar.AddAction(1, "Details")
		p.ui.ActionBar.AddAction(2, "Events")
		p.ui.ActionBar.AddAction(3, "Logs")
		switch p.state.details {
		case detailsObject:
			p.ui.ActionBar.AddAction(4, "Edit")
		case detailsEvents:
		case detailsLog:
			p.ui.ActionBar.AddAction(4, "View")
		}
		p.ui.ActionBar.AddAction(5, "Refresh")
	}
	p.ui.ActionBar.AddAction(10, "Quit")
}

func (p *Pods) refreshFocused() {
	switch p.state.activeComponent {
	case podsTree:
		go func() {
			p.displayError(p.populatePods(p.state.namespace))
		}()
	case podsDetails:
		switch p.state.details {
		case detailsEvents:
			p.ui.App.QueueEvent(tcell.NewEventKey(tcell.KeyF2, rune(tcell.KeyF2), tcell.ModNone))
		}
	}
}

func primitiveToComponent(p tview.Primitive) podsComponent {
	switch p.(type) {
	case *tview.TreeView:
		return podsTree
	case *tview.TextView, *tview.Table:
		return podsDetails
	default:
		return podsTree
	}
}

var errNotObjMeta = errors.New("object does not have meta data")

func objectMeta(object interface{}) (meta.Object, error) {
	if c, ok := object.(k8s.Controller); ok {
		return c.Controller().GetObjectMeta(), nil
	}

	if g, ok := object.(k8s.ObjectMetaGetter); ok {
		return g.GetObjectMeta(), nil
	}

	return nil, errNotObjMeta
}

func portsToString(ports []cv1.ServicePort) string {
	parts := make([]string, len(ports))
	for ix := range ports {
		port := &ports[ix]
		parts[ix] = fmt.Sprintf("%d/%s", port.Port, port.Protocol)
		if port.NodePort > 0 {
			parts[ix] = fmt.Sprintf("%d:%d/%s", port.Port, port.NodePort, port.Protocol)
		}
	}
	return strings.Join(parts, ",")
}
