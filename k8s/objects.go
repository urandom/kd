package k8s

import (
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
		fixPods(pods.Items...)
		return nil
	})

	g.Go(func() (err error) {
		if statefulsets, err = apps.StatefulSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of stateful sets for ns %s: %w", nsName, err)
		}
		fixStatefulSets(statefulsets.Items...)
		return err
	})

	g.Go(func() (err error) {
		if deployments, err = apps.Deployments(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of deployments for ns %s: %w", nsName, err)
		}
		fixDeployments(deployments.Items...)
		return nil
	})

	g.Go(func() (err error) {
		if daemonsets, err = apps.DaemonSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of daemon sets for ns %s: %w", nsName, err)
		}
		fixDaemonSets(daemonsets.Items...)
		return nil
	})

	g.Go(func() (err error) {
		if jobs, err = batch.Jobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of jobs for ns %s: %w", nsName, err)
		}
		fixJobs(jobs.Items...)
		return nil
	})

	g.Go(func() (err error) {
		if cronjobs, err = batchBeta.CronJobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of cronjobs for ns %s: %w", nsName, err)
		}
		fixCronJobs(cronjobs.Items...)
		return nil
	})

	g.Go(func() (err error) {
		if services, err = core.Services(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of services for ns %s: %w", nsName, err)
		}
		fixServices(services.Items...)
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

func fixPods(pods ...cv1.Pod) {
	for i := range pods {
		if pods[i].TypeMeta.Kind == "" {
			pods[i].TypeMeta.Kind = "Pod"
			pods[i].TypeMeta.APIVersion = "v1"
		}
	}
}

func fixStatefulSets(statefulsets ...av1.StatefulSet) {
	for i := range statefulsets {
		if statefulsets[i].TypeMeta.Kind == "" {
			statefulsets[i].TypeMeta.Kind = "StatefulSet"
			statefulsets[i].TypeMeta.APIVersion = "apps/v1"
		}
	}
}

func fixDeployments(deployments ...av1.Deployment) {
	for i := range deployments {
		if deployments[i].TypeMeta.Kind == "" {
			deployments[i].TypeMeta.Kind = "Deployment"
			deployments[i].TypeMeta.APIVersion = "extensions/v1beta1"
		}
	}
}

func fixDaemonSets(daemonsets ...av1.DaemonSet) {
	for i := range daemonsets {
		if daemonsets[i].TypeMeta.Kind == "" {
			daemonsets[i].TypeMeta.Kind = "DaemonSet"
			daemonsets[i].TypeMeta.APIVersion = "extensions/v1beta1"
		}
	}
}

func fixJobs(jobs ...bv1.Job) {
	for i := range jobs {
		if jobs[i].TypeMeta.Kind == "" {
			jobs[i].TypeMeta.Kind = "Job"
			jobs[i].TypeMeta.APIVersion = "batch/v1"
		}
	}
}

func fixCronJobs(cronjobs ...bv1b1.CronJob) {
	for i := range cronjobs {
		if cronjobs[i].TypeMeta.Kind == "" {
			cronjobs[i].TypeMeta.Kind = "CronJob"
			cronjobs[i].TypeMeta.APIVersion = "batch/v1beta1"
		}
	}
}

func fixServices(services ...cv1.Service) {
	for i := range services {
		if services[i].TypeMeta.Kind == "" {
			services[i].TypeMeta.Kind = "Service"
			services[i].TypeMeta.APIVersion = "v1"
		}
	}
}
