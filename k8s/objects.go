package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

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

type ControllerOperators map[ControllerType]ControllerOperator

func (c ControllerOperators) Types() []ControllerType {
	types := make([]ControllerType, 0, len(c))

	for k := range c {
		types = append(types, k)
	}

	sort.Slice(types, func(i, j int) bool {
		return types[i].weight() < types[j].weight()
	})

	return types
}

type ControllerOperator struct {
	Factory ControllerFactory
	List    ControllerList
	Watch   ControllerWatch
	Update  ControllerUpdate
	Delete  ControllerDelete
}

type ControllerFactory func(o ObjectMetaGetter, tree PodTree) Controller
type ControllerGenerator func(tree PodTree) Controllers
type ControllerList func(c ClientSet, ns string, opts meta.ListOptions) (
	ControllerGenerator, error,
)
type ControllerWatch func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error)
type ControllerUpdate func(c ClientSet, obj ObjectMetaGetter) error
type ControllerDelete func(c ClientSet, obj ObjectMetaGetter, opts meta.DeleteOptions) error

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
	if strings.HasSuffix(string(c), "s") {
		return string(c)
	}
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

func (t ControllerType) weight() int {
	switch t {
	case StatefulSetType:
		return 1
	case DeploymentType:
		return 2
	case DaemonSetType:
		return 3
	case JobType:
		return 4
	case CronJobType:
		return 5
	case ServiceType:
		return 6
	default:
		hash := fnv.New32()
		hash.Write([]byte(t))
		return int(hash.Sum32())
	}
}

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

func NewGenericCtrl(o ObjectMetaGetter, category Category, selector Selector, tree PodTree) *Ctrl {
	return &Ctrl{o, category, selector, matchPods(tree.pods, selector)}
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

func NewCtrlWithPods(o ObjectMetaGetter, category Category, selector Selector, pods Pods) *Ctrl {
	return &Ctrl{o, category, selector, pods}
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
	watchers := []watch.Interface{}
	w, err := c.CoreV1().Pods(nsName).Watch(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("creating pod watcher: %w", err)
	}
	watchers = append(watchers, w)

	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, op := range c.controllerOperators {
		if op.Watch == nil {
			continue
		}

		if w, err = op.Watch(c, nsName, meta.ListOptions{}); err != nil {
			return nil, err
		}

		if w != nil {
			watchers = append(watchers, w)
		}
	}

	tree, err := c.PodTree(nsName)
	if err != nil {
		return nil, xerrors.Errorf("getting initial pod tree: %w", err)
	}

	ch := make(chan PodWatcherEvent)
	go c.selectFromWatchers(ctx, ch, tree, watchers...)

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
	tree := PodTree{}

	c.mu.RLock()
	defer c.mu.RUnlock()

	g := &errgroup.Group{}

	var pods *cv1.PodList
	g.Go(func() (err error) {
		if pods, err = c.CoreV1().Pods(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of pods for ns %s: %w", nsName, err)
		}
		return nil
	})

	types := c.controllerOperators.Types()
	type genType struct {
		gen  ControllerGenerator
		kind ControllerType
	}
	genC := make(chan genType)
	for _, t := range types {
		t := t
		g.Go(func() error {
			if c.controllerOperators[t].List == nil {
				return nil
			}

			gen, err := c.controllerOperators[t].List(c, nsName, meta.ListOptions{})
			if err != nil {
				return err
			}

			genC <- genType{gen, t}
			return nil
		})
	}

	genMapC := make(chan map[ControllerType]ControllerGenerator)
	go func() {
		genMap := map[ControllerType]ControllerGenerator{}
		for genT := range genC {
			genMap[genT.kind] = genT.gen
		}
		genMapC <- genMap
	}()

	if err := g.Wait(); err != nil {
		return tree, err
	}
	close(genC)

	tree.pods = make(Pods, len(pods.Items))
	for i := range pods.Items {
		tree.pods[i] = &pods.Items[i]
	}

	genMap := <-genMapC
	for _, t := range types {
		gen := genMap[t]
		if gen == nil {
			continue
		}
		tree.Controllers = append(tree.Controllers, gen(tree)...)
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
	default:
		typeName := ObjectType(object)
		c.mu.RLock()

		op, ok := c.controllerOperators[ControllerType(typeName)]
		c.mu.RUnlock()
		if !ok || op.Update == nil {
			return UnsupportedObjectError{TypeName: typeName}
		}

		update := reflect.New(reflect.TypeOf(object).Elem()).Interface()
		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into %s: %w", typeName, err)
		}

		return op.Update(c, update.(ObjectMetaGetter))
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
		typeName := ObjectType(object)
		c.mu.RLock()

		op, ok := c.controllerOperators[ControllerType(typeName)]
		c.mu.RUnlock()
		if !ok || op.Delete == nil {
			return UnsupportedObjectError{TypeName: typeName}
		}

		prop := meta.DeletePropagationForeground
		if err := op.Delete(c, object, meta.DeleteOptions{PropagationPolicy: &prop}); err != nil {
			return err
		}

		if op.Watch != nil {
			w, err := op.Watch(c, object.GetObjectMeta().GetNamespace(), meta.ListOptions{
				FieldSelector: "metadata.name=" + v.GetObjectMeta().GetName(),
			})
			if err != nil {
				return xerrors.Errorf("getting %s watcher: %w", typeName, err)
			}
			for {
				select {
				case <-time.After(timeout):
					w.Stop()
					return nil
				case ev := <-w.ResultChan():
					if ev.Type == watch.Deleted {
						return nil
					}
				}
			}
		}

		return nil
	}

	return nil
}

func (c *Client) selectFromWatchers(
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
				typeName := ObjectType(o)
				var factory ControllerFactory

				c.mu.RLock()
				if op, ok := c.controllerOperators[ControllerType(typeName)]; ok {
					factory = op.Factory
				}
				c.mu.RUnlock()
				if ev.Type == watch.Deleted {
					factory = nil
				} else if factory == nil {
					log.Printf("Factory function for type %s missing", typeName)
					continue
				}

				tree.Controllers = modifyControllerList(tree.Controllers, o, factory, tree)

				agg <- PodWatcherEvent{Tree: tree.DeepCopy(), EventType: ev.Type}
			}
		}
	}
}

func ObjectType(o interface{}) string {
	if c, ok := o.(Controller); ok {
		o = c.Controller()
	}

	return strings.Split(fmt.Sprintf("%T", o), ".")[1]
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

func modifyControllerList(controllers Controllers, o ObjectMetaGetter, factory ControllerFactory, tree PodTree) Controllers {
	found := false
	for i := range controllers {
		if controllers[i].GetObjectMeta().GetUID() == o.GetObjectMeta().GetUID() {
			if factory == nil {
				// Delete the controller from the list
				copy(controllers[i:], controllers[i+1:])
				controllers[len(controllers)-1] = nil
				controllers = controllers[:len(controllers)-1]
			} else {
				// Overwrite the controller
				controller := factory(o, tree)
				if controller != nil {
					controllers[i] = controller
				}
			}
			found = true
			break
		}
	}

	if !found && factory != nil {
		// Insert a new controller in the list
		controller := factory(o, tree)
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
