package k8s

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
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

	pods []cv1.Pod
}

func (d Deployment) Pods() []cv1.Pod {
	return d.pods
}

type StatefulSet struct {
	av1.StatefulSet

	pods []cv1.Pod
}

func (s StatefulSet) Pods() []cv1.Pod {
	return s.pods
}

type DaemonSet struct {
	av1.DaemonSet

	pods []cv1.Pod
}

func (d DaemonSet) Pods() []cv1.Pod {
	return d.pods
}

type Job struct {
	bv1.Job

	pods []cv1.Pod
}

func (j Job) Pods() []cv1.Pod {
	return j.pods
}

type CronJob struct {
	bv1b1.CronJob

	pods []cv1.Pod
}

func (c CronJob) Pods() []cv1.Pod {
	return c.pods
}

type Service struct {
	cv1.Service

	pods []cv1.Pod
}

func (s Service) Pods() []cv1.Pod {
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

func (c Client) Events(obj meta.ObjectMeta) ([]cv1.Event, error) {
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
			err = xerrors.Errorf("getting list of pods for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if statefulsets, err = apps.StatefulSets(nsName).List(meta.ListOptions{}); err != nil {
			err = xerrors.Errorf("getting list of stateful sets for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if deployments, err = apps.Deployments(nsName).List(meta.ListOptions{}); err != nil {
			err = xerrors.Errorf("getting list of deployments for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if daemonsets, err = apps.DaemonSets(nsName).List(meta.ListOptions{}); err != nil {
			err = xerrors.Errorf("getting list of daemon sets for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if jobs, err = batch.Jobs(nsName).List(meta.ListOptions{}); err != nil {
			err = xerrors.Errorf("getting list of jobs for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if cronjobs, err = batchBeta.CronJobs(nsName).List(meta.ListOptions{}); err != nil {
			err = xerrors.Errorf("getting list of cronjobs for ns %s: %w", nsName, err)
		}
		return err
	})

	g.Go(func() (err error) {
		if services, err = core.Services(nsName).List(meta.ListOptions{}); err != nil {
			err = xerrors.Errorf("getting list of services for ns %s: %w", nsName, err)
		}
		return err
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

type PodGetter interface {
	Pods() []cv1.Pod
}

type ErrMultipleContainers struct {
	error

	Containers []string
}

func (c Client) Logs(ctx context.Context, object interface{}, previous bool, container string) (<-chan []byte, error) {
	writer := make(chan []byte)
	reader := make(chan []byte)

	go demuxLogs(ctx, writer, reader)

	if pod, ok := object.(cv1.Pod); ok {
		if len(pod.Spec.Containers) > 1 {
			names := make([]string, len(pod.Spec.Containers))
			for i, c := range pod.Spec.Containers {
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

		name := pod.ObjectMeta.GetName()
		req := c.CoreV1().Pods(pod.ObjectMeta.GetNamespace()).GetLogs(
			name, &cv1.PodLogOptions{Previous: previous, Follow: true, Container: container})
		rc, err := req.Stream()
		if err != nil {
			return nil, xerrors.Errorf("getting logs for pod %s: %w", name, err)
		}

		go readLogData(ctx, rc, reader)
	}

	return writer, nil
}

func demuxLogs(ctx context.Context, writer chan<- []byte, reader <-chan []byte) {
	var buf bytes.Buffer
	canTrigger := true
	trig := make(chan struct{})
	for {
		select {
		case <-ctx.Done():
			return
		case <-trig:
			writer <- buf.Bytes()
			buf.Reset()
			canTrigger = true
		case bytes := <-reader:
			buf.Write(bytes)
			// Buffer the writes in a timed window to avoid having to print out
			// line by line when there is a lot of initial content
			if canTrigger {
				time.AfterFunc(250*time.Millisecond, func() { trig <- struct{}{} })
				canTrigger = false
			}
		}
	}
}

func readLogData(ctx context.Context, rc io.ReadCloser, data chan<- []byte) {
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

		data <- bytes
	}
}

func matchPods(pods []cv1.Pod, selector map[string]string) []cv1.Pod {
	var matched []cv1.Pod
	for _, p := range pods {
		labels := p.GetLabels()

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

		matched = append(matched, p)
	}

	return matched
}
