package ui

import (
	"fmt"
	"log"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"golang.org/x/xerrors"
	yaml "gopkg.in/yaml.v2"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FatalError struct {
	error
}

type UserRetryableError struct {
	error
	RetryOp func() error
}

type ClientFactory func() (k8s.Client, error)

type ErrorPresenter struct {
	ui *UI

	isModalVisible bool
	focused        tview.Primitive
}

const (
	buttonQuit    = "Quit"
	buttonClose   = "Close"
	buttonRetry   = "Retry"
	buttonRefresh = "Refre"
	buttonEmpty   = "      "
)

func (p *ErrorPresenter) displayError(err error) bool {
	if err == nil {
		if p.isModalVisible {
			p.ui.app.QueueUpdateDraw(func() {
				p.ui.pages.HidePage(pageK8sError)
			})
		}
		return false
	}

	var buttons []string

	if xerrors.As(err, &FatalError{}) {
		buttons = append(buttons, buttonQuit)
	} else {
		buttons = append(buttons, buttonClose)
	}

	if xerrors.As(err, &UserRetryableError{}) {
		buttons = append(buttons, buttonRetry)
	}

	p.ui.app.QueueUpdateDraw(func() {
		p.ui.errorModal.
			SetText(fmt.Sprintf("Error creating k8s client: %s", err)).
			ClearButtons().
			AddButtons(buttons).
			SetDoneFunc(func(idx int, label string) {
				switch label {
				case buttonQuit:
					p.ui.app.Stop()
				case buttonRetry:
					go func() {
						p.displayError(err.(UserRetryableError).RetryOp())
					}()
					fallthrough
				case buttonClose:
					p.isModalVisible = false
					p.ui.pages.HidePage(pageK8sError)
					p.ui.app.SetFocus(p.focused)
				}
			})
		p.isModalVisible = true
		p.focused = p.ui.app.GetFocus()
		p.ui.pages.ShowPage(pageK8sError)
		p.ui.app.SetFocus(p.ui.errorModal)
	})

	return true
}

type MainPresenter struct {
	*ErrorPresenter

	clientFactory ClientFactory

	client        k8s.Client
	podsPresenter *PodsPresenter
}

func NewMainPresenter(ui *UI, clientFactory ClientFactory) *MainPresenter {
	return &MainPresenter{
		ErrorPresenter: &ErrorPresenter{ui: ui},
		clientFactory:  clientFactory,
	}
}

func (p *MainPresenter) Run() error {
	go func() {
		if !p.displayError(p.initClient()) {
			p.podsPresenter = NewPodsPresenter(p.ui, p.client)
			p.podsPresenter.initKeybindings()
			p.displayError(p.podsPresenter.populateNamespaces())
		}
	}()

	return p.ui.app.Run()
}

func (p *MainPresenter) initClient() error {
	var err error
	log.Println("Creating k8s client")
	if p.client, err = p.clientFactory(); err != nil {
		log.Println("Error creating k8s client:", err)

		return FatalError{err}
	}

	return nil
}

type podsComponent int

const (
	podsNamespace podsComponent = iota
	podsTree
	podsDetails
	podsButtons
)

type detailsView int

const (
	detailsObject detailsView = iota
	detailsEvents
)

type PodsPresenter struct {
	*ErrorPresenter

	client k8s.Client
	state  struct {
		activeComponent podsComponent
		namespace       string
		object          meta.Object
		details         detailsView
	}
}

func NewPodsPresenter(ui *UI, client k8s.Client) *PodsPresenter {
	return &PodsPresenter{
		ErrorPresenter: &ErrorPresenter{ui: ui},
		client:         client,
	}
}

func (p *PodsPresenter) initKeybindings() {
	p.ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF2:
			if p.state.activeComponent == podsDetails {
				p.showEvents()
				return nil
			}
		case tcell.KeyF5:
			p.refreshFocused()
			return nil
		case tcell.KeyF10:
			p.ui.app.Stop()
			return nil
		}
		return event
	})
}

func (p *PodsPresenter) populateNamespaces() error {
	log.Println("Getting cluster namespaces")
	defer p.ui.statusBar.StopSpin()
	p.ui.app.QueueUpdate(func() {
		p.ui.pages.SwitchToPage(pagePods)
		p.ui.statusBar.SpinText("Loading namespaces", p.ui.app)
	})

	if namespaces, err := p.client.Namespaces(); err == nil {
		p.ui.app.QueueUpdateDraw(func() {
			p.ui.namespaceDropDown.SetOptions(namespaces, func(text string, idx int) {
				if text == p.state.namespace {
					return
				}
				go func() {
					p.displayError(p.populatePods(text, true))
				}()
			})
			found := false
			for i := range namespaces {
				if namespaces[i] == p.state.namespace {
					p.ui.namespaceDropDown.SetCurrentOption(i)
					found = true
					break
				}
			}
			if !found {
				p.ui.namespaceDropDown.SetCurrentOption(0)
			}
			p.ui.app.SetFocus(p.ui.namespaceDropDown)
			p.onFocused(p.ui.namespaceDropDown)
			p.state.activeComponent = podsNamespace

			p.cycleFocusCapture(p.ui.namespaceDropDown, p.ui.podsDetails, p.ui.podsTree)
			p.cycleFocusCapture(p.ui.podsTree, p.ui.namespaceDropDown, p.ui.podsDetails)
			p.cycleFocusCapture(p.ui.podsDetails, p.ui.podsTree, p.ui.namespaceDropDown)
		})

		return nil
	} else {
		log.Println("Error getting cluster namespaces:", err)
		return UserRetryableError{err, p.populateNamespaces}
	}
}

