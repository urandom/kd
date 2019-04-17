package presenter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	kerrs "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/duration"
	"sigs.k8s.io/yaml"
)

type FatalError struct {
	error
}

type UserRetryableError struct {
	error
	RetryOp func() error
}

type ClientFactory func() (k8s.Client, error)

type Error struct {
	ui *ui.UI

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

func (p *Error) displayError(err error) bool {
	if err == nil {
		if p.isModalVisible {
			p.ui.App.QueueUpdateDraw(func() {
				p.ui.Pages.HidePage(ui.PageK8sError)
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

	p.ui.App.QueueUpdateDraw(func() {
		p.ui.ErrorModal.
			SetText(fmt.Sprintf("Error: %s", err)).
			//ClearButtons().
			AddButtons(buttons).
			SetDoneFunc(func(idx int, label string) {
				switch label {
				case buttonQuit:
					p.ui.App.Stop()
				case buttonRetry:
					go func() {
						p.displayError(err.(UserRetryableError).RetryOp())
					}()
					fallthrough
				case buttonClose:
					p.isModalVisible = false
					p.ui.Pages.HidePage(ui.PageK8sError)
					p.ui.App.SetFocus(p.focused)
				}
			})
		p.isModalVisible = true
		p.focused = p.ui.App.GetFocus()
		p.ui.Pages.ShowPage(ui.PageK8sError)
		p.ui.App.SetFocus(p.ui.ErrorModal)
	})

	return true
}

type Picker struct {
	ui *ui.UI

	isModalVisible bool
	focused        tview.Primitive
}

func (p *Picker) PickFrom(title string, items []string) <-chan string {
	choice := make(chan string)

	p.ui.App.QueueUpdateDraw(func() {
		list := p.ui.Picker.List().Clear().SetSelectedFunc(
			func(idx int, main, sec string, sk rune) {
				choice <- main
				close(choice)
				p.ui.Pages.HidePage(ui.PagePicker)
				p.ui.App.SetFocus(p.focused)
			})
		for i := range items {
			list.AddItem(items[i], "", rune(strconv.Itoa(i)[0]), nil)
		}

		list.SetTitle(title)

		p.isModalVisible = true
		p.focused = p.ui.App.GetFocus()
		p.ui.Pages.ShowPage(ui.PagePicker)
		p.ui.App.SetFocus(p.ui.Picker)
	})

	return choice
}

type Main struct {
	*Error

	clientFactory ClientFactory

	client k8s.Client
	Pods   *Pods
}

func NewMain(ui *ui.UI, clientFactory ClientFactory) *Main {
	return &Main{
		Error:         &Error{ui: ui},
		clientFactory: clientFactory,
	}
}

func (p *Main) Run() error {
	go func() {
		if !p.displayError(p.initClient()) {
			p.Pods = NewPods(p.ui, p.client)
			p.Pods.initKeybindings()
			p.displayError(p.Pods.populateNamespaces())
		}
	}()

	return p.ui.App.Run()
}

func (p *Main) initClient() error {
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
	picker *Picker

	client k8s.Client
	state  struct {
		activeComponent podsComponent
		namespace       string
		object          interface{}
		details         detailsView
		logContainer    string
		fullscreen      bool
	}
	cancelWatchFn context.CancelFunc
}

func NewPods(ui *ui.UI, client k8s.Client) *Pods {
	return &Pods{
		Error:  &Error{ui: ui},
		picker: &Picker{ui: ui},
		client: client,
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
					p.displayError(p.populatePods(text, true))
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

type InputCapturer interface {
	SetInputCapture(capture func(event *tcell.EventKey) *tcell.EventKey) *tview.Box
}

func (p *Pods) populatePods(ns string, clear bool) error {
	p.ui.StatusBar.SpinText("Loading pods", p.ui.App)
	defer p.ui.StatusBar.StopSpin()
	p.ui.App.QueueUpdateDraw(func() {
		p.ui.PodsTree.SetRoot(tview.NewTreeNode(""))
	})

	log.Printf("Getting pod tree for namespace %s", ns)
	podTree, err := p.client.PodTree(ns)
	if err != nil {
		log.Printf("Error getting pod tree for namespaces %s: %s", ns, err)
		return UserRetryableError{err, func() error {
			return p.populatePods(ns, clear)
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

	p.state.namespace = ns
	p.ui.App.QueueUpdateDraw(func() {
		log.Printf("Updating tree view with pods for namespaces %s", ns)
		root := tview.NewTreeNode(".")
		p.ui.PodsTree.SetRoot(root)

		for i, c := range controllers {
			if len(c) == 0 {
				continue
			}
			dn := tview.NewTreeNode(controllerNames[i]).
				SetSelectable(true).
				SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, controller := range c {
				d := tview.NewTreeNode(controller.Controller().GetObjectMeta().GetName()).
					SetReference(controller).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range controller.Pods() {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(root.GetChildren()) > 0 {
			p.ui.PodsTree.SetCurrentNode(root.GetChildren()[0])
		}
	})

	p.ui.PodsTree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref == nil {
			node.SetExpanded(!node.IsExpanded())
			return
		}

		p.state.object = ref
		p.onFocused(p.ui.PodsTree)
		switch p.state.details {
		case detailsObject:
			go p.showDetails(ref)
		case detailsEvents:
			go func() {
				p.displayError(p.showEvents(p.state.object))
			}()
		case detailsLog:
			go func() {
				p.displayError(p.showLog(p.state.object, p.state.logContainer))
			}()
		}
	})

	return nil
}

func (p *Pods) showDetails(object interface{}) {
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

	if v, ok := object.(k8s.Controller); ok {
		object = v.Controller()
	}

	p.state.details = detailsObject
	p.onFocused(p.ui.PodData)
	p.ui.App.QueueUpdateDraw(func() {
		p.setDetailsView()
		p.ui.PodData.SetText("").SetRegions(true).SetDynamicColors(true)
		if data, err := yaml.Marshal(object); err == nil {
			fmt.Fprint(p.ui.PodData, "[greenyellow::b]Summary\n=======\n\n")
			p.printObjectSummary(p.ui.PodData, object)
			fmt.Fprint(p.ui.PodData, "[greenyellow::b]Object\n======\n\n")
			fmt.Fprint(p.ui.PodData, string(data))
		} else {
			p.ui.PodData.SetText(err.Error())
		}
	})
}

func (p *Pods) printObjectSummary(w io.Writer, object interface{}) {
	switch v := object.(type) {
	case *cv1.Pod:
		total := len(v.Status.ContainerStatuses)
		ready := 0
		var restarts int32
		var lastRestart time.Time
		for _, cs := range v.Status.ContainerStatuses {
			if cs.Ready {
				ready += 1
			}
			restarts += cs.RestartCount
			if cs.LastTerminationState.Terminated != nil {
				if cs.LastTerminationState.Terminated.FinishedAt.Time.After(lastRestart) {
					lastRestart = cs.LastTerminationState.Terminated.FinishedAt.Time
				}
			}
		}
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d/%d\n", ready, total)
		fmt.Fprintf(w, "[skyblue::b]Status:[white::-] %s\n", v.Status.Phase)
		fmt.Fprintf(w, "[skyblue::b]Restarts:[white::-] %d\n", restarts)
		if !lastRestart.IsZero() {
			fmt.Fprintf(w, "[skyblue::b]\tLast restart:[white::-] %s\n", duration.HumanDuration(time.Since(lastRestart)))
		}
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.Status.StartTime.Time)))
	case *av1.StatefulSet:
		replicas := v.Status.Replicas
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d/%d\n", v.Status.ReadyReplicas, replicas)
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case *av1.Deployment:
		replicas := v.Status.Replicas
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d/%d\n", v.Status.ReadyReplicas, replicas)
		fmt.Fprintf(w, "[skyblue::b]Up-to-date:[white::-] %d/%d\n", v.Status.UpdatedReplicas, replicas)
		fmt.Fprintf(w, "[skyblue::b]Available:[white::-] %d\n", v.Status.AvailableReplicas)
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case *av1.DaemonSet:
		fmt.Fprintf(w, "[skyblue::b]Desired:[white::-] %d\n", v.Status.DesiredNumberScheduled)
		fmt.Fprintf(w, "[skyblue::b]Current:[white::-] %d\n", v.Status.CurrentNumberScheduled)
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d\n", v.Status.NumberReady)
		fmt.Fprintf(w, "[skyblue::b]Up-to-date:[white::-] %d\n", v.Status.UpdatedNumberScheduled)
		fmt.Fprintf(w, "[skyblue::b]Available:[white::-] %d\n", v.Status.NumberAvailable)
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case *bv1.Job:
		fmt.Fprintf(w, "[skyblue::b]Active:[white::-] %d\n", v.Status.Active)
		fmt.Fprintf(w, "[skyblue::b]Succeeded:[white::-] %d\n", v.Status.Succeeded)
		fmt.Fprintf(w, "[skyblue::b]Failed:[white::-] %d\n", v.Status.Failed)
		if v.Status.CompletionTime != nil {
			fmt.Fprintf(w, "[skyblue::b]Duration:[white::-] %s\n", duration.HumanDuration(v.Status.CompletionTime.Sub(v.Status.StartTime.Time)))
		}
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case *bv1b1.CronJob:
		fmt.Fprintf(w, "[skyblue::b]Schedule:[white::-] %s\n", v.Spec.Schedule)
		if v.Spec.Suspend != nil {
			fmt.Fprintf(w, "[skyblue::b]Suspend:[white::-] %v\n", *v.Spec.Suspend)
		}
		fmt.Fprintf(w, "[skyblue::b]Active:[white::-] %d\n", len(v.Status.Active))
		fmt.Fprintf(w, "[skyblue::b]Last scheduled:[white::-] %s\n", duration.HumanDuration(time.Since(v.Status.LastScheduleTime.Time)))
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case *cv1.Service:
		fmt.Fprintf(w, "[skyblue::b]Type:[white::-] %s\n", v.Spec.Type)
		fmt.Fprintf(w, "[skyblue::b]Cluster IP:[white::-] %s\n", v.Spec.ClusterIP)
		if len(v.Spec.ExternalIPs) > 0 {
			fmt.Fprintf(w, "[skyblue::b]External IPs:[white::-] %s\n", strings.Join(v.Spec.ExternalIPs, ", "))
		}
		if v.Spec.Type == cv1.ServiceTypeExternalName {
			fmt.Fprintf(w, "[skyblue::b]External Name:[white::-] %s\n", v.Spec.ExternalName)
		}
		fmt.Fprintf(w, "[skyblue::b]Ports:[white::-] %s\n", portsToString(v.Spec.Ports))
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	default:
		fmt.Fprintf(w, "[skyblue::b]Type:[::] %T\n", v)
	}
	fmt.Fprintln(w, "")
}

func (p *Pods) showEvents(object interface{}) error {
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

	meta, err := objectMeta(object)
	if err != nil {
		log.Printf("Error getting meta information from object %T: %v", object, err)
		return err
	}

	log.Printf("Getting events for object %s", meta.GetName())
	p.ui.StatusBar.SpinText("Loading events", p.ui.App)
	defer p.ui.StatusBar.StopSpin()

	list, err := p.client.Events(meta)
	if err != nil {
		log.Printf("Error getting events for object %s: %s", meta.GetName(), err)
		return UserRetryableError{err, func() error {
			return p.showEvents(object)
		}}
	}

	p.state.details = detailsEvents
	p.onFocused(p.ui.PodEvents)
	p.ui.App.QueueUpdateDraw(func() {
		p.setDetailsView()
		p.ui.PodEvents.Clear()
		headers := []string{}
		if len(list) == 0 {
			headers = append(headers, "No events")
		} else {
			headers = append(headers, "Type", "Reason", "Age", "From", "Message")
		}

		for i, h := range headers {
			p.ui.PodEvents.SetCell(0, i, tview.NewTableCell(h).
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
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(event.Type))
				case 1:
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(event.Reason))
				case 2:
					first := duration.HumanDuration(time.Since(event.FirstTimestamp.Time))
					interval := first
					if event.Count > 1 {
						last := duration.HumanDuration(time.Since(event.LastTimestamp.Time))
						interval = fmt.Sprintf("%s (x%d since %s)", last, event.Count, first)
					}
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(interval))
				case 3:
					from := event.Source.Component
					if len(event.Source.Host) > 0 {
						from += ", " + event.Source.Host
					}
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(from))
				case 4:
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(strings.TrimSpace(event.Message)))
				}
			}
		}
	})

	return nil
}

func (p *Pods) showLog(object interface{}, container string) error {
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

	log.Println("Getting logs")
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelWatchFn = cancel

	p.ui.StatusBar.SpinText("Loading logs", p.ui.App)
	p.state.details = detailsLog
	p.onFocused(p.ui.PodData)
	p.state.logContainer = container
	p.ui.App.QueueUpdateDraw(func() {
		p.setDetailsView()
		p.ui.PodData.Clear().SetRegions(false).SetDynamicColors(true)
	})
	data, err := p.client.Logs(ctx, object, false, container, []string{"yellow", "aqua", "chartreuse"})
	if err != nil {
		if xerrors.As(err, &k8s.ErrMultipleContainers{}) {
			names := err.(k8s.ErrMultipleContainers).Containers
			go func() {
				choice := <-p.picker.PickFrom("Containers", names)
				p.displayError(p.showLog(p.state.object, choice))
			}()
			return nil
		} else {
			return err
		}
	}

	if err != nil {
		return err
	}

	go func() {
		initial := true
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-data:
				if initial {
					p.ui.StatusBar.StopSpin()
					initial = false
				}
				p.ui.App.QueueUpdateDraw(func() {
					fmt.Fprint(p.ui.PodData, string(b))
				})
			}
		}
	}()

	return nil
}

