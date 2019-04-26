package k8s

import (
	"context"
	"encoding/json"
	"sort"

	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"
)

type PodGetter interface {
	Pods() []*cv1.Pod
}

type ObjectMetaGetter interface {
	GetObjectMeta() meta.Object
}

type Controller interface {
	PodGetter
	Controller() ObjectMetaGetter
}

type PodTree struct {
	StatefulSets []*StatefulSet
	Deployments  []*Deployment
	DaemonSets   []*DaemonSet
	Jobs         []*Job
	CronJobs     []*CronJob
	Services     []*Service
	pods         []*cv1.Pod
}

type StatefulSet struct {
	av1.StatefulSet

	pods []*cv1.Pod
}

func newStatefulSet(o av1.StatefulSet, allPods []*cv1.Pod) *StatefulSet {
	return &StatefulSet{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (c StatefulSet) Controller() ObjectMetaGetter {
	return &c.StatefulSet
}

func (s StatefulSet) Pods() []*cv1.Pod {
	return s.pods
}

func newDeployment(o av1.Deployment, allPods []*cv1.Pod) *Deployment {
	return &Deployment{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

type Deployment struct {
	av1.Deployment

	pods []*cv1.Pod
}

func (c Deployment) Controller() ObjectMetaGetter {
	return &c.Deployment
}

func (d Deployment) Pods() []*cv1.Pod {
	return d.pods
}

type DaemonSet struct {
	av1.DaemonSet

	pods []*cv1.Pod
}

func newDaemonSet(o av1.DaemonSet, allPods []*cv1.Pod) *DaemonSet {
	return &DaemonSet{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (c DaemonSet) Controller() ObjectMetaGetter {
	return &c.DaemonSet
}

func (d DaemonSet) Pods() []*cv1.Pod {
	return d.pods
}

type Job struct {
	bv1.Job

	pods []*cv1.Pod
}

func newJob(o bv1.Job, allPods []*cv1.Pod) *Job {
	return &Job{o, matchPods(allPods, o.Spec.Selector.MatchLabels)}
}

func (j Job) Pods() []*cv1.Pod {
	return j.pods
}

func (c Job) Controller() ObjectMetaGetter {
	return &c.Job
}

type CronJob struct {
	bv1b1.CronJob

	pods []*cv1.Pod
}

func newCronJob(o bv1b1.CronJob, allPods []*cv1.Pod, jobs []*Job) *CronJob {
	var pods []*cv1.Pod
	for _, j := range jobs {
		for _, owner := range j.GetOwnerReferences() {
			if owner.UID == o.GetUID() {
				pods = append(pods, matchPods(allPods, j.Spec.Selector.MatchLabels)...)
			}
		}
	}

	return &CronJob{o, pods}
}

func (c CronJob) Controller() ObjectMetaGetter {
	return &c.CronJob
}

func (c CronJob) Pods() []*cv1.Pod {
	return c.pods
}

type Service struct {
	cv1.Service

	pods []*cv1.Pod
}

func newService(o cv1.Service, allPods []*cv1.Pod) *Service {
	return &Service{o, matchPods(allPods, o.Spec.Selector)}
}

func (c Service) Controller() ObjectMetaGetter {
	return &c.Service
}

func (s Service) Pods() []*cv1.Pod {
	return s.pods
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
		ch <- PodWatcherEvent{Tree: tree}

		for {
			select {
			case <-ctx.Done():
				pw.Stop()
				close(ch)
				return
			case ev := <-pw.ResultChan():
				if o, ok := ev.Object.(*cv1.Pod); ok {
					fixPod(o)
					modifyPodInTree(&tree, o, ev.Type == watch.Deleted)
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			case ev := <-stsw.ResultChan():
				if o, ok := ev.Object.(*av1.StatefulSet); ok {
					fixStatefulSet(o)
					found := false
					for i := range tree.StatefulSets {
						if tree.StatefulSets[i].GetUID() == o.GetUID() {
							if ev.Type == watch.Deleted {
								copy(tree.StatefulSets[i:], tree.StatefulSets[i+1:])
								tree.StatefulSets[len(tree.StatefulSets)-1] = nil
								tree.StatefulSets = tree.StatefulSets[:len(tree.StatefulSets)-1]
							} else {
								tree.StatefulSets[i] = newStatefulSet(*o, tree.pods)
							}
							found = true
							break
						}
					}
					if !found && ev.Type != watch.Deleted {
						tree.StatefulSets = append(tree.StatefulSets, newStatefulSet(*o, tree.pods))
						sort.Slice(tree.StatefulSets, func(i, j int) bool {
							return tree.StatefulSets[i].GetName() < tree.StatefulSets[j].GetName()
						})
					}
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			case ev := <-dw.ResultChan():
				if o, ok := ev.Object.(*av1.Deployment); ok {
					fixDeployment(o)
					found := false
					for i := range tree.Deployments {
						if tree.Deployments[i].GetUID() == o.GetUID() {
							if ev.Type == watch.Deleted {
								copy(tree.Deployments[i:], tree.Deployments[i+1:])
								tree.Deployments[len(tree.Deployments)-1] = nil
								tree.Deployments = tree.Deployments[:len(tree.Deployments)-1]
							} else {
								tree.Deployments[i] = newDeployment(*o, tree.pods)
							}
							found = true
							break
						}
					}
					if !found && ev.Type != watch.Deleted {
						tree.Deployments = append(tree.Deployments, newDeployment(*o, tree.pods))
						sort.Slice(tree.Deployments, func(i, j int) bool {
							return tree.Deployments[i].GetName() < tree.Deployments[j].GetName()
						})
					}
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			case ev := <-dsw.ResultChan():
				if o, ok := ev.Object.(*av1.DaemonSet); ok {
					fixDaemonSet(o)
					found := false
					for i := range tree.DaemonSets {
						if tree.DaemonSets[i].GetUID() == o.GetUID() {
							if ev.Type == watch.Deleted {
								copy(tree.DaemonSets[i:], tree.DaemonSets[i+1:])
								tree.DaemonSets[len(tree.DaemonSets)-1] = nil
								tree.DaemonSets = tree.DaemonSets[:len(tree.DaemonSets)-1]
							} else {
								tree.DaemonSets[i] = newDaemonSet(*o, tree.pods)
							}
							found = true
							break
						}
					}
					if !found && ev.Type != watch.Deleted {
						tree.DaemonSets = append(tree.DaemonSets, newDaemonSet(*o, tree.pods))
						sort.Slice(tree.DaemonSets, func(i, j int) bool {
							return tree.DaemonSets[i].GetName() < tree.DaemonSets[j].GetName()
						})
					}
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			case ev := <-jw.ResultChan():
				if o, ok := ev.Object.(*bv1.Job); ok {
					fixJob(o)
					found := false
					for i := range tree.Jobs {
						if tree.Jobs[i].GetUID() == o.GetUID() {
							if ev.Type == watch.Deleted {
								copy(tree.Jobs[i:], tree.Jobs[i+1:])
								tree.Jobs[len(tree.Jobs)-1] = nil
								tree.Jobs = tree.Jobs[:len(tree.Jobs)-1]
							} else {
								tree.Jobs[i] = newJob(*o, tree.pods)
							}
							found = true
							break
						}
					}
					if !found && ev.Type != watch.Deleted {
						tree.Jobs = append(tree.Jobs, newJob(*o, tree.pods))
						sort.Slice(tree.Jobs, func(i, j int) bool {
							return tree.Jobs[i].GetName() < tree.Jobs[j].GetName()
						})
					}
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			case ev := <-cjw.ResultChan():
				if o, ok := ev.Object.(*bv1b1.CronJob); ok {
					fixCronJob(o)
					found := false
					for i := range tree.CronJobs {
						if tree.CronJobs[i].GetUID() == o.GetUID() {
							if ev.Type == watch.Deleted {
								copy(tree.CronJobs[i:], tree.CronJobs[i+1:])
								tree.CronJobs[len(tree.CronJobs)-1] = nil
								tree.CronJobs = tree.CronJobs[:len(tree.CronJobs)-1]
							} else {
								tree.CronJobs[i] = newCronJob(*o, tree.pods, tree.Jobs)
							}
							found = true
							break
						}
					}
					if !found && ev.Type != watch.Deleted {
						tree.CronJobs = append(tree.CronJobs, newCronJob(*o, tree.pods, tree.Jobs))
						sort.Slice(tree.CronJobs, func(i, j int) bool {
							return tree.CronJobs[i].GetName() < tree.CronJobs[j].GetName()
						})
					}
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			case ev := <-sw.ResultChan():
				if o, ok := ev.Object.(*cv1.Service); ok {
					fixService(o)
					found := false
					for i := range tree.Services {
						if tree.Services[i].GetUID() == o.GetUID() {
							if ev.Type == watch.Deleted {
								copy(tree.Services[i:], tree.Services[i+1:])
								tree.Services[len(tree.Services)-1] = nil
								tree.Services = tree.Services[:len(tree.Services)-1]
							} else {
								tree.Services[i] = newService(*o, tree.pods)
							}
							found = true
							break
						}
					}
					if !found && ev.Type != watch.Deleted {
						tree.Services = append(tree.Services, newService(*o, tree.pods))
						sort.Slice(tree.Services, func(i, j int) bool {
							return tree.Services[i].GetName() < tree.Services[j].GetName()
						})
					}
					ch <- PodWatcherEvent{Tree: tree, EventType: ev.Type}
				}
			}
		}
	}()

	return ch, nil
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
		for i := range pods.Items {
			fixPod(&pods.Items[i])
		}
		return nil
	})

	g.Go(func() (err error) {
		if statefulsets, err = apps.StatefulSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of stateful sets for ns %s: %w", nsName, err)
		}
		for i := range statefulsets.Items {
			fixStatefulSet(&statefulsets.Items[i])
		}
		return err
	})

	g.Go(func() (err error) {
		if deployments, err = apps.Deployments(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of deployments for ns %s: %w", nsName, err)
		}
		for i := range deployments.Items {
			fixDeployment(&deployments.Items[i])
		}
		return nil
	})

	g.Go(func() (err error) {
		if daemonsets, err = apps.DaemonSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of daemon sets for ns %s: %w", nsName, err)
		}
		for i := range daemonsets.Items {
			fixDaemonSet(&daemonsets.Items[i])
		}
		return nil
	})

	g.Go(func() (err error) {
		if jobs, err = batch.Jobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of jobs for ns %s: %w", nsName, err)
		}
		for i := range jobs.Items {
			fixJob(&jobs.Items[i])
		}
		return nil
	})

	g.Go(func() (err error) {
		if cronjobs, err = batchBeta.CronJobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of cronjobs for ns %s: %w", nsName, err)
		}
		for i := range cronjobs.Items {

			fixCronJob(&cronjobs.Items[i])
		}
		return nil
	})

	g.Go(func() (err error) {
		if services, err = core.Services(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of services for ns %s: %w", nsName, err)
		}
		for i := range services.Items {
			fixService(&services.Items[i])
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

func fixPod(pod *cv1.Pod) {
	if pod.TypeMeta.Kind == "" {
		pod.TypeMeta.Kind = "Pod"
		pod.TypeMeta.APIVersion = "v1"
	}
}

func fixStatefulSet(statefulset *av1.StatefulSet) {
	if statefulset.TypeMeta.Kind == "" {
		statefulset.TypeMeta.Kind = "StatefulSet"
		statefulset.TypeMeta.APIVersion = "apps/v1"
	}
}

func fixDeployment(deployment *av1.Deployment) {
	if deployment.TypeMeta.Kind == "" {
		deployment.TypeMeta.Kind = "Deployment"
		deployment.TypeMeta.APIVersion = "extensions/v1beta1"
	}
}

func fixDaemonSet(daemonset *av1.DaemonSet) {
	if daemonset.TypeMeta.Kind == "" {
		daemonset.TypeMeta.Kind = "DaemonSet"
		daemonset.TypeMeta.APIVersion = "extensions/v1beta1"
	}
}

func fixJob(job *bv1.Job) {
	if job.TypeMeta.Kind == "" {
		job.TypeMeta.Kind = "Job"
		job.TypeMeta.APIVersion = "batch/v1"
	}
}

func fixCronJob(cronjob *bv1b1.CronJob) {
	if cronjob.TypeMeta.Kind == "" {
		cronjob.TypeMeta.Kind = "CronJob"
		cronjob.TypeMeta.APIVersion = "batch/v1beta1"
	}
}

func fixService(service *cv1.Service) {
	if service.TypeMeta.Kind == "" {
		service.TypeMeta.Kind = "Service"
		service.TypeMeta.APIVersion = "v1"
	}
}

func modifyPodInTree(tree *PodTree, pod *cv1.Pod, delete bool) {
	modifyPodInList(&tree.pods, pod, delete, nil)

	for i := range tree.StatefulSets {
		modifyPodInList(&tree.StatefulSets[i].pods, pod, delete,
			tree.StatefulSets[i].Spec.Selector.MatchLabels)
	}

	for i := range tree.Deployments {
		modifyPodInList(&tree.Deployments[i].pods, pod, delete,
			tree.Deployments[i].Spec.Selector.MatchLabels)
	}

	for i := range tree.DaemonSets {
		modifyPodInList(&tree.DaemonSets[i].pods, pod, delete,
			tree.DaemonSets[i].Spec.Selector.MatchLabels)
	}

	for i := range tree.Jobs {
		modifyPodInList(&tree.Jobs[i].pods, pod, delete,
			tree.Jobs[i].Spec.Selector.MatchLabels)
	}

	for i := range tree.CronJobs {
		labels := map[string]string{}
		for _, j := range tree.Jobs {
			for _, owner := range j.GetOwnerReferences() {
				if owner.UID == tree.CronJobs[i].GetUID() {
					for k, v := range j.Spec.Selector.MatchLabels {
						labels[k] = v
					}
				}
			}
		}

		modifyPodInList(&tree.CronJobs[i].pods, pod, delete, labels)
	}

	for i := range tree.Services {
		modifyPodInList(&tree.Services[i].pods, pod, delete,
			tree.Services[i].Spec.Selector)
	}
}

func modifyPodInList(pods *[]*cv1.Pod, pod *cv1.Pod, delete bool, labels map[string]string) {
	found := false
	for idx, p := range *pods {
		if p.GetUID() == pod.GetUID() {
			if delete {
				copy((*pods)[idx:], (*pods)[idx+1:])
				(*pods)[len(*pods)-1] = nil
				*pods = (*pods)[:len(*pods)-1]
			} else {
				(*pods)[idx] = pod
			}
			found = true
			break
		}
	}

	if !found && !delete {
		if labels == nil {
			*pods = append(*pods, pod)
			sort.Slice(*pods, func(i, j int) bool {
				return (*pods)[i].GetName() < (*pods)[j].GetName()
			})
		} else {
			selected := matchPods([]*cv1.Pod{pod}, labels)
			if len(selected) > 0 {
				*pods = append(*pods, pod)
				sort.Slice(*pods, func(i, j int) bool {
					return (*pods)[i].GetName() < (*pods)[j].GetName()
				})
			}
		}
	}
}