type InputCapturer interface {
	SetInputCapture(capture func(event *tcell.EventKey) *tcell.EventKey) *tview.Box
}

func (p *PodsPresenter) cycleFocusCapture(on InputCapturer, prev, next tview.Primitive) {
	on.SetInputCapture(cycleFocusCapture(p.ui.app, prev, next, p.onFocused))
}

func (p *PodsPresenter) populatePods(ns string, clear bool) error {
	defer p.ui.statusBar.StopSpin()
	p.ui.app.QueueUpdateDraw(func() {
		p.ui.podsTree.SetRoot(tview.NewTreeNode(""))
		p.ui.statusBar.SpinText("Loading pods", p.ui.app)
	})

	log.Printf("Getting pod tree for namespace %s", ns)
	podTree, err := p.client.PodTree(ns)
	if err != nil {
		log.Printf("Error getting pod tree for namespaces %s: %s", ns, err)
		return UserRetryableError{err, func() error {
			return p.populatePods(ns, clear)
		}}
	}

	p.state.namespace = ns
	p.ui.app.QueueUpdateDraw(func() {
		log.Printf("Updating tree view with pods for namespaces %s", ns)
		root := tview.NewTreeNode(".")
		p.ui.podsTree.SetRoot(root)

		if len(podTree.Deployments) > 0 {
			dn := tview.NewTreeNode("Deployments").SetSelectable(true)
			root.AddChild(dn)

			for _, deployment := range podTree.Deployments {
				d := tview.NewTreeNode(deployment.GetObjectMeta().GetName()).
					SetReference(deployment).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range deployment.Pods {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(root.GetChildren()) > 0 {
			p.ui.podsTree.SetCurrentNode(root.GetChildren()[0])
		}
	})

	p.ui.podsTree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref == nil {
			node.SetExpanded(!node.IsExpanded())
			return
		}

		p.state.object = ref.(meta.Object)
		if data, err := yaml.Marshal(ref); err == nil {
			p.ui.podsDetails.SetText(string(data))
		} else {
			p.ui.podsDetails.SetText(err.Error())
		}
	})

	return nil
}

func (p *PodsPresenter) onFocused(primitive tview.Primitive) {
	p.state.activeComponent = primitiveToComponent(primitive)

	p.resetButtons()
	switch p.state.activeComponent {
	case podsNamespace:
		p.buttonsForNamespaces()
	case podsTree:
		p.buttonsForPodsTree()
	case podsDetails:
		p.buttonsForPodsDetails()
	}
}

func (p PodsPresenter) resetButtons() {
	p.ui.actionBar.Clear()
}

func (p *PodsPresenter) buttonsForNamespaces() {
	p.ui.actionBar.AddAction(5, "Refresh")
	p.ui.actionBar.AddAction(10, "Quit")
}

func (p *PodsPresenter) buttonsForPodsTree() {
	p.ui.actionBar.AddAction(5, "Refresh")
	p.ui.actionBar.AddAction(10, "Quit")
}

func (p *PodsPresenter) buttonsForPodsDetails() {
	p.ui.actionBar.AddAction(2, "Events")
	p.ui.actionBar.AddAction(10, "Quit")
}

func (p *PodsPresenter) showEvents() {
}

func (p *PodsPresenter) refreshFocused() {
	switch p.state.activeComponent {
	case podsNamespace:
		go func() {
			p.displayError(p.populateNamespaces())
		}()
	case podsTree:
		go func() {
			p.displayError(p.populatePods(p.state.namespace, false))
		}()
	case podsDetails:
	}
}

func cycleFocusCapture(app *tview.Application, prev, next tview.Primitive, focused func(p tview.Primitive)) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			if next != nil {
				app.SetFocus(next)
				focused(next)
				return nil
			}
		case tcell.KeyBacktab:
			if prev != nil {
				app.SetFocus(prev)
				focused(prev)
				return nil
			}
		}
		return event
	}
}

func primitiveToComponent(p tview.Primitive) podsComponent {
	switch p.(type) {
	case *tview.DropDown:
		return podsNamespace
	case *tview.TreeView:
		return podsTree
	case *tview.TextView:
		return podsDetails
	default:
		return podsButtons
	}
}
