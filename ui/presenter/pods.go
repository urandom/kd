package presenter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/urandom/kd/ext"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"gitlab.com/tslocum/cview"
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if namespaces, err := p.client.Namespaces(ctx); err == nil {
		opts := make([]*cview.DropDownOption, len(namespaces))
		if namespaces[0] == "" {
			namespaces[0] = AllNamespaces
		}
		for i, ns := range namespaces {
			opts[i] = cview.NewDropDownOption(ns)
		}
		p.ui.App.QueueUpdateDraw(func() {
			p.ui.NamespaceDropDown.SetOptions(func(idx int, opt *cview.DropDownOption) {
				text := opt.GetText()
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
			}, opts...)
			found := false
			for i := range namespaces {
				if namespaces[i] == p.state.namespace {
					p.ui.NamespaceDropDown.SetCurrentOption(i)
					found = true
					break
				}
			}
			if !found {
				if len(namespaces) > 1 && namespaces[0] == AllNamespaces {
					p.ui.NamespaceDropDown.SetCurrentOption(1)
				}
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

	log.Printf("Getting pod tree for namespace %q", ns)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelNSFn = cancel

	c, err := p.client.PodTreeWatcher(ctx, ns)
	if err != nil {
		log.Printf("Error getting pod tree for namespaces %q: %s", ns, err)
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
					clsNodes := make([]*cview.TreeNode, 0, len(controllers))
					for i, c := range controllers {
						names := map[string]struct{}{}
						var clsNode *cview.TreeNode
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
							clsNode = cview.NewTreeNode(controllers[i][0].Category().Plural())
							clsNode.SetSelectable(true)
							clsNode.SetColor(tcell.ColorCoral)
							clsNode.SetReference(controllers[i][0].Category())
							clsNode.SetSelectedFunc(func() {
								clsNode.SetExpanded(!clsNode.IsExpanded())
							})
						}

						if clsNode == nil {
							continue
						}

						conNodes := make([]*cview.TreeNode, 0, len(c))
						for _, controller := range c {
							controller := controller
							conName := controller.GetObjectMeta().GetName()
							uid := controller.GetObjectMeta().GetUID()
							var conNode *cview.TreeNode
							for _, node := range clsNode.GetChildren() {
								ref := node.GetReference().(k8s.Controller)
								if uid == ref.GetObjectMeta().GetUID() {
									podNodes := make([]*cview.TreeNode, 0, len(controller.Pods()))
									for _, pod := range controller.Pods() {
										pod := pod
										var podNode *cview.TreeNode
										for _, pNode := range node.GetChildren() {
											podRef := pNode.GetReference().(*cv1.Pod)
											if podRef.GetUID() == pod.GetUID() {
												podNode = pNode
												podNode.SetReference(pod)
												podNode.SetFocusedFunc(func() {
													p.onFocused(p.ui.PodsTree)
													p.showObject(pod)
												})
												break
											}
										}

										if podNode == nil {
											name := pod.GetName()
											if _, present := names[name]; present {
												name = pod.GetNamespace() + "/" + name
											}
											names[name] = struct{}{}
											podNode = cview.NewTreeNode(name)
											podNode.SetReference(pod)
											podNode.SetSelectable(true)
											podNode.SetFocusedFunc(func() {
												p.onFocused(p.ui.PodsTree)
												p.showObject(pod)
											})
										}

										podNodes = append(podNodes, podNode)
									}

									conNode = node
									conNode.SetReference(controller)
									conNode.SetFocusedFunc(func() {
										p.onFocused(p.ui.PodsTree)
										p.showObject(controller)
									})
									conNode.SetChildren(podNodes)
									break
								}
							}

							// controller not found
							if conNode == nil {
								// Ensure somewhat unique names
								if _, present := names[conName]; present {
									conName = controller.GetObjectMeta().GetNamespace() + "/" + conName
								}
								names[conName] = struct{}{}

								conNode = cview.NewTreeNode(conName)
								conNode.SetReference(controller)
								conNode.SetSelectable(true)
								conNode.SetFocusedFunc(func() {
									p.onFocused(p.ui.PodsTree)
									p.showObject(controller)
								})
								for _, pod := range controller.Pods() {
									pod := pod
									podNode := cview.NewTreeNode(pod.GetObjectMeta().GetName())
									podNode.SetReference(pod)
									podNode.SetSelectable(true)
									podNode.SetFocusedFunc(func() {
										p.onFocused(p.ui.PodsTree)
										p.showObject(pod)
									})
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

	return nil
}

func (p *Pods) setDetailsView(object cview.Primitive) {
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
	p.ui.InputEvents.Add("pods", func(event *tcell.EventKey) *tcell.EventKey {
		switch p.ui.App.GetFocus().(type) {
		case *cview.Button, *cview.InputField:
			return event
		}
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			var toFocus cview.Primitive
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

func (p *Pods) onFocused(primitive cview.Primitive) {
	p.state.activeComponent = primitiveToComponent(primitive)

	p.ui.App.QueueUpdate(func() {
		p.resetButtons()
		p.setupButtons()
	})
}

func (p *Pods) resetButtons() {
	p.ui.ActionBar.Clear()
}

func (p *Pods) setupButtons() {
	if p.state.object != nil {
		p.ui.ActionBar.AddAction(1, "Details", tcell.NewEventKey(tcell.KeyF1, 0x0, 0), func() bool {
			if p.state.object != nil {
				go func() {
					p.state.details = detailsObject
					p.showObject(p.state.object)
				}()
				return true
			}
			return false
		})
		p.ui.ActionBar.AddAction(2, "Events", tcell.NewEventKey(tcell.KeyF2, 0x0, 0), func() bool {
			if p.state.object != nil {
				go func() {
					p.state.details = detailsEvents
					p.showObject(p.state.object)
				}()
				return true
			}
			return false
		})
		if c, ok := p.state.object.(k8s.Controller); !ok || len(c.Pods()) > 0 {
			p.ui.ActionBar.AddAction(3, "Logs", tcell.NewEventKey(tcell.KeyF3, 0x0, 0), func() bool {
				if c, ok := p.state.object.(k8s.Controller); p.state.object != nil && (!ok || len(c.Pods()) > 0) {
					go func() {
						p.state.details = detailsLog
						p.showObject(p.state.object)
					}()
					return true
				}
				return false
			})
		}
		switch p.state.details {
		case detailsObject:
			p.ui.ActionBar.AddAction(4, "Edit", tcell.NewEventKey(tcell.KeyF4, 0x0, 0), func() bool {
				if p.state.object != nil {
					switch p.state.details {
					case detailsObject:
						go func() {
							_, err := p.editor.edit(p.state.object)
							p.DisplayError(err)
						}()
						return true
					}
				}
				return false
			})
		case detailsEvents:
		case detailsLog, detailsText:
			p.ui.ActionBar.AddAction(4, "View", tcell.NewEventKey(tcell.KeyF4, 0x0, 0), func() bool {
				if p.state.object != nil {
					switch p.state.details {
					case detailsLog, detailsText:
						go func() {
							p.DisplayError(p.editor.viewText())
						}()
						return true
					}
				}
				return false
			})
		}
	}
	if p.state.activeComponent == podsTree || p.state.object != nil {
		p.ui.ActionBar.AddAction(5, "Refresh", tcell.NewEventKey(tcell.KeyF5, 0x0, 0), func() bool {
			p.refreshFocused()
			return true
		})
	}
	if _, ok := p.state.object.(k8s.Controller); p.state.object != nil && !ok {
		p.ui.ActionBar.AddAction(6, "Delete", tcell.NewEventKey(tcell.KeyF6, 0x0, 0), func() bool {
			if _, ok := p.state.object.(k8s.Controller); !ok && p.state.object != nil {
				go func() {
					p.DisplayError(p.editor.delete(p.state.object))
				}()
				return true
			}
			return false
		})
	}
	if c, ok := p.state.object.(k8s.Controller); ok && c.Category() == k8s.CategoryDeployment {
		p.ui.ActionBar.AddAction(7, "Scale", tcell.NewEventKey(tcell.KeyF7, 0x0, 0), func() bool {
			if c, ok := p.state.object.(k8s.Controller); ok && c.Category() == k8s.CategoryDeployment {
				go func() {
					p.DisplayError(p.editor.scaleDeployment(c))
				}()
				return true
			}
			return false
		})
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
			p.ui.ActionBar.AddAction(9, "More", tcell.NewEventKey(tcell.KeyF9, 0x0, 0), func() bool {
				if p.state.object != nil && len(p.selectedActionsData) > 0 {
					go func() {
						choice := <-p.picker.PickFrom("More", p.selectedActionsData.Labels())
						selected := p.selectedActionsData.FindForLabel(choice)
						if selected.Valid() {
							p.DisplayError(selected.Callback())
						}
					}()
					return true
				}
				return false
			})
		}
	}
	p.ui.ActionBar.AddAction(10, "Quit", tcell.NewEventKey(tcell.KeyF10, 0x0, 0), func() bool {
		p.ui.App.Stop()
		return true
	})
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
		p.ui.PodData.SetText(text)
		p.ui.PodData.SetRegions(true)
		p.ui.PodData.SetDynamicColors(true)
		p.ui.PodData.ScrollToBeginning()
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

func primitiveToComponent(p cview.Primitive) podsComponent {
	switch p.(type) {
	case *cview.TreeView:
		return podsTree
	case *cview.TextView, *cview.Table:
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
