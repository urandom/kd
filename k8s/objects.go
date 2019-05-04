package k8s

import (
	"context"
	"encoding/json"
	"sort"
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

type PodManager interface {
	Pods() []*cv1.Pod
	SetPods([]*cv1.Pod)
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

type PodTree struct {
	StatefulSets Controllers
	Deployments  Controllers
	DaemonSets   Controllers
	Jobs         Controllers
	CronJobs     Controllers
	Services     Controllers
	pods         []*cv1.Pod
}

func (p PodTree) DeepCopy() PodTree {
	dst := PodTree{
		StatefulSets: p.StatefulSets.DeepCopy(),
		Deployments:  p.Deployments.DeepCopy(),
		DaemonSets:   p.DaemonSets.DeepCopy(),
		Jobs:         p.Jobs.DeepCopy(),
		CronJobs:     p.CronJobs.DeepCopy(),
		Services:     p.Services.DeepCopy(),
		pods:         make([]*cv1.Pod, len(p.pods)),
	}

	for i := range p.pods {
		dst.pods[i] = p.pods[i].DeepCopy()
	}

	return dst
}

type StatefulSet struct {
	av1.StatefulSet

	pods []*cv1.Pod
}

func newStatefulSet(o av1.StatefulSet, allPods []*cv1.Pod) *StatefulSet {
	return &StatefulSet{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (c *StatefulSet) Controller() ObjectMetaGetter {
	return &c.StatefulSet
}

func (c *StatefulSet) Selector() Selector {
	return c.Spec.Selector.MatchLabels
}

func (c *StatefulSet) Pods() []*cv1.Pod {
	return c.pods
}

func (c *StatefulSet) SetPods(pods []*cv1.Pod) {
	c.pods = pods
}

func (c *StatefulSet) DeepCopy() Controller {
	dst := &StatefulSet{
		*(c.StatefulSet.DeepCopy()),
		make([]*cv1.Pod, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type Deployment struct {
	av1.Deployment

	pods []*cv1.Pod
}

func newDeployment(o av1.Deployment, allPods []*cv1.Pod) *Deployment {
	return &Deployment{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (c *Deployment) Controller() ObjectMetaGetter {
	return &c.Deployment
}

func (c *Deployment) Selector() Selector {
	return c.Spec.Selector.MatchLabels
}

func (c *Deployment) Pods() []*cv1.Pod {
	return c.pods
}

func (c *Deployment) SetPods(pods []*cv1.Pod) {
	c.pods = pods
}

func (c *Deployment) DeepCopy() Controller {
	dst := &Deployment{
		*(c.Deployment.DeepCopy()),
		make([]*cv1.Pod, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type DaemonSet struct {
	av1.DaemonSet

	pods []*cv1.Pod
}

func newDaemonSet(o av1.DaemonSet, allPods []*cv1.Pod) *DaemonSet {
	return &DaemonSet{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (c *DaemonSet) Controller() ObjectMetaGetter {
	return &c.DaemonSet
}

func (c *DaemonSet) Selector() Selector {
	return c.Spec.Selector.MatchLabels
}

func (c *DaemonSet) Pods() []*cv1.Pod {
	return c.pods
}

func (c *DaemonSet) SetPods(pods []*cv1.Pod) {
	c.pods = pods
}

func (c *DaemonSet) DeepCopy() Controller {
	dst := &DaemonSet{
		*(c.DaemonSet.DeepCopy()),
		make([]*cv1.Pod, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type Job struct {
	bv1.Job

	pods []*cv1.Pod
}

func newJob(o bv1.Job, allPods []*cv1.Pod) *Job {
	return &Job{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (c *Job) Pods() []*cv1.Pod {
	return c.pods
}

func (c *Job) SetPods(pods []*cv1.Pod) {
	c.pods = pods
}

func (c *Job) Controller() ObjectMetaGetter {
	return &c.Job
}

func (c *Job) Selector() Selector {
	return c.Spec.Selector.MatchLabels
}

func (c *Job) DeepCopy() Controller {
	dst := &Job{
		*(c.Job.DeepCopy()),
		make([]*cv1.Pod, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type CronJob struct {
	bv1b1.CronJob

	selector Selector
	pods     []*cv1.Pod
}

func newCronJob(o bv1b1.CronJob, allPods []*cv1.Pod, jobs []Controller) *CronJob {
	selector := Selector{}
	for _, j := range jobs {
		for _, owner := range j.GetObjectMeta().GetOwnerReferences() {
			if owner.UID == o.GetUID() {
				for k, v := range j.Selector() {
					selector[k] = v
				}
			}
		}
	}

	pods := matchPods(allPods, selector)
	return &CronJob{o, selector, pods}
}

func (c *CronJob) Controller() ObjectMetaGetter {
	return &c.CronJob
}

func (c *CronJob) Selector() Selector {
	return c.selector
}

func (c *CronJob) Pods() []*cv1.Pod {
	return c.pods
}

func (c *CronJob) SetPods(pods []*cv1.Pod) {
	c.pods = pods
}

func (c *CronJob) DeepCopy() Controller {
	dst := &CronJob{
		*(c.CronJob.DeepCopy()),
		c.selector,
		make([]*cv1.Pod, len(c.pods)),
	}
	copy(dst.pods, c.pods)

	return dst
}

type Service struct {
	cv1.Service

	pods []*cv1.Pod
}

func newService(o cv1.Service, allPods []*cv1.Pod) *Service {
	return &Service{o, matchPods(allPods, o.Spec.Selector)}
}

func (c *Service) Controller() ObjectMetaGetter {
	return &c.Service
}

func (c *Service) Selector() Selector {
	return c.Spec.Selector
}

func (c *Service) Pods() []*cv1.Pod {
	return c.pods
}

func (c *Service) SetPods(pods []*cv1.Pod) {
	c.pods = pods
}

func (c *Service) DeepCopy() Controller {
	dst := &Service{
		*(c.Service.DeepCopy()),
		make([]*cv1.Pod, len(c.pods)),
	}
	copy(dst.pods, c.pods)

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
							return newStatefulSet(*o, tree.pods)
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
							return newDeployment(*o, tree.pods)
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
							return newDaemonSet(*o, tree.pods)
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
							return newJob(*o, tree.pods)
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
							return newCronJob(*o, tree.pods, tree.Jobs)
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
							return newService(*o, tree.pods)
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

	tree.pods = make([]*cv1.Pod, len(pods.Items))
	for i := range pods.Items {
		tree.pods[i] = &pods.Items[i]
	}
	for _, o := range statefulsets.Items {
		tree.StatefulSets = append(tree.StatefulSets, newStatefulSet(o, tree.pods))
	}

	for _, o := range deployments.Items {
		tree.Deployments = append(tree.Deployments, newDeployment(o, tree.pods))
	}

	for _, o := range daemonsets.Items {
		tree.DaemonSets = append(tree.DaemonSets, newDaemonSet(o, tree.pods))
	}

	for _, o := range jobs.Items {
		tree.Jobs = append(tree.Jobs, newJob(o, tree.pods))
	}

	for _, o := range cronjobs.Items {
		tree.CronJobs = append(tree.CronJobs, newCronJob(o, tree.pods, tree.Jobs))
	}

	for _, o := range services.Items {
		tree.Services = append(tree.Services, newService(o, tree.pods))
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
	case *StatefulSet:
		update := &av1.StatefulSet{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into stateful set: %w", err)
		}
		update, err := c.AppsV1().StatefulSets(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating stateful set %s: %w", update.GetName(), err)
		}

		v.StatefulSet = *update
	case *Deployment:
		update := &av1.Deployment{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into deployment: %w", err)
		}
		update, err := c.AppsV1().Deployments(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating deployment %s: %w", update.GetName(), err)
		}

		v.Deployment = *update
	case *DaemonSet:
		update := &av1.DaemonSet{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into daemon set: %w", err)
		}
		update, err := c.AppsV1().DaemonSets(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating daemon set %s: %w", update.GetName(), err)
		}

		v.DaemonSet = *update
	case *Job:
		update := &bv1.Job{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into job: %w", err)
		}
		update, err := c.BatchV1().Jobs(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating job %s: %w", update.GetName(), err)
		}

		v.Job = *update
	case *CronJob:
		update := &bv1b1.CronJob{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into cron job: %w", err)
		}
		update, err := c.BatchV1beta1().CronJobs(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating job %s: %w", update.GetName(), err)
		}

		v.CronJob = *update
	case *Service:
		update := &cv1.Service{}

		if err := json.Unmarshal(data, update); err != nil {
			return xerrors.Errorf("unmarshaling data into service: %w", err)
		}
		update, err := c.CoreV1().Services(v.GetNamespace()).Update(update)
		if err != nil {
			return xerrors.Errorf("updating service %s: %w", update.GetName(), err)
		}

		v.Service = *update
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
	}

	return nil
}

func (c Client) FixObject(object ObjectMetaGetter) ObjectMetaGetter {
	if object.GetObjectKind().GroupVersionKind().Kind == "" {
		if v, ok := object.(Controller); ok {
			object = v.Controller()
		}
		switch v := object.(type) {
		case *cv1.Pod:
			v.TypeMeta.Kind = "Pod"
			v.TypeMeta.APIVersion = "v1"
			return v
		case *av1.StatefulSet:
			v.TypeMeta.Kind = "StatefulSet"
			v.TypeMeta.APIVersion = "apps/v1"
			return v
		case *av1.Deployment:
			v.TypeMeta.Kind = "Deployment"
			v.TypeMeta.APIVersion = "extensions/v1beta1"
			return v
		case *av1.DaemonSet:
			v.TypeMeta.Kind = "DaemonSet"
			v.TypeMeta.APIVersion = "extensions/v1beta1"
			return v
		case *bv1.Job:
			v.TypeMeta.Kind = "Job"
			v.TypeMeta.APIVersion = "batch/v1"
			return v
		case *bv1b1.CronJob:
			v.TypeMeta.Kind = "CronJob"
			v.TypeMeta.APIVersion = "batch/v1beta1"
			return v
		case *cv1.Service:
			v.TypeMeta.Kind = "Service"
			v.TypeMeta.APIVersion = "v1"
			return v
		case *cv1.ConfigMap:
			v.TypeMeta.Kind = "ConfigMap"
			v.TypeMeta.APIVersion = "v1"
			return v
		case *cv1.Secret:
			v.TypeMeta.Kind = "Secret"
			v.TypeMeta.APIVersion = "v1"
			return v
		}
	}

	return object
}

func matchPods(pods []*cv1.Pod, selector map[string]string) []*cv1.Pod {
	if len(selector) == 0 {
		return nil
	}
	var matched []*cv1.Pod
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

func modifyPodInList(pods []*cv1.Pod, pod *cv1.Pod, delete bool, labels map[string]string, forceMatch bool) []*cv1.Pod {
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
			selected := matchPods([]*cv1.Pod{pod}, labels)
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
