package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"reflect"
	"sort"
	"strings"
	"sync"
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
type ControllerFactory func(o ObjectMetaGetter, tree PodTree) Controller

var (
	factories = map[ControllerType]ControllerFactory{}
	mu        = sync.RWMutex{}
)

func RegisterControllerFactory(kind ControllerType, f ControllerFactory) {
	mu.Lock()
	factories[kind] = f
	mu.Unlock()
}

type Category string

const (
	CategoryStatefulSet Category = "Stateful Set"
	CategoryDeployment  Category = "Deployment"
	CategoryDaemonSet   Category = "Daemon Set"
	CategoryJob         Category = "Job"
	CategoryCronJob     Category = "Cron Job"
	CategoryService     Category = "Service"
)

func (c Category) Plural() string {
	return string(c) + "s"
}

func (c Category) weight() int {
	switch c {
	case CategoryStatefulSet:
		return 1
	case CategoryDeployment:
		return 2
	case CategoryDaemonSet:
		return 3
	case CategoryJob:
		return 4
	case CategoryCronJob:
		return 5
	case CategoryService:
		return 6
	default:
		hash := fnv.New32()
		hash.Write([]byte(c))
		return int(hash.Sum32())
	}
}

type ControllerType string

const (
	StatefulSetType ControllerType = "StatefulSet"
	DeploymentType  ControllerType = "Deployment"
	DaemonSetType   ControllerType = "DaemonSet"
	JobType         ControllerType = "Job"
	CronJobType     ControllerType = "CronJob"
	ServiceType     ControllerType = "Service"
)

type Controller interface {
	PodManager
	ObjectMetaGetter
	Controller() ObjectMetaGetter
	Category() Category
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
	category Category

	selector Selector
	pods     Pods
}

func NewGenericCtrl(o ObjectMetaGetter, category Category, selector Selector, allPods Pods) *Ctrl {
	return &Ctrl{o, category, selector, matchPods(allPods, selector)}
}

func NewInheritCtrl(o ObjectMetaGetter, category Category, tree PodTree) *Ctrl {
	selector := Selector{}
	for _, c := range tree.Controllers {
		for _, owner := range c.GetObjectMeta().GetOwnerReferences() {
			if owner.UID == o.GetObjectMeta().GetUID() {
				for k, v := range c.Selector() {
					selector[k] = v
				}
			}
		}
	}
	return &Ctrl{o, category, selector, matchPods(tree.pods, selector)}
}

func (c *Ctrl) Controller() ObjectMetaGetter {
	return c.ObjectMetaGetter
}