func (p *Pods) setDetailsView() {
	p.ui.PodsDetails.RemoveItem(p.ui.PodData)
	p.ui.PodsDetails.RemoveItem(p.ui.PodEvents)
	switch p.state.details {
	case detailsObject:
		p.ui.PodData.SetTitle("Details")
		p.ui.PodsDetails.AddItem(p.ui.PodData, 0, 1, false)
	case detailsEvents:
		p.ui.PodsDetails.AddItem(p.ui.PodEvents, 0, 1, false)
	case detailsLog:
		p.ui.PodData.SetTitle("Logs")
		p.ui.PodsDetails.AddItem(p.ui.PodData, 0, 1, false)
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
		case tcell.KeyF1:
			p.ui.App.SetFocus(p.ui.PodData)
			p.onFocused(p.ui.PodData)
			if p.state.object != nil {
				go p.showDetails(p.state.object)
				return nil
			}
		case tcell.KeyCtrlN:
			p.ui.App.SetFocus(p.ui.NamespaceDropDown)
			p.ui.App.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, rune(tcell.KeyEnter), tcell.ModNone))
		case tcell.KeyF2:
			if p.state.object != nil {
				p.ui.App.SetFocus(p.ui.PodEvents)
				p.onFocused(p.ui.PodEvents)
				go func() {
					p.displayError(p.showEvents(p.state.object))
				}()
				return nil
			}
		case tcell.KeyF3:
			if p.state.object != nil {
				p.ui.App.SetFocus(p.ui.PodData)
				p.onFocused(p.ui.PodData)
				go func() {
					p.displayError(p.showLog(p.state.object, ""))
				}()
				return nil
			}
		case tcell.KeyF4:
			if p.state.object != nil {
				switch p.state.details {
				case detailsObject:
					go func() {
						p.displayError(p.editObject(p.state.object))
					}()
				case detailsEvents:
					return event
				case detailsLog:
					go func() {
						p.displayError(p.viewLog())
					}()
				}
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
			p.displayError(p.populatePods(p.state.namespace, false))
		}()
	case podsDetails:
		go func() {
			switch p.state.details {
			case detailsEvents:
				p.displayError(p.showEvents(p.state.object))
			}
		}()
	}
}

