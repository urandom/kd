package presenter

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/rivo/tview"
	"github.com/urandom/kd/ext"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/duration"
	"sigs.k8s.io/yaml"
)

type Details struct {
	ui        *ui.UI
	client    *k8s.Client
	mu        sync.RWMutex
	summaries map[string]ext.ObjectSummaryProvider
}

func NewDetails(ui *ui.UI, client *k8s.Client) *Details {
	return &Details{
		ui:        ui,
		client:    client,
		summaries: map[string]ext.ObjectSummaryProvider{},
	}
}

func (p *Details) RegisterObjectMutateActions(
	typeName string,
	summary ext.ObjectSummaryProvider,
) {
	p.mu.Lock()
	p.summaries[typeName] = summary
	p.mu.Unlock()
}

func (p *Details) show(object k8s.ObjectMetaGetter) tview.Primitive {
	if v, ok := object.(k8s.Controller); ok {
		object = v.Controller()
	}

	p.ui.App.QueueUpdateDraw(func() {
		p.ui.PodData.SetText("").SetRegions(true).
			SetDynamicColors(true).ScrollToBeginning()
		if data, err := yaml.Marshal(object); err == nil {
			fmt.Fprint(p.ui.PodData, "[greenyellow::b]Summary\n=======\n\n")
			p.printObjectSummary(p.ui.PodData, object)
			fmt.Fprint(p.ui.PodData, "[greenyellow::b]Object\n======\n\n")
			fmt.Fprint(p.ui.PodData, string(data))
		} else {
			p.ui.PodData.SetText(err.Error())
		}
	})

	return p.ui.PodData
}

func (p *Details) printObjectSummary(w io.Writer, object k8s.ObjectMetaGetter) {
	typeName := strings.Split(fmt.Sprintf("%T", object), ".")[1]
	fmt.Fprintf(w, "[lightgreen::b]%s: [white::-]%s\n", typeName, object.GetObjectMeta().GetName())

	if object.GetObjectMeta().GetDeletionTimestamp() != nil {
		fmt.Fprintf(w, "[skyblue::b]Delete request:[white::-] %s\n", duration.HumanDuration(object.GetObjectMeta().GetDeletionTimestamp().Sub(time.Now())))
	}

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
		if v.Status.StartTime != nil {
			fmt.Fprintf(w, "[skyblue::b]Age:[white::-] %s\n", duration.HumanDuration(time.Since(v.Status.StartTime.Time)))
		}
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
		if v.Status.LastScheduleTime != nil {
			fmt.Fprintf(w, "[skyblue::b]Last scheduled:[white::-] %s\n", duration.HumanDuration(time.Since(v.Status.LastScheduleTime.Time)))
		}
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
		p.mu.RLock()
		defer p.mu.RUnlock()

		if provider, ok := p.summaries[typeName]; ok {
			if summary, err := provider(object); err == nil {
				fmt.Fprintf(w, summary)
			} else {
				fmt.Fprintf(w, "[red]Error during summary provision: %v[white]\n", err)
			}
		}
	}
	fmt.Fprintln(w, "")
}
