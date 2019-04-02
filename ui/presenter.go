package ui

import (
	"fmt"
	"log"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"golang.org/x/xerrors"
	yaml "gopkg.in/yaml.v2"
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
}

const (
	buttonQuit  = "Quit"
	buttonClose = "Close"
	buttonRetry = "Retry"
)

func (p ErrorPresenter) displayError(err error) bool {
	if err == nil {
		p.ui.app.QueueUpdateDraw(func() {
			p.ui.pages.HidePage(pageK8sError)
		})
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
				case buttonClose:
					p.ui.pages.HidePage(pageK8sError)
				case buttonRetry:
					p.ui.pages.HidePage(pageK8sError)
					go func() {
						p.displayError(err.(UserRetryableError).RetryOp())
					}()
				}
			})
		p.ui.pages.ShowPage(pageK8sError)
		p.ui.app.SetFocus(p.ui.errorModal)
	})

	return true
}

type MainPresenter struct {
	ErrorPresenter

	clientFactory ClientFactory

	client        k8s.Client
	podsPresenter *PodsPresenter
}

func NewMainPresenter(ui *UI, clientFactory ClientFactory) *MainPresenter {
	return &MainPresenter{
		ErrorPresenter: ErrorPresenter{ui: ui},
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

type PodsPresenter struct {
	ErrorPresenter

	client k8s.Client
}

func NewPodsPresenter(ui *UI, client k8s.Client) *PodsPresenter {
	return &PodsPresenter{
		ErrorPresenter: ErrorPresenter{ui},
		client:         client,
	}
}

func (p *PodsPresenter) initKeybindings() {
	p.ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF10:
			p.ui.app.Stop()
			return nil
		}
		return event
	})
}

func (p *PodsPresenter) populateNamespaces() error {
	log.Println("Getting cluster namespaces")
	done := make(chan struct{})
	defer close(done)
	p.ui.app.QueueUpdate(func() {
		p.ui.pages.SwitchToPage(pagePods)
		spinText(p.ui.app, p.ui.statusBar, "Loading namespaces", done)
	})

	if namespaces, err := p.client.Namespaces(); err == nil {
		p.ui.app.QueueUpdateDraw(func() {
			p.ui.namespaceDropDown.SetOptions(namespaces, func(text string, idx int) {
				p.ui.podsTree.SetRoot(tview.NewTreeNode(""))
				done := make(chan struct{})
				spinText(p.ui.app, p.ui.statusBar, "Loading pods", done)
				go func() {
					p.displayError(p.populatePods(text))
					close(done)
				}()
			})
			p.ui.namespaceDropDown.SetCurrentOption(0)
			p.ui.app.SetFocus(p.ui.namespaceDropDown)
			p.ui.namespaceDropDown.SetInputCapture(cycleFocusCapture(p.ui.app, p.ui.podsDetails, p.ui.podsTree))
			p.ui.podsTree.SetInputCapture(cycleFocusCapture(p.ui.app, p.ui.namespaceDropDown, p.ui.podsDetails))
			p.ui.podsDetails.SetInputCapture(cycleFocusCapture(p.ui.app, p.ui.podsTree, p.ui.buttonBar))
			p.ui.buttonBar.GetButton(0).SetInputCapture(cycleFocusCapture(p.ui.app, p.ui.podsDetails, nil))
			p.ui.buttonBar.GetButton(p.ui.buttonBar.GetButtonCount() - 1).SetInputCapture(cycleFocusCapture(p.ui.app, nil, p.ui.namespaceDropDown))
		})

		return nil
	} else {
		log.Println("Error getting cluster namespaces:", err)
		return UserRetryableError{err, p.populateNamespaces}
	}
}

func (p *PodsPresenter) populatePods(ns string) error {
	log.Printf("Getting pod tree for namespace %s", ns)
	podTree, err := p.client.PodTree(ns)
	if err != nil {
		log.Printf("Error getting pod tree for namespaces %s: %s", ns, err)
		return UserRetryableError{err, func() error {
			return p.populatePods(ns)
		}}
	}

	p.ui.app.QueueUpdateDraw(func() {
		log.Printf("Updating tree view with pods for namespaces %s", ns)
		root := tview.NewTreeNode(".")
		p.ui.podsTree.SetRoot(root).SetCurrentNode(root)

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
	})

	p.ui.podsTree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref == nil {
			node.SetExpanded(!node.IsExpanded())
			return
		}

		if data, err := yaml.Marshal(ref); err == nil {
			p.ui.podsDetails.SetText(string(data))
		} else {
			p.ui.podsDetails.SetText(err.Error())
		}
	})

	return nil
}

func cycleFocusCapture(app *tview.Application, prev, next tview.Primitive) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			if next != nil {
				app.SetFocus(next)
				return nil
			}
		case tcell.KeyBacktab:
			if prev != nil {
				app.SetFocus(prev)
				return nil
			}
		}
		return event
	}
}

var spinners = [...]string{"⠋ ", "⠙ ", "⠹ ", "⠸ ", "⠼ ", "⠴ ", "⠦ ", "⠧ ", "⠇ ", "⠏ "}

func spinText(app *tview.Application, textView *tview.TextView, text string, done <-chan struct{}) {
	app.QueueUpdateDraw(func() {
		textView.SetText(spinners[0] + text)
	})
	go func() {
		i := 1
		for {
			select {
			case <-done:
				app.QueueUpdateDraw(func() {
					textView.SetText("")
				})
				return
			case <-time.After(100 * time.Millisecond):
				spin := i % len(spinners)
				app.QueueUpdateDraw(func() {
					textView.SetText(spinners[spin] + text)
				})
				i++
			}
		}
	}()
}