func (c *Ctrl) Category() Category {
	return c.category
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
		cp, c.Category(),
		c.selector, make(Pods, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type PodTree struct {
	Controllers Controllers
	pods        Pods
}

func (p PodTree) DeepCopy() PodTree {
	dst := PodTree{
		Controllers: p.Controllers.DeepCopy(),
		pods:        make(Pods, len(p.pods)),
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

func (c *Client) PodTreeWatcher(ctx context.Context, nsName string) (<-chan PodWatcherEvent, error) {
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
	go selectFromWatchers(ctx, ch, tree, pw, stsw, dw, dsw, jw, cjw, sw)

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

func (c *Client) PodTree(nsName string) (PodTree, error) {
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
		return nil
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

	mu.RLock()
	defer mu.RUnlock()
	factory := factories[StatefulSetType]
	for i := range statefulsets.Items {
		tree.Controllers = append(tree.Controllers, factory(&statefulsets.Items[i], tree))
	}

	factory = factories[DeploymentType]
	for i := range deployments.Items {
		tree.Controllers = append(tree.Controllers, factory(&deployments.Items[i], tree))
	}

	factory = factories[DaemonSetType]
	for i := range daemonsets.Items {
		tree.Controllers = append(tree.Controllers, factory(&daemonsets.Items[i], tree))
	}

	factory = factories[JobType]
	for i := range jobs.Items {
		tree.Controllers = append(tree.Controllers, factory(&jobs.Items[i], tree))
	}

	factory = factories[CronJobType]
	for i := range cronjobs.Items {
		tree.Controllers = append(tree.Controllers, factory(&cronjobs.Items[i], tree))
	}

	factory = factories[ServiceType]
	for i := range services.Items {
		tree.Controllers = append(tree.Controllers, factory(&services.Items[i], tree))
	}

	return tree, nil
}

func (c *Client) UpdateObject(object ObjectMetaGetter, data []byte) error {
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

func (c *Client) DeleteObject(object ObjectMetaGetter, timeout time.Duration) error {
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

	for i := range tree.Controllers {
		tree.Controllers[i].SetPods(
			modifyPodInList(
				tree.Controllers[i].Pods(), pod, delete,
				tree.Controllers[i].Selector(), false),
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

func modifyControllerList(controllers []Controller, o ObjectMetaGetter, factory func() Controller) []Controller {
	found := false
	for i := range controllers {
		if controllers[i].GetObjectMeta().GetUID() == o.GetObjectMeta().GetUID() {
			if factory == nil {
				copy(controllers[i:], controllers[i+1:])
				controllers[len(controllers)-1] = nil
				controllers = controllers[:len(controllers)-1]
			} else {
				controller := factory()
				if controller != nil {
					controllers[i] = controller
				}
			}
		}
		found = true
		break
	}

	if !found && factory != nil {
		controller := factory()
		if controller == nil {
			return controllers
		}
		controllers = append(controllers, controller)
		sort.Slice(controllers, func(i, j int) bool {
			if controllers[i].Category().weight() == controllers[j].Category().weight() {
				return controllers[i].GetObjectMeta().GetName() < controllers[j].GetObjectMeta().GetName()
			}

			return controllers[i].Category().weight() < controllers[j].Category().weight()
		})
	}

	return controllers
}

func selectFromWatchers(
	ctx context.Context, agg chan<- PodWatcherEvent,
	tree PodTree, wi ...watch.Interface) {

	evC := make(chan watch.Event)
	for _, w := range wi {
		go func(w <-chan watch.Event) {
			for {
				select {
				case <-ctx.Done():
					return
				case ev := <-w:
					evC <- ev
				}
			}
		}(w.ResultChan())
	}

	agg <- PodWatcherEvent{Tree: tree.DeepCopy()}
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-evC:
			switch o := ev.Object.(type) {
			case *cv1.Pod:
				modifyPodInTree(&tree, o, ev.Type == watch.Deleted)
				agg <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
			case ObjectMetaGetter:
				typeName := strings.Split(fmt.Sprintf("%T", o), ".")[1]
				var factory ControllerFactory

				mu.RLock()
				factory = factories[ControllerType(typeName)]
				mu.RUnlock()

				tree.Controllers = modifyControllerList(tree.Controllers, o,
					func() Controller {
						return factory(o, tree)
					})

				agg <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
			}
		}
	}
}

func init() {
	RegisterControllerFactory(StatefulSetType, func(o ObjectMetaGetter, tree PodTree) Controller {
		if o, ok := o.(*av1.StatefulSet); ok {
			return NewGenericCtrl(o, CategoryStatefulSet, o.Spec.Selector.MatchLabels, tree.pods)
		}

		return nil
	})

	RegisterControllerFactory(DeploymentType, func(o ObjectMetaGetter, tree PodTree) Controller {
		if o, ok := o.(*av1.Deployment); ok {
			return NewGenericCtrl(o, CategoryDeployment, o.Spec.Selector.MatchLabels, tree.pods)
		}

		return nil
	})

	RegisterControllerFactory(DaemonSetType, func(o ObjectMetaGetter, tree PodTree) Controller {
		if o, ok := o.(*av1.DaemonSet); ok {
			return NewGenericCtrl(o, CategoryDaemonSet, o.Spec.Selector.MatchLabels, tree.pods)
		}

		return nil
	})

	RegisterControllerFactory(JobType, func(o ObjectMetaGetter, tree PodTree) Controller {
		if o, ok := o.(*bv1.Job); ok {
			return NewGenericCtrl(o, CategoryJob, o.Spec.Selector.MatchLabels, tree.pods)
		}

		return nil
	})

	RegisterControllerFactory(CronJobType, func(o ObjectMetaGetter, tree PodTree) Controller {
		if o, ok := o.(*bv1b1.CronJob); ok {
			return NewInheritCtrl(o, CategoryCronJob, tree)
		}

		return nil
	})

	RegisterControllerFactory(ServiceType, func(o ObjectMetaGetter, tree PodTree) Controller {
		if o, ok := o.(*cv1.Service); ok {
			return NewGenericCtrl(o, CategoryService, o.Spec.Selector, tree.pods)
		}

		return nil
	})

}
