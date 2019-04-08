package ui

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"golang.org/x/xerrors"
	yaml "gopkg.in/yaml.v2"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/duration"
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
			SetText(fmt.Sprintf("Error: %s", err)).
			//ClearButtons().
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
		object          interface{}
		details         detailsView
	}
}

func NewPodsPresenter(ui *UI, client k8s.Client) *PodsPresenter {
	return &PodsPresenter{
		ErrorPresenter: &ErrorPresenter{ui: ui},
		client:         client,
	}
}

func (p *PodsPresenter) populateNamespaces() error {
	log.Println("Getting cluster namespaces")
	p.ui.statusBar.SpinText("Loading namespaces", p.ui.app)
	defer p.ui.statusBar.StopSpin()
	p.ui.app.QueueUpdate(func() {
		p.ui.pages.SwitchToPage(pagePods)
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

			p.cycleFocusCapture(
				p.ui.namespaceDropDown,
				stateMultiPrimitiveToFocus(p),
				singlePrimitiveToFocus(p.ui.podsTree))
			p.cycleFocusCapture(p.ui.podsTree,
				singlePrimitiveToFocus(p.ui.namespaceDropDown),
				stateMultiPrimitiveToFocus(p))
			p.cycleFocusCapture(p.ui.podData,
				singlePrimitiveToFocus(p.ui.podsTree),
				singlePrimitiveToFocus(p.ui.namespaceDropDown))
			p.cycleFocusCapture(p.ui.podEvents,
				singlePrimitiveToFocus(p.ui.podsTree),
				singlePrimitiveToFocus(p.ui.namespaceDropDown))
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

func (p *PodsPresenter) cycleFocusCapture(on InputCapturer, prev, next primitiveToFocus) {
	on.SetInputCapture(cycleFocusCapture(p.ui.app, prev, next, p.onFocused))
}

func (p *PodsPresenter) populatePods(ns string, clear bool) error {
	p.ui.statusBar.SpinText("Loading pods", p.ui.app)
	defer p.ui.statusBar.StopSpin()
	p.ui.app.QueueUpdateDraw(func() {
		p.ui.podsTree.SetRoot(tview.NewTreeNode(""))
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
					SetReference(deployment.Deployment).SetSelectable(true)
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

		p.state.object = ref
		switch p.state.details {
		case detailsObject:
			go p.showDetails(ref)
		case detailsEvents:
			go func() {
				p.displayError(p.showEvents(p.state.object))
			}()
		}
	})

	return nil
}

func (p *PodsPresenter) showDetails(object interface{}) {
	p.state.details = detailsObject
	p.ui.app.QueueUpdateDraw(func() {
		p.setDetailsView()
		if data, err := yaml.Marshal(object); err == nil {
			p.ui.podData.SetText(string(data))
		} else {
			p.ui.podData.SetText(err.Error())
		}
		p.ui.app.SetFocus(p.ui.podData)
	})
}

func (p *PodsPresenter) showEvents(object interface{}) error {
	meta, err := objectMeta(object)
	if err != nil {
		log.Printf("Error getting meta information from object %T: %v", object, err)
		return err
	}

	log.Printf("Getting events for object %s", meta.GetName())
	p.ui.statusBar.SpinText("Loading events", p.ui.app)
	defer p.ui.statusBar.StopSpin()

	list, err := p.client.Events(meta)
	if err != nil {
		log.Printf("Error getting events for object %s: %s", meta.GetName(), err)
		return UserRetryableError{err, func() error {
			return p.showEvents(object)
		}}
	}

	p.state.details = detailsEvents
	p.ui.app.QueueUpdateDraw(func() {
		p.setDetailsView()
		p.ui.podEvents.Clear()
		headers := []string{}
		if len(list) == 0 {
			headers = append(headers, "No events")
		} else {
			headers = append(headers, "Type", "Reason", "Age", "From", "Message")
		}

		for i, h := range headers {
			p.ui.podEvents.SetCell(0, i, tview.NewTableCell(h).
				SetAlign(tview.AlignCenter).
				SetTextColor(tcell.ColorYellow))

		}

		if len(list) == 0 {
			return
		}

		for i, event := range list {
			for j := range headers {
				switch j {
				case 0:
					p.ui.podEvents.SetCell(i+1, j,
						tview.NewTableCell(event.Type))
				case 1:
					p.ui.podEvents.SetCell(i+1, j,
						tview.NewTableCell(event.Reason))
				case 2:
					first := duration.HumanDuration(time.Since(event.FirstTimestamp.Time))
					interval := first
					if event.Count > 1 {
						last := duration.HumanDuration(time.Since(event.LastTimestamp.Time))
						interval = fmt.Sprintf("%s (x%d since %s)", last, event.Count, first)
					}
					p.ui.podEvents.SetCell(i+1, j,
						tview.NewTableCell(interval))
				case 3:
					from := event.Source.Component
					if len(event.Source.Host) > 0 {
						from += ", " + event.Source.Host
					}
					p.ui.podEvents.SetCell(i+1, j,
						tview.NewTableCell(from))
				case 4:
					p.ui.podEvents.SetCell(i+1, j,
						tview.NewTableCell(strings.TrimSpace(event.Message)))
				}
			}
			p.ui.app.SetFocus(p.ui.podData)
		}
	})

	return nil
}

func (p *PodsPresenter) setDetailsView() {
	p.ui.podsDetails.RemoveItem(p.ui.podData)
	p.ui.podsDetails.RemoveItem(p.ui.podEvents)
	switch p.state.details {
	case detailsObject:
		p.ui.podsDetails.AddItem(p.ui.podData, 0, 1, false)
	case detailsEvents:
		p.ui.podsDetails.AddItem(p.ui.podEvents, 0, 1, false)
	}
}

func (p *PodsPresenter) initKeybindings() {
	p.ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF1:
			if p.state.activeComponent == podsDetails && p.state.object != nil {
				go p.showDetails(p.state.object)
				return nil
			}
		case tcell.KeyF2:
			if p.state.activeComponent == podsDetails && p.state.object != nil {
				go func() {
					p.displayError(p.showEvents(p.state.object))
				}()
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
	if p.state.object != nil {
		p.ui.actionBar.AddAction(1, "Details")
		p.ui.actionBar.AddAction(2, "Events")
	}
	p.ui.actionBar.AddAction(10, "Quit")
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

type primitiveToFocus func() tview.Primitive

func singlePrimitiveToFocus(p tview.Primitive) primitiveToFocus {
	return func() tview.Primitive {
		return p
	}
}

func stateMultiPrimitiveToFocus(p *PodsPresenter) primitiveToFocus {
	return func() tview.Primitive {
		switch p.state.details {
		case detailsObject:
			return p.ui.podData
		case detailsEvents:
			return p.ui.podEvents
		}
		return nil
	}
}

func cycleFocusCapture(app *tview.Application, prev, next primitiveToFocus, focused func(p tview.Primitive)) func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			n := next()
			if n != nil {
				app.SetFocus(n)
				focused(n)
				return nil
			}
		case tcell.KeyBacktab:
			p := prev()
			if p != nil {
				app.SetFocus(p)
				focused(p)
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
	case *tview.TextView, *tview.Table:
		return podsDetails
	default:
		return podsButtons
	}
}

type ObjectMetaGetter interface {
	GetObjectMeta() meta.ObjectMeta
}

var errNotObjMeta = errors.New("object does not have meta data")

func objectMeta(object interface{}) (meta.ObjectMeta, error) {
	var m meta.ObjectMeta
	if g, ok := object.(ObjectMetaGetter); ok {
		return g.GetObjectMeta(), nil
	}

	typ := reflect.TypeOf(object)
	if typ.Kind() != reflect.Struct {
		return m, errNotObjMeta
	}
	val := reflect.ValueOf(object)

	for i := 0; i < typ.NumField(); i++ {
		f := val.Field(i)
		if f.Kind() != reflect.Struct {
			continue
		}

		if m, ok := f.Interface().(meta.ObjectMeta); ok {
			return m, nil
		}
	}

	return m, errNotObjMeta
}

func HumanDuration(d time.Duration) string {
	// Allow deviation no more than 2 seconds(excluded) to tolerate machine time
	// inconsistence, it can be considered as almost now.
	if seconds := int(d.Seconds()); seconds < -1 {
		return fmt.Sprintf("<invalid>")
	} else if seconds < 0 {
		return fmt.Sprintf("0s")
	} else if seconds < 60*2 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := int(d / time.Minute)
	if minutes < 10 {
		s := int(d/time.Second) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, s)
	} else if minutes < 60*3 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := int(d / time.Hour)
	if hours < 8 {
		m := int(d/time.Minute) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, m)
	} else if hours < 48 {
		return fmt.Sprintf("%dh", hours)
	} else if hours < 24*8 {
		h := hours % 24
		if h == 0 {
			return fmt.Sprintf("%dd", hours/24)
		}
		return fmt.Sprintf("%dd%dh", hours/24, h)
	} else if hours < 24*365*2 {
		return fmt.Sprintf("%dd", hours/24)
	} else if hours < 24*365*8 {
		return fmt.Sprintf("%dy%dd", hours/24/365, (hours/24)%365)
	}
	return fmt.Sprintf("%dy", int(hours/24/365))
}