func (p *Pods) editObject(object interface{}) (err error) {
	objData, err := yaml.Marshal(object)
	if err != nil {
		return err
	}

	preemble := []byte(`# Please edit the object below. Lines beginning with a '#' will be ignored,
# and an empty file will abort the edit. If an error occurs while saving this file will be
# reopened with the relevant failures.
#
`)
	p.ui.App.Suspend(func() {
		var msg string
		for {
			data := append([]byte(nil), preemble...)
			if msg != "" {
				data = append(data, '#', ' ')
				data = append(data, []byte(msg)...)
			}
			data = append(data, objData...)

			var updated []byte
			if updated, err = externalEditor(data, true); err != nil {
				return
			}

			if bytes.Equal(objData, updated) {
				// No updates
				return
			}

			var json []byte
			if json, err = yaml.YAMLToJSON(updated); err != nil {
				return
			}

			if err = p.client.UpdateObject(object, json); err != nil {
				if w := xerrors.Unwrap(err); w != nil {
					if statusError, ok := w.(*kerrs.StatusError); ok {
						msg = strings.SplitN(statusError.ErrStatus.Message, "\n", 2)[0] + "\n"
						continue
					}
				}
			}

			return
		}
	})

	return err
}

func (p *Pods) viewLog() (err error) {
	log := p.ui.PodData.GetText(true)
	p.ui.App.Suspend(func() {
		_, err = externalEditor([]byte(log), false)
	})

	return err
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

func externalEditor(text []byte, readBack bool) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	f, err := ioutil.TempFile("", "*.log")
	if err != nil {
		return nil, xerrors.Errorf("creating temporary file: %w", err)
	}
	defer os.Remove(f.Name())
	_, err = f.Write(text)
	if err != nil {
		return nil, xerrors.Errorf("writing data to temporary file: %w", err)
	}
	f.Sync()

	// Clear the screnn
	fmt.Print("\033[H\033[2J")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt, os.Kill)
	defer signal.Stop(sig)
	go func() {
		<-sig
		cancel()
	}()

	cmd := exec.CommandContext(ctx, editor, f.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err = cmd.Run(); err != nil {
		return nil, xerrors.Errorf("viewing data through %s: %w", editor, err)
	}

	if readBack {
		f.Seek(0, 0)
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, xerrors.Errorf("reading back data from temporary file: %w", editor, err)
		}

		return b, nil
	}

	return nil, nil
}
