package k8s

import (
	"context"
	"encoding/json"

	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"

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
}

type StatefulSet struct {
	av1.StatefulSet

	pods []*cv1.Pod
}

func (c StatefulSet) Controller() ObjectMetaGetter {
	return &c.StatefulSet
}

func (s StatefulSet) Pods() []*cv1.Pod {
	return s.pods
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

func (c Service) Controller() ObjectMetaGetter {
	return &c.Service
}

func (s Service) Pods() []*cv1.Pod {
	return s.pods
}

type PodWatcherEvent struct {
	Tree  PodTree
	Error error
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

	ch := make(chan PodWatcherEvent)
	go func() {
		tree, err := c.PodTree(nsName)
		ch <- PodWatcherEvent{tree, err}

		for {
			select {
			case <-ctx.Done():
				pw.Stop()
				return
			case ev := <-pw.ResultChan():
				pod := ev.Object.(*cv1.Pod)
				fixPod(pod)
			case ev := <-stsw.ResultChan():
				sts := ev.Object.(*av1.StatefulSet)
				fixStatefulSet(sts)
			case ev := <-dw.ResultChan():
				d := ev.Object.(*av1.Deployment)
				fixDeployment(d)
			case ev := <-dsw.ResultChan():
				ds := ev.Object.(*av1.DaemonSet)
				fixDaemonSet(ds)
			case ev := <-jw.ResultChan():
				job := ev.Object.(*bv1.Job)
				fixJob(job)
			case ev := <-cjw.ResultChan():
				cron := ev.Object.(*bv1b1.CronJob)
				fixCronJob(cron)
			case ev := <-sw.ResultChan():
				svc := ev.Object.(*cv1.Service)
				fixService(svc)
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

	for _, o := range statefulsets.Items {
		tree.StatefulSets = append(tree.StatefulSets,
			&StatefulSet{o, matchPods(pods.Items, o.Spec.Selector.MatchLabels)})
	}

	for _, o := range deployments.Items {
		tree.Deployments = append(tree.Deployments,
			&Deployment{o, matchPods(pods.Items, o.Spec.Selector.MatchLabels)})
	}

	for _, o := range daemonsets.Items {
		tree.DaemonSets = append(tree.DaemonSets,
			&DaemonSet{o, matchPods(pods.Items, o.Spec.Selector.MatchLabels)})
	}

	for _, o := range jobs.Items {
		tree.Jobs = append(tree.Jobs,
			&Job{o, matchPods(pods.Items, o.Spec.Selector.MatchLabels)})
	}

	for _, o := range cronjobs.Items {
		tree.CronJobs = append(tree.CronJobs,
			&CronJob{o, matchPods(pods.Items, o.Spec.JobTemplate.Spec.Selector.MatchLabels)})
	}

	for _, o := range services.Items {
		tree.Services = append(tree.Services,
			&Service{o, matchPods(pods.Items, o.Spec.Selector)})
	}

	return tree, nil
}

func (c Client) UpdateObject(object interface{}, data []byte) error {
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

func matchPods(pods []cv1.Pod, selector map[string]string) []*cv1.Pod {
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

		matched = append(matched, &pods[i])
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
