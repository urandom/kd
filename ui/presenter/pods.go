package presenter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/ext"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	cv1 "k8s.io/api/core/v1"
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
	detailsText
)

type Pods struct {
	Error
	picker  Picker
	details *Details
	events  *Events
	log     *Log
	editor  *Editor

	client *k8s.Client
	state  struct {
		activeComponent podsComponent
		namespace       string
		object          k8s.ObjectMetaGetter
		details         detailsView
		fullscreen      bool
	}
	cancelWatchFn       context.CancelFunc
	cancelNSFn          context.CancelFunc
	selectedActions     []ext.ObjectSelectedAction
	selectedActionsData ext.ObjectSelectedDataSlice

	mu sync.RWMutex
}

func NewPods(ui *ui.UI, client *k8s.Client, extManager ext.Manager) *Pods {
	p := &Pods{
		Error:   NewError(ui),
		picker:  NewPicker(ui),
		details: NewDetails(ui, client),
		events:  NewEvents(ui, client),
		log:     NewLog(ui, client),
		editor:  NewEditor(ui, client),
		client:  client,
	}
	p.state.namespace = "___"

	if err := extManager.Start(
		ext.Client(p.client),
		ext.PickFrom(p.picker.PickFrom),
		ext.DisplayText(p.showText),
		ext.DisplayObject(p.showObject),
		ext.RegisterObjectSelectAction(func(action ext.ObjectSelectedAction) {
			p.mu.Lock()
			p.selectedActions = append(p.selectedActions, action)
			p.mu.Unlock()
		}),
		ext.RegisterObjectSummaryProvider(p.details.RegisterObjectMutateActions),
	); err != nil {
		log.Println("Error starting extension manager:", err)
	}

	return p
}

const AllNamespaces = " ALL "

