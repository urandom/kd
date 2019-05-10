package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"
)

type UnsupportedObjectError struct {
	TypeName string
}

func (e UnsupportedObjectError) Error() string {
	return fmt.Sprintf("Object '%s' not supported", e.TypeName)
}

type Pods []*cv1.Pod

type PodManager interface {
	Pods() Pods
	SetPods(Pods)
}

type ObjectMetaGetter interface {
	GetObjectMeta() meta.Object
	GetObjectKind() schema.ObjectKind
}

type Selector map[string]string
type ControllerFactory func() Controller

type Controller interface {
	PodManager
	ObjectMetaGetter
	Controller() ObjectMetaGetter
	Selector() Selector
	DeepCopy() Controller
}

type Controllers []Controller

func (c Controllers) DeepCopy() Controllers {
	dst := make(Controllers, len(c))

	for i := range c {
		dst[i] = c[i].DeepCopy()
	}

	return dst
}

type Ctrl struct {
	ObjectMetaGetter
	Category string

	selector Selector
	pods     Pods
}

func newStatefulSetCtrl(o ObjectMetaGetter, selector Selector, allPods Pods) *Ctrl {
	return &Ctrl{o, "Stateful Sets", selector, matchPods(allPods, selector)}
}

func newDeploymentCtrl(o ObjectMetaGetter, selector Selector, allPods Pods) *Ctrl {
	return &Ctrl{o, "Deployments", selector, matchPods(allPods, selector)}
}

func newDaemonSetCtrl(o ObjectMetaGetter, selector Selector, allPods Pods) *Ctrl {
	return &Ctrl{o, "Daemon Sets", selector, matchPods(allPods, selector)}
}

func newJobCtrl(o ObjectMetaGetter, selector Selector, allPods Pods) *Ctrl {
	return &Ctrl{o, "Jobs", selector, matchPods(allPods, selector)}
}

func newCronJobCtrl(o ObjectMetaGetter, jobs []Controller, allPods Pods) *Ctrl {
	selector := Selector{}
	for _, j := range jobs {
		for _, owner := range j.GetObjectMeta().GetOwnerReferences() {
			if owner.UID == o.GetObjectMeta().GetUID() {
				for k, v := range j.Selector() {
					selector[k] = v
				}
			}
		}
	}
	return &Ctrl{o, "Cron Jobs", selector, matchPods(allPods, selector)}
}

func newServiceCtrl(o ObjectMetaGetter, selector Selector, allPods Pods) *Ctrl {
	return &Ctrl{o, "Services", selector, matchPods(allPods, selector)}
}

func (c *Ctrl) Controller() ObjectMetaGetter {
	return c.ObjectMetaGetter
}

func (c *Ctrl) Pods() Pods {
	return c.pods
}

func (c *Ctrl) SetPods(pods Pods) {
	c.pods = pods
}

func (c *Ctrl) Selector() Selector {
	return c.selector
}

