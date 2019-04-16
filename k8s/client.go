package k8s

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	batch "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchBeta "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	core "k8s.io/client-go/kubernetes/typed/core/v1"

	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"k8s.io/client-go/tools/clientcmd"
)

type clientSet interface {
	AppsV1() apps.AppsV1Interface
	CoreV1() core.CoreV1Interface
	BatchV1() batch.BatchV1Interface
	BatchV1beta1() batchBeta.BatchV1beta1Interface
}

// Client provides functions around the k8s clientset api.
type Client struct {
	clientSet
}

// New returns a new k8s Client, using the kubeconfig specified by the path, or
// by reading the KUBECONFIG environment variable.
func New(configPath string) (Client, error) {
	if configPath == "" {
		configPath = os.Getenv("KUBECONFIG")
	}

	if configPath == "" {
		configPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	var client = Client{}

	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return client, xerrors.Errorf("building config with path %s: %w", configPath, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return client, xerrors.Errorf("creating k8s clientset: %w", err)
	}

	client.clientSet = clientset

	return client, nil
}

type PodTree struct {
	StatefulSets []*StatefulSet
	Deployments  []*Deployment
	DaemonSets   []*DaemonSet
	Jobs         []*Job
	CronJobs     []*CronJob
	Services     []*Service
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

func (c Client) Namespaces() ([]string, error) {
	ns, err := c.CoreV1().Namespaces().List(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("getting list of namespaces: %w", err)
	}

	namespaces := make([]string, len(ns.Items))
	for i := range ns.Items {
		namespaces[i] = ns.Items[i].GetName()
	}

	return namespaces, nil
}

func (c Client) Events(obj meta.Object) ([]cv1.Event, error) {
	name, ns := obj.GetName(), obj.GetNamespace()
	core := c.CoreV1()
	events := core.Events(ns)
	selector := events.GetFieldSelector(&name, &ns, nil, nil)
	opts := meta.ListOptions{FieldSelector: selector.String()}
	list, err := events.List(opts)
	if err != nil {
		err = xerrors.Errorf("getting list of events for object %s: %w", name, err)
	}
	return list.Items, err
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
			if pods.Items[i].TypeMeta.Kind == "" {
				pods.Items[i].TypeMeta.Kind = "Pod"
				pods.Items[i].TypeMeta.APIVersion = "v1"
			}
		}
		return nil
	})

	g.Go(func() (err error) {
		if statefulsets, err = apps.StatefulSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of stateful sets for ns %s: %w", nsName, err)
		}
		for i := range statefulsets.Items {
			if statefulsets.Items[i].TypeMeta.Kind == "" {
				statefulsets.Items[i].TypeMeta.Kind = "StatefulSet"
				statefulsets.Items[i].TypeMeta.APIVersion = "apps/v1"
			}
		}
		return err
	})

	g.Go(func() (err error) {
		if deployments, err = apps.Deployments(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of deployments for ns %s: %w", nsName, err)
		}
		for i := range deployments.Items {
			if deployments.Items[i].TypeMeta.Kind == "" {
				deployments.Items[i].TypeMeta.Kind = "Deployment"
				deployments.Items[i].TypeMeta.APIVersion = "extensions/v1beta1"
			}
		}
		return nil
	})

	g.Go(func() (err error) {
		if daemonsets, err = apps.DaemonSets(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of daemon sets for ns %s: %w", nsName, err)
		}
		for i := range daemonsets.Items {
			if daemonsets.Items[i].TypeMeta.Kind == "" {
				daemonsets.Items[i].TypeMeta.Kind = "DaemonSet"
				daemonsets.Items[i].TypeMeta.APIVersion = "extensions/v1beta1"
			}
		}
		return nil
	})

	g.Go(func() (err error) {
		if jobs, err = batch.Jobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of jobs for ns %s: %w", nsName, err)
		}
		for i := range jobs.Items {
			if jobs.Items[i].TypeMeta.Kind == "" {
				jobs.Items[i].TypeMeta.Kind = "Job"
				jobs.Items[i].TypeMeta.APIVersion = "batch/v1"
			}
		}
		return nil
	})

	g.Go(func() (err error) {
		if cronjobs, err = batchBeta.CronJobs(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of cronjobs for ns %s: %w", nsName, err)
		}
		for i := range cronjobs.Items {
			if cronjobs.Items[i].TypeMeta.Kind == "" {
				cronjobs.Items[i].TypeMeta.Kind = "CronJob"
				cronjobs.Items[i].TypeMeta.APIVersion = "batch/v1beta1"
			}
		}
		return nil
	})

	g.Go(func() (err error) {
		if services, err = core.Services(nsName).List(meta.ListOptions{}); err != nil {
			return xerrors.Errorf("getting list of services for ns %s: %w", nsName, err)
		}
		for i := range services.Items {
			if services.Items[i].TypeMeta.Kind == "" {
				services.Items[i].TypeMeta.Kind = "Service"
				services.Items[i].TypeMeta.APIVersion = "v1"
			}
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

type ObjectMetaGetter interface {
	GetObjectMeta() meta.Object
}

type Controller interface {
	PodGetter
	Controller() ObjectMetaGetter
}

type PodGetter interface {
	Pods() []*cv1.Pod
}

type ErrMultipleContainers struct {
	error

	Containers []string
}

type logData struct {
	color []byte
	from  []byte
	line  []byte
}

func (c Client) Logs(ctx context.Context, object interface{}, previous bool, container string, colors []string) (<-chan []byte, error) {
	var pods []*cv1.Pod

	switch v := object.(type) {
	case *cv1.Pod:
		pods = append(pods, v)
	case PodGetter:
		pods = v.Pods()
	}

	if len(pods) == 0 {
		return nil, nil
	}

	if len(pods[0].Spec.Containers) > 1 {
		names := make([]string, len(pods[0].Spec.Containers))
		for i, c := range pods[0].Spec.Containers {
			if c.Name == container {
				names = nil
				break
			}
			names[i] = c.Name
		}
		if names != nil {
			return nil, ErrMultipleContainers{
				errors.New("multiple containers"),
				names,
			}
		}
	}

	writer := make(chan []byte)
	reader := make(chan logData)

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	go demuxLogs(ctx, writer, reader, len(pods) > 1)

	for i, pod := range pods {
		name := pod.ObjectMeta.GetName()
		req := c.CoreV1().Pods(pod.ObjectMeta.GetNamespace()).GetLogs(
			name, &cv1.PodLogOptions{Previous: previous, Follow: true, Container: container})
		rc, err := req.Stream()
		if err != nil {
			cancel()
			return nil, xerrors.Errorf("getting logs for pod %s: %w", name, err)
		}

		prefix := name
		idx := strings.LastIndex(name, "-")
		if idx > 0 {
			prefix = name[idx+1:]
		}

		go readLogData(ctx, rc, reader, []byte(prefix), []byte(colors[i%len(colors)]))
	}

	return writer, nil
}

func demuxLogs(ctx context.Context, writer chan<- []byte, reader <-chan logData, showPrefixes bool) {
	var logData []logData
	var buf bytes.Buffer
	canTrigger := true
	trig := make(chan struct{})
	for {
		select {
		case <-ctx.Done():
			return
		case <-trig:
			for _, d := range logData {
				if showPrefixes {
					buf.Write([]byte("["))
					buf.Write(d.color)
					buf.Write([]byte("]"))
					buf.Write(d.from)
					buf.Write([]byte(" → "))
					buf.Write([]byte("[white]"))
				}
				buf.Write(d.line)
			}
			logData = nil

			writer <- buf.Bytes()
			buf.Reset()
			canTrigger = true
		case data, ok := <-reader:
			if !ok {
				return
			}
			logData = append(logData, data)
			// Buffer the writes in a timed window to avoid having to print out
			// line by line when there is a lot of initial content
			if canTrigger {
				time.AfterFunc(250*time.Millisecond, func() { trig <- struct{}{} })
				canTrigger = false
			}
		}
	}
}

func readLogData(ctx context.Context, rc io.ReadCloser, data chan<- logData, prefix []byte, color []byte) {
	defer rc.Close()

	r := bufio.NewReader(rc)
	for {
		if ctx.Err() != nil {
			return
		}
		bytes, err := r.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading stream: %v", err)
				return
			}
			return
		}

		data <- logData{from: prefix, color: color, line: bytes}
	}
}

func matchPods(pods []cv1.Pod, selector map[string]string) []*cv1.Pod {
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