func (p *Pods) populateNamespaces() error {
	log.Println("Getting cluster namespaces")
	p.ui.StatusBar.SpinText("Loading namespaces")
	defer p.ui.StatusBar.StopSpin()
	p.ui.App.QueueUpdate(func() {
		p.ui.Pages.SwitchToPage(ui.PagePods)
	})

	if namespaces, err := p.client.Namespaces(); err == nil {
		if namespaces[0] == "" {
			namespaces[0] = AllNamespaces
		}
		p.ui.App.QueueUpdateDraw(func() {
			p.ui.NamespaceDropDown.SetOptions(namespaces, func(text string, idx int) {
				if text == p.state.namespace {
					return
				}
				p.ui.App.SetFocus(p.ui.PodsTree)
				p.onFocused(p.ui.PodsTree)
				go func() {
					if text == AllNamespaces {
						text = ""
					}
					p.DisplayError(p.populatePods(text))
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
	p.ui.StatusBar.SpinText("Loading pods")
	defer p.ui.StatusBar.StopSpin()

	if p.cancelNSFn != nil {
		p.cancelNSFn()
	}

	log.Printf("Getting pod tree for namespace %s", ns)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelNSFn = cancel

	c, err := p.client.PodTreeWatcher(ctx, ns)
	if err != nil {
		log.Printf("Error getting pod tree for namespaces %s: %s", ns, err)
		cancel()
		return UserRetryableError{err, func() error {
			return p.populatePods(ns)
		}}
	}

	// Start watching for updates to the pod tree
	go func() {
		for {
			select {
			case podWatcherEvent, open := <-c:
				if !open {
					return
				}
				p.ui.App.QueueUpdateDraw(func() {
					podTree := podWatcherEvent.Tree

					// Build a dual-level controller representation
					controllers := [][]k8s.Controller{}
					i := -1
					for _, c := range podTree.Controllers {
						if i == -1 || controllers[i][0].Category() != c.Category() {
							i++
							controllers = append(controllers, []k8s.Controller{})
						}

						controllers[i] = append(controllers[i], c)
					}

					log.Printf("Updating tree view with pods for namespaces %s", ns)
					p.state.namespace = ns

					root := p.ui.PodsTree.GetRoot()
					clsNodes := make([]*tview.TreeNode, 0, len(controllers))
					for i, c := range controllers {
						var clsNode *tview.TreeNode
						for _, node := range root.GetChildren() {
							if c[0].Category() == node.GetReference() {
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
							clsNode = tview.NewTreeNode(controllers[i][0].Category().Plural()).
								SetSelectable(true).
								SetColor(tcell.ColorCoral).
								SetReference(controllers[i][0].Category())
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

					if p.ui.PodsTree.GetCurrentNode() == nil && len(root.GetChildren()) > 0 {
						p.ui.PodsTree.SetCurrentNode(root.GetChildren()[0])
					}
				})

			}
		}
	}()

	p.ui.PodsTree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if _, ok := ref.(k8s.Category); ok {
			node.SetExpanded(!node.IsExpanded())
			return
		}

		p.onFocused(p.ui.PodsTree)
		p.showObject(ref.(k8s.ObjectMetaGetter))
	})

	return nil
}

func (p *Pods) setDetailsView(object tview.Primitive) {
	p.ui.App.QueueUpdateDraw(func() {
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
		case detailsText:
			p.ui.PodData.SetTitle("Text")
		}
	})
}

func (p *Pods) initKeybindings() {
	p.ui.App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch p.ui.App.GetFocus().(type) {
		case *tview.Button, *tview.InputField:
			return event
		}
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			var toFocus tview.Primitive
			switch p.ui.App.GetFocus() {
			case p.ui.PodsTree:
				switch p.state.details {
				case detailsObject, detailsLog, detailsText:
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
			if p.state.object != nil {
				go func() {
					p.state.details = detailsObject
					p.showObject(p.state.object)
				}()
				return nil
			}
		case tcell.KeyF2:
			if p.state.object != nil {
				go func() {
					p.state.details = detailsEvents
					p.showObject(p.state.object)
				}()
				return nil
			}
		case tcell.KeyF3:
			if c, ok := p.state.object.(k8s.Controller); !ok || len(c.Pods()) > 0 {
				go func() {
					p.state.details = detailsLog
					p.showObject(p.state.object)
				}()
				return nil
			}
		case tcell.KeyF4:
			if p.state.object != nil {
				switch p.state.details {
				case detailsObject:
					go func() {
						_, err := p.editor.edit(p.state.object)
						p.DisplayError(err)
					}()
				case detailsEvents:
				case detailsLog, detailsText:
					go func() {
						p.DisplayError(p.editor.viewText())
					}()
				}
				return nil
			}
		case tcell.KeyF5:
			p.refreshFocused()
			return nil
		case tcell.KeyF6:
			if _, ok := p.state.object.(k8s.Controller); !ok && p.state.object != nil {
				go func() {
					p.DisplayError(p.editor.delete(p.state.object))
				}()
				return nil
			}
		case tcell.KeyF7:
			if c, ok := p.state.object.(k8s.Controller); ok && c.Category() == k8s.CategoryDeployment {
				go func() {
					p.DisplayError(p.editor.scaleDeployment(c))
				}()
				return nil
			}
		case tcell.KeyF9:
			if p.state.object != nil && len(p.selectedActionsData) > 0 {
				go func() {
					choice := <-p.picker.PickFrom("More", p.selectedActionsData.Labels())
					selected := p.selectedActionsData.FindForLabel(choice)
					if selected.Valid() {
						p.DisplayError(selected.Callback())
					}
				}()
				return nil
			}
		case tcell.KeyF10:
			p.ui.App.Stop()
			return nil
		case tcell.KeyCtrlF:
			if p.state.activeComponent == podsDetails {
				p.state.fullscreen = !p.state.fullscreen
				p.ui.PodsDetails.SetFullScreen(p.state.fullscreen)
				return nil
			}
		case tcell.KeyEsc:
			if p.state.details == detailsText {
				p.state.details = detailsObject
				p.showObject(p.state.object)
			}
		}
		return event
	})
}

func (p *Pods) onFocused(primitive tview.Primitive) {
	p.state.activeComponent = primitiveToComponent(primitive)

	p.resetButtons()
	p.setupButtons()
}

func (p *Pods) resetButtons() {
	p.ui.ActionBar.Clear()
}

func (p *Pods) setupButtons() {
	if p.state.object != nil {
		p.ui.ActionBar.AddAction(1, "Details")
		p.ui.ActionBar.AddAction(2, "Events")
		if c, ok := p.state.object.(k8s.Controller); !ok || len(c.Pods()) > 0 {
			p.ui.ActionBar.AddAction(3, "Logs")
		}
		switch p.state.details {
		case detailsObject:
			p.ui.ActionBar.AddAction(4, "Edit")
		case detailsEvents:
		case detailsLog, detailsText:
			p.ui.ActionBar.AddAction(4, "View")
		}
	}
	if p.state.activeComponent == podsTree || p.state.object != nil {
		p.ui.ActionBar.AddAction(5, "Refresh")
	}
	if _, ok := p.state.object.(k8s.Controller); p.state.object != nil && !ok {
		p.ui.ActionBar.AddAction(6, "Delete")
	}
	if c, ok := p.state.object.(k8s.Controller); ok && c.Category() == k8s.CategoryDeployment {
		p.ui.ActionBar.AddAction(7, "Scale")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state.object != nil && len(p.selectedActions) > 0 {
		p.selectedActionsData = make(ext.ObjectSelectedDataSlice, 0, len(p.selectedActions))
		for _, action := range p.selectedActions {
			obj := p.state.object
			if c, ok := obj.(k8s.Controller); ok {
				obj = c.Controller()
			}
			data, err := action(obj)
			if p.DisplayError(err) {
				return
			}
			if !data.Valid() {
				continue
			}
			p.selectedActionsData = append(p.selectedActionsData, data)
		}
		if len(p.selectedActionsData) > 0 {
			p.selectedActionsData.SortByLabel()
			p.ui.ActionBar.AddAction(9, "More")
		}
	}
	p.ui.ActionBar.AddAction(10, "Quit")
}

func (p *Pods) refreshFocused() {
	switch p.state.activeComponent {
	case podsTree:
		go func() {
			p.DisplayError(p.populatePods(p.state.namespace))
		}()
	case podsDetails:
		switch p.state.details {
		case detailsEvents:
			p.ui.App.QueueEvent(tcell.NewEventKey(tcell.KeyF2, rune(tcell.KeyF2), tcell.ModNone))
		}
	}
}

func (p *Pods) showText(text string) {
	p.state.details = detailsText
	p.ui.App.QueueUpdateDraw(func() {
		p.ui.PodData.SetText(text).SetRegions(true).
			SetDynamicColors(true).ScrollToBeginning()
	})
	p.setDetailsView(p.ui.PodData)
}

func (p *Pods) showObject(obj k8s.ObjectMetaGetter) {
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

	p.state.object = obj

	if p.state.details == detailsText {
		p.state.details = detailsObject
	}

	switch p.state.details {
	case detailsObject:
		go func() {
			focused := p.details.show(obj)
			p.setDetailsView(focused)
		}()
	case detailsEvents:
		go func() {
			focused, err := p.events.show(obj)
			p.DisplayError(err)
			p.setDetailsView(focused)
		}()
	case detailsLog:
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			p.cancelWatchFn = cancel
			focused, err := p.log.show(ctx, obj, "")
			p.DisplayError(err)
			p.setDetailsView(focused)
		}()
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