func (c *Ctrl) DeepCopy() Controller {
	dcV := reflect.ValueOf(c.ObjectMetaGetter).MethodByName("DeepCopy")

	var cp ObjectMetaGetter
	if dcV.IsValid() {
		cpV := dcV.Call(nil)
		if len(cpV) == 1 {
			if v, ok := cpV[0].Interface().(ObjectMetaGetter); ok {
				cp = v
			}
		}
	}
	if cp == nil {
		cp = c.ObjectMetaGetter
	}

	dst := &Ctrl{
		cp, c.Category,
		c.selector, make(Pods, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type PodTree struct {
	StatefulSets Controllers
	Deployments  Controllers
	DaemonSets   Controllers
	Jobs         Controllers
	CronJobs     Controllers
	Services     Controllers
	pods         Pods
}

func (p PodTree) DeepCopy() PodTree {
	dst := PodTree{
		StatefulSets: p.StatefulSets.DeepCopy(),
		Deployments:  p.Deployments.DeepCopy(),
		DaemonSets:   p.DaemonSets.DeepCopy(),
		Jobs:         p.Jobs.DeepCopy(),
		CronJobs:     p.CronJobs.DeepCopy(),
		Services:     p.Services.DeepCopy(),
		pods:         make(Pods, len(p.pods)),
	}

	for i := range p.pods {
		dst.pods[i] = p.pods[i].DeepCopy()
	}

	return dst
}

type PodWatcherEvent struct {
	Tree      PodTree
	EventType watch.EventType
}

func (c Client) PodTreeWatcher(ctx context.Context, nsName string) (<-chan PodWatcherEvent, error) {
	core := c.CoreV1()
	apps := c.AppsV1()
	batch := c.BatchV1()
	batchBeta := c.BatchV1beta1()

	pw, err := core.Pods(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating pod watcher: %w", err)
	}
	stsw, err := apps.StatefulSets(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating stateful set watcher: %w", err)
	}
	dw, err := apps.Deployments(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating deployment watcher: %w", err)
	}
	dsw, err := apps.DaemonSets(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating daemon set watcher: %w", err)
	}
	jw, err := batch.Jobs(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating job watcher: %w", err)
	}
	cjw, err := batchBeta.CronJobs(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating cron job watcher: %w", err)
	}
	sw, err := core.Services(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating service watcher: %w", err)
	}
	tree, err := c.PodTree(nsName)
	if err != nil {
		return nil, xerrors.Errorf("getting initial pod tree: %w", err)
	}

	ch := make(chan PodWatcherEvent)
	go func() {
		ch <- PodWatcherEvent{Tree: tree.DeepCopy()}
		for {
			select {
			case <-ctx.Done():
				pw.Stop()
				close(ch)
				return
			case ev := <-pw.ResultChan():
				if o, ok := ev.Object.(*cv1.Pod); ok {
					modifyPodInTree(&tree, o, ev.Type == watch.Deleted)
					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			case ev := <-stsw.ResultChan():
				if o, ok := ev.Object.(*av1.StatefulSet); ok {
					var factory ControllerFactory
					if ev.Type != watch.Deleted {
						factory = func() Controller {
							return newStatefulSetCtrl(o, o.Spec.Selector.MatchLabels, tree.pods)
						}
					}
					tree.StatefulSets = modifyControllerList(
						tree.StatefulSets, o, factory)

					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			case ev := <-dw.ResultChan():
				if o, ok := ev.Object.(*av1.Deployment); ok {
					var factory ControllerFactory
					if ev.Type != watch.Deleted {
						factory = func() Controller {
							return newDeploymentCtrl(o, o.Spec.Selector.MatchLabels, tree.pods)
						}
					}
					tree.Deployments = modifyControllerList(
						tree.Deployments, o, factory)

					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			case ev := <-dsw.ResultChan():
				if o, ok := ev.Object.(*av1.DaemonSet); ok {
					var factory ControllerFactory
					if ev.Type != watch.Deleted {
						factory = func() Controller {
							return newDaemonSetCtrl(o, o.Spec.Selector.MatchLabels, tree.pods)
						}
					}
					tree.DaemonSets = modifyControllerList(
						tree.DaemonSets, o, factory)

					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			case ev := <-jw.ResultChan():
				if o, ok := ev.Object.(*bv1.Job); ok {
					var factory ControllerFactory
					if ev.Type != watch.Deleted {
						factory = func() Controller {
							return newJobCtrl(o, o.Spec.Selector.MatchLabels, tree.pods)
						}
					}
					tree.Jobs = modifyControllerList(
						tree.Jobs, o, factory)

					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			case ev := <-cjw.ResultChan():
				if o, ok := ev.Object.(*bv1b1.CronJob); ok {
					var factory ControllerFactory
					if ev.Type != watch.Deleted {
						factory = func() Controller {
							return newCronJobCtrl(o, tree.Jobs, tree.pods)
						}
					}
					tree.CronJobs = modifyControllerList(
						tree.CronJobs, o, factory)

					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			case ev := <-sw.ResultChan():
				if o, ok := ev.Object.(*cv1.Service); ok {
					var factory ControllerFactory
					if ev.Type != watch.Deleted {
						factory = func() Controller {
							return newServiceCtrl(o, o.Spec.Selector, tree.pods)
						}
					}
					tree.Services = modifyControllerList(
						tree.Services, o, factory)

					ch <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
				}
			}
		}
	}()

	window := make(chan PodWatcherEvent)
	go func() {
		var current PodWatcherEvent
		trig := make(chan struct{})
		canTrigger := true
		initial := true
		for {
			select {
			case <-ctx.Done():
				return
			case <-trig:
				window <- current
				canTrigger = true
			case ev, ok := <-ch:
				if !ok {
					return
				}
				current = ev
				if initial {
					initial = false
					window <- current
				} else if canTrigger {
					canTrigger = false
					time.AfterFunc(500*time.Millisecond, func() { trig <- struct{}{} })
				}
			}
		}
	}()

	return window, nil
}

func (c Client) PodTree(nsName string) (PodTree, error) {
	core := c.CoreV1()
	apps := c.AppsV1()
	batch := c.BatchV1()
	batchBeta := c.BatchV1beta1()

	tree := PodTree{}

	var (
		pods         *cv1.PodList
		statefulsets *av1.StatefulSetList
		deployments  *av1.DeploymentList
		daemonsets   *av1.DaemonSetList
		jobs         *bv1.JobList
		cronjobs     *bv1b1.CronJobList
		services     *cv1.ServiceList
	)

	g := &errgroup.Group{}
	g.Go(func() (err error) {
		if pods, err = core.Pods(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of pods for ns %s: %w", nsName, err)
		}
		return nil
	})

	g.Go(func() (err error) {
		if statefulsets, err = apps.StatefulSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of stateful sets for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if deployments, err = apps.Deployments(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of deployments for ns %s: %w", nsName, err)
		}
		return nil
	})

	g.Go(func() (err error) {
		if daemonsets, err = apps.DaemonSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of daemon sets for ns %s: %w", nsName, err)
		}
		return nil
	})

	g.Go(func() (err error) {
		if jobs, err = batch.Jobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of jobs for ns %s: %w", nsName, err)
		}
		return nil
	})

	g.Go(func() (err error) {
		if cronjobs, err = batchBeta.CronJobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of cronjobs for ns %s: %w", nsName, err)
		}
		return nil
	})

	g.Go(func() (err error) {
		if services, err = core.Services(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of services for ns %s: %w", nsName, err)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return tree, err
	}

	tree.pods = make(Pods, len(pods.Items))
	for i := range pods.Items {
		tree.pods[i] = &pods.Items[i]
	}
	for _, o := range statefulsets.Items {
		o := o
		tree.StatefulSets = append(tree.StatefulSets, newStatefulSetCtrl(&o, o.Spec.Selector.MatchLabels, tree.pods))
	}

	for _, o := range deployments.Items {
		o := o
		tree.Deployments = append(tree.Deployments, newDeploymentCtrl(&o, o.Spec.Selector.MatchLabels, tree.pods))
	}

	for _, o := range daemonsets.Items {
		o := o
		tree.DaemonSets = append(tree.DaemonSets, newDaemonSetCtrl(&o, o.Spec.Selector.MatchLabels, tree.pods))
	}

	for _, o := range jobs.Items {
		o := o
		tree.Jobs = append(tree.Jobs, newJobCtrl(&o, o.Spec.Selector.MatchLabels, tree.pods))
	}

	for _, o := range cronjobs.Items {
		o := o
		tree.CronJobs = append(tree.CronJobs, newCronJobCtrl(&o, tree.Jobs, tree.pods))
	}

	for _, o := range services.Items {
		o := o
		tree.Services = append(tree.Services, newServiceCtrl(&o, o.Spec.Selector, tree.pods))
	}

	return tree, nil
}

func (c Client) UpdateObject(object ObjectMetaGetter, data []byte) error {
	switch v := object.(type) {
	case *cv1.Pod:
		update := &cv1.Pod{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into pod: %w", err)
		}
		update, err := c.CoreV1().Pods(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating pod %s: %w", update.GetName(), err)
		}

		*v = *update
	case *Ctrl:
		switch v.ObjectMetaGetter.(type) {
		case *av1.StatefulSet:
			update := &av1.StatefulSet{}

			if err := json.Unmarshal(data, update); err != nil {
				return xerrors.Errorf("unmarshaling data into stateful set: %w", err)
			}
			update, err := c.AppsV1().StatefulSets(v.GetObjectMeta().GetNamespace()).Update(update)
			if err != nil {
				return xerrors.Errorf("updating stateful set %s: %w", update.GetName(), err)
			}

			v.ObjectMetaGetter = update
		case *av1.Deployment:
			update := &av1.Deployment{}

			if err := json.Unmarshal(data, update); err != nil {
				return xerrors.Errorf("unmarshaling data into deployment: %w", err)
			}
			update, err := c.AppsV1().Deployments(v.GetObjectMeta().GetNamespace()).Update(update)
			if err != nil {
				return xerrors.Errorf("updating deployment %s: %w", update.GetName(), err)
			}

			v.ObjectMetaGetter = update
		case *av1.DaemonSet:
			update := &av1.DaemonSet{}

			if err := json.Unmarshal(data, update); err != nil {
				return xerrors.Errorf("unmarshaling data into daemon set: %w", err)
			}
			update, err := c.AppsV1().DaemonSets(v.GetObjectMeta().GetNamespace()).Update(update)
			if err != nil {
				return xerrors.Errorf("updating daemon set %s: %w", update.GetName(), err)
			}

			v.ObjectMetaGetter = update
		case *bv1.Job:
			update := &bv1.Job{}

			if err := json.Unmarshal(data, update); err != nil {
				return xerrors.Errorf("unmarshaling data into job: %w", err)
			}
			update, err := c.BatchV1().Jobs(v.GetObjectMeta().GetNamespace()).Update(update)
			if err != nil {
				return xerrors.Errorf("updating job %s: %w", update.GetName(), err)
			}

			v.ObjectMetaGetter = update
		case *bv1b1.CronJob:
			update := &bv1b1.CronJob{}

			if err := json.Unmarshal(data, update); err != nil {
				return xerrors.Errorf("unmarshaling data into cron job: %w", err)
			}
			update, err := c.BatchV1beta1().CronJobs(v.GetObjectMeta().GetNamespace()).Update(update)
			if err != nil {
				return xerrors.Errorf("updating job %s: %w", update.GetName(), err)
			}

			v.ObjectMetaGetter = update
		case *cv1.Service:
			update := &cv1.Service{}

			if err := json.Unmarshal(data, update); err != nil {
				return xerrors.Errorf("unmarshaling data into service: %w", err)
			}
			update, err := c.CoreV1().Services(v.GetObjectMeta().GetNamespace()).Update(update)
			if err != nil {
				return xerrors.Errorf("updating service %s: %w", update.GetName(), err)
			}

			v.ObjectMetaGetter = update
		}
	default:
		typeName := strings.Split(fmt.Sprintf("%T", object), ".")[1]
		return UnsupportedObjectError{TypeName: typeName}
	}

	return nil
}

func (c Client) DeleteObject(object ObjectMetaGetter, timeout time.Duration) error {
	propagation := meta.DeletePropagationForeground
	switch v := object.(type) {
	case *cv1.Pod:
		err := c.CoreV1().Pods(v.GetNamespace()).Delete(v.GetName(), &meta.DeleteOptions{PropagationPolicy: &propagation})
		if err != nil {
			return xerrors.Errorf("deleting pod %s: %w", v.GetName(), err)
		}

		pw, err := c.CoreV1().Pods(v.GetNamespace()).Watch(meta.ListOptions{FieldSelector: "metadata.name=" + v.GetName()})
		if err != nil {
			return xerrors.Errorf("getting pod watcher: %w", err)
		}
		for {
			select {
			case <-time.After(timeout):
				pw.Stop()
				return nil
			case ev := <-pw.ResultChan():
				if ev.Type == watch.Deleted {
					return nil
				}
			}
		}
	default:
		typeName := strings.Split(fmt.Sprintf("%T", object), ".")[1]
		return UnsupportedObjectError{TypeName: typeName}
	}

	return nil
}

func matchPods(pods Pods, selector map[string]string) Pods {
	if len(selector) == 0 {
		return nil
	}
	var matched Pods
	for i := range pods {
		labels := pods[i].GetLabels()

		mismatch := false
		for k, v := range selector {
			if labels[k] != v {
				mismatch = true
				break
			}
		}

		if mismatch {
			continue
		}

		matched = append(matched, pods[i])
	}

	return matched
}

func modifyPodInTree(tree *PodTree, pod *cv1.Pod, delete bool) {
	tree.pods = modifyPodInList(tree.pods, pod, delete, nil, true)

	for i := range tree.StatefulSets {
		tree.StatefulSets[i].SetPods(
			modifyPodInList(
				tree.StatefulSets[i].Pods(), pod, delete,
				tree.StatefulSets[i].Selector(), false),
		)
	}

	for i := range tree.Deployments {
		tree.Deployments[i].SetPods(
			modifyPodInList(
				tree.Deployments[i].Pods(), pod, delete,
				tree.Deployments[i].Selector(), false),
		)
	}

	for i := range tree.DaemonSets {
		tree.DaemonSets[i].SetPods(
			modifyPodInList(
				tree.DaemonSets[i].Pods(), pod, delete,
				tree.DaemonSets[i].Selector(), false),
		)
	}

	for i := range tree.Jobs {
		tree.Jobs[i].SetPods(
			modifyPodInList(
				tree.Jobs[i].Pods(), pod, delete,
				tree.Jobs[i].Selector(), false),
		)
	}

	for i := range tree.CronJobs {
		tree.CronJobs[i].SetPods(
			modifyPodInList(
				tree.CronJobs[i].Pods(), pod, delete,
				tree.CronJobs[i].Selector(), false),
		)
	}

	for i := range tree.Services {
		tree.Services[i].SetPods(
			modifyPodInList(
				tree.Services[i].Pods(), pod, delete,
				tree.Services[i].Selector(), false),
		)
	}
}

func modifyPodInList(pods Pods, pod *cv1.Pod, delete bool, labels map[string]string, forceMatch bool) Pods {
	found := false
	for idx, p := range pods {
		if p.GetUID() == pod.GetUID() {
			if delete {
				copy(pods[idx:], pods[idx+1:])
				pods[len(pods)-1] = nil
				pods = pods[:len(pods)-1]
			} else {
				pods[idx] = pod
			}
			found = true
			break
		}
	}

	if !found && !delete {
		if forceMatch {
			pods = append(pods, pod)
			sort.Slice(pods, func(i, j int) bool {
				return pods[i].GetName() < pods[j].GetName()
			})
		} else {
			selected := matchPods(Pods{pod}, labels)
			if len(selected) > 0 {
				pods = append(pods, pod)
				sort.Slice(pods, func(i, j int) bool {
					return pods[i].GetName() < pods[j].GetName()
				})
			}
		}
	}

	return pods
}

func modifyControllerList(controllers []Controller, o ObjectMetaGetter, factory ControllerFactory) []Controller {
	found := false
	for i := range controllers {
		if controllers[i].GetObjectMeta().GetUID() == o.GetObjectMeta().GetUID() {
			if factory == nil {
				copy(controllers[i:], controllers[i+1:])
				controllers[len(controllers)-1] = nil
				controllers = controllers[:len(controllers)-1]
			} else {
				controllers[i] = factory()
			}
		}
		found = true
		break
	}

	if !found && factory != nil {
		controllers = append(controllers, factory())
		sort.Slice(controllers, func(i, j int) bool {
			return controllers[i].GetObjectMeta().GetName() < controllers[i].GetObjectMeta().GetName()
		})
	}

	return controllers
}
