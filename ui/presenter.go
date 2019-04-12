package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"golang.org/x/xerrors"
	yaml "gopkg.in/yaml.v2"
	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
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

type PickerPresenter struct {
	ui *UI

	isModalVisible bool
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
	podsTree podsComponent = iota
	podsDetails
	podsButtons
)

type detailsView int

const (
	detailsObject detailsView = iota
	detailsEvents
	detailsLog
)

type PodsPresenter struct {
	*ErrorPresenter
	picker *PickerPresenter

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

func NewPodsPresenter(ui *UI, client k8s.Client) *PodsPresenter {
	return &PodsPresenter{
		ErrorPresenter: &ErrorPresenter{ui: ui},
		picker:         &PickerPresenter{ui: ui},
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
				p.ui.app.SetFocus(p.ui.podsTree)
				p.onFocused(p.ui.podsTree)
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

		if len(podTree.StatefulSets) > 0 {
			dn := tview.NewTreeNode("Stateful Sets").SetSelectable(true).SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, statefulset := range podTree.StatefulSets {
				d := tview.NewTreeNode(statefulset.GetObjectMeta().GetName()).
					SetReference(statefulset.StatefulSet).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range statefulset.Pods() {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(podTree.Deployments) > 0 {
			dn := tview.NewTreeNode("Deployments").SetSelectable(true).SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, deployment := range podTree.Deployments {
				d := tview.NewTreeNode(deployment.GetObjectMeta().GetName()).
					SetReference(deployment.Deployment).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range deployment.Pods() {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(podTree.DaemonSets) > 0 {
			dn := tview.NewTreeNode("Daemon Sets").SetSelectable(true).SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, daemonset := range podTree.DaemonSets {
				d := tview.NewTreeNode(daemonset.GetObjectMeta().GetName()).
					SetReference(daemonset.DaemonSet).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range daemonset.Pods() {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(podTree.Jobs) > 0 {
			dn := tview.NewTreeNode("Jobs").SetSelectable(true).SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, job := range podTree.Jobs {
				d := tview.NewTreeNode(job.GetObjectMeta().GetName()).
					SetReference(job.Job).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range job.Pods() {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(podTree.CronJobs) > 0 {
			dn := tview.NewTreeNode("Cron Jobs").SetSelectable(true).SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, cronjob := range podTree.CronJobs {
				d := tview.NewTreeNode(cronjob.GetObjectMeta().GetName()).
					SetReference(cronjob.CronJob).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range cronjob.Pods() {
					p := tview.NewTreeNode(pod.GetObjectMeta().GetName()).
						SetReference(pod).SetSelectable(true)
					d.AddChild(p)
				}
			}
		}

		if len(podTree.Services) > 0 {
			dn := tview.NewTreeNode("Services").SetSelectable(true).SetColor(tcell.ColorCoral)
			root.AddChild(dn)

			for _, service := range podTree.Services {
				d := tview.NewTreeNode(service.GetObjectMeta().GetName()).
					SetReference(service.Service).SetSelectable(true)
				dn.AddChild(d)

				for _, pod := range service.Pods() {
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
		p.onFocused(p.ui.podsTree)
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
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

	p.state.details = detailsObject
	p.ui.app.QueueUpdateDraw(func() {
		p.setDetailsView()
		p.ui.podData.SetText("")
		if data, err := yaml.Marshal(object); err == nil {
			fmt.Fprint(p.ui.podData, "[greenyellow::b]Summary\n=======\n\n")
			p.printObjectSummary(p.ui.podData, object)
			fmt.Fprint(p.ui.podData, "[greenyellow::b]Object\n======\n\n")
			fmt.Fprint(p.ui.podData, string(data))
		} else {
			p.ui.podData.SetText(err.Error())
		}
	})
}

func (p *PodsPresenter) printObjectSummary(w io.Writer, object interface{}) {
	switch v := object.(type) {
	case cv1.Pod:
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
	case av1.StatefulSet:
		replicas := v.Status.Replicas
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d/%d\n", v.Status.ReadyReplicas, replicas)
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case av1.Deployment:
		replicas := v.Status.Replicas
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d/%d\n", v.Status.ReadyReplicas, replicas)
		fmt.Fprintf(w, "[skyblue::b]Up-to-date:[white::-] %d/%d\n", v.Status.UpdatedReplicas, replicas)
		fmt.Fprintf(w, "[skyblue::b]Available:[white::-] %d\n", v.Status.AvailableReplicas)
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case av1.DaemonSet:
		fmt.Fprintf(w, "[skyblue::b]Desired:[white::-] %d\n", v.Status.DesiredNumberScheduled)
		fmt.Fprintf(w, "[skyblue::b]Current:[white::-] %d\n", v.Status.CurrentNumberScheduled)
		fmt.Fprintf(w, "[skyblue::b]Ready:[white::-] %d\n", v.Status.NumberReady)
		fmt.Fprintf(w, "[skyblue::b]Up-to-date:[white::-] %d\n", v.Status.UpdatedNumberScheduled)
		fmt.Fprintf(w, "[skyblue::b]Available:[white::-] %d\n", v.Status.NumberAvailable)
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case bv1.Job:
		fmt.Fprintf(w, "[skyblue::b]Active:[white::-] %d\n", v.Status.Active)
		fmt.Fprintf(w, "[skyblue::b]Succeeded:[white::-] %d\n", v.Status.Succeeded)
		fmt.Fprintf(w, "[skyblue::b]Failed:[white::-] %d\n", v.Status.Failed)
		fmt.Fprintf(w, "[skyblue::b]Duration:[white::-] %s\n", duration.HumanDuration(v.Status.CompletionTime.Sub(v.Status.StartTime.Time)))
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case bv1b1.CronJob:
		fmt.Fprintf(w, "[skyblue::b]Schedule:[white::-] %s\n", v.Spec.Schedule)
		if v.Spec.Suspend != nil {
			fmt.Fprintf(w, "[skyblue::b]Suspend:[white::-] %v\n", *v.Spec.Suspend)
		}
		fmt.Fprintf(w, "[skyblue::b]Active:[white::-] %d\n", len(v.Status.Active))
		fmt.Fprintf(w, "[skyblue::b]Last scheduled:[white::-] %s\n", duration.HumanDuration(time.Since(v.Status.LastScheduleTime.Time)))
		fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.ObjectMeta.CreationTimestamp.Time)))
	case cv1.Service:
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

func (p *PodsPresenter) showEvents(object interface{}) error {
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

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
		}
	})

	return nil
}

func (p *PodsPresenter) showLog(object interface{}) error {
	if p.cancelWatchFn != nil {
		p.cancelWatchFn()
	}

	log.Println("Getting logs")
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelWatchFn = cancel

	p.ui.statusBar.SpinText("Loading logs", p.ui.app)
	p.state.details = detailsLog
	p.ui.app.QueueUpdateDraw(func() {
		p.setDetailsView()
		p.ui.podData.Clear()
	})
	data, err := p.client.Logs(ctx, object, false, "")
	if err != nil {
		if xerrors.As(err, &k8s.ErrMultipleContainers{}) {
			names := err.(k8s.ErrMultipleContainers).Containers
			data, err = p.client.Logs(ctx, object, false, names[0])
		} else {
			return err
		}
	}

	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-data:
				p.ui.statusBar.StopSpin()
				p.ui.app.QueueUpdateDraw(func() {
					fmt.Fprint(p.ui.podData, string(b))
				})
			}
		}
	}()

	return nil
}

func (p *PodsPresenter) setDetailsView() {
	p.ui.podsDetails.RemoveItem(p.ui.podData)
	p.ui.podsDetails.RemoveItem(p.ui.podEvents)
	switch p.state.details {
	case detailsObject:
		p.ui.podData.SetTitle("Details")
		p.ui.podsDetails.AddItem(p.ui.podData, 0, 1, false)
	case detailsEvents:
		p.ui.podsDetails.AddItem(p.ui.podEvents, 0, 1, false)
	case detailsLog:
		p.ui.podData.SetTitle("Logs")
		p.ui.podsDetails.AddItem(p.ui.podData, 0, 1, false)
	}
}

func (p *PodsPresenter) initKeybindings() {
	p.ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab, tcell.KeyBacktab:
			var toFocus tview.Primitive
			switch p.ui.app.GetFocus() {
			case p.ui.podsTree:
				switch p.state.details {
				case detailsObject, detailsLog:
					toFocus = p.ui.podData
				case detailsEvents:
					toFocus = p.ui.podEvents
				}
			case p.ui.podData, p.ui.podEvents:
				toFocus = p.ui.podsTree
			default:
				toFocus = p.ui.podsTree
			}
			p.ui.app.SetFocus(toFocus)
			p.onFocused(toFocus)
			return nil
		case tcell.KeyF1:
			p.ui.app.SetFocus(p.ui.podData)
			if (p.state.activeComponent == podsDetails ||
				p.state.activeComponent == podsTree) &&
				p.state.object != nil {
				go p.showDetails(p.state.object)
				return nil
			}
		case tcell.KeyCtrlN:
			p.ui.app.SetFocus(p.ui.namespaceDropDown)
			p.ui.app.QueueEvent(tcell.NewEventKey(tcell.KeyEnter, rune(tcell.KeyEnter), tcell.ModNone))
		case tcell.KeyF2:
			if (p.state.activeComponent == podsDetails ||
				p.state.activeComponent == podsTree) &&
				p.state.object != nil {
				p.ui.app.SetFocus(p.ui.podEvents)
				go func() {
					p.displayError(p.showEvents(p.state.object))
				}()
				return nil
			}
		case tcell.KeyF3:
			if (p.state.activeComponent == podsDetails ||
				p.state.activeComponent == podsTree) &&
				p.state.object != nil {
				p.ui.app.SetFocus(p.ui.podData)
				go func() {
					p.displayError(p.showLog(p.state.object))
				}()
				return nil
			}
		case tcell.KeyF5:
			p.refreshFocused()
			return nil
		case tcell.KeyF10:
			p.ui.app.Stop()
			return nil
		case tcell.KeyCtrlF:
			if p.state.activeComponent == podsDetails {
				p.state.fullscreen = !p.state.fullscreen
				p.ui.podsDetails.SetFullScreen(p.state.fullscreen)
				return nil
			}
		}
		return event
	})
}

func (p *PodsPresenter) onFocused(primitive tview.Primitive) {
	p.state.activeComponent = primitiveToComponent(primitive)

	p.resetButtons()
	switch p.state.activeComponent {
	case podsTree:
		p.buttonsForPodsTree()
	case podsDetails:
		p.buttonsForPodsDetails()
	}
}

func (p PodsPresenter) resetButtons() {
	p.ui.actionBar.Clear()
}

func (p *PodsPresenter) buttonsForPodsTree() {
	if p.state.object != nil {
		p.ui.actionBar.AddAction(1, "Details")
		p.ui.actionBar.AddAction(2, "Events")
		p.ui.actionBar.AddAction(3, "Logs")
	}
	p.ui.actionBar.AddAction(5, "Refresh")
	p.ui.actionBar.AddAction(10, "Quit")
}

func (p *PodsPresenter) buttonsForPodsDetails() {
	if p.state.object != nil {
		p.ui.actionBar.AddAction(1, "Details")
		p.ui.actionBar.AddAction(2, "Events")
		p.ui.actionBar.AddAction(3, "Logs")
		p.ui.actionBar.AddAction(5, "Refresh")
	}
	p.ui.actionBar.AddAction(10, "Quit")
}

func (p *PodsPresenter) refreshFocused() {
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

func primitiveToComponent(p tview.Primitive) podsComponent {
	switch p.(type) {
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
