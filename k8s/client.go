package k8s

import (
	"os"
	"path/filepath"

	av1 "k8s.io/api/apps/v1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	core "k8s.io/client-go/kubernetes/typed/core/v1"

	"golang.org/x/xerrors"

	"k8s.io/client-go/tools/clientcmd"
)

type clientSet interface {
	AppsV1() apps.AppsV1Interface
	CoreV1() core.CoreV1Interface
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
	Deployments []Deployment
}

type Deployment struct {
	av1.Deployment

	Pods []cv1.Pod
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

func (c Client) PodTree(nsName string) (PodTree, error) {
	core := c.CoreV1()
	apps := c.AppsV1()

	tree := PodTree{}

	pods, err := core.Pods(nsName).List(meta.ListOptions{})
	if err != nil {
		return tree, xerrors.Errorf("getting list of pods for ns %s: %w", nsName, err)
	}

	deployments, err := apps.Deployments(nsName).List(meta.ListOptions{})
	if err != nil {
		return tree, xerrors.Errorf("getting list of deployments for ns %s: %w", nsName, err)
	}

	for _, d := range deployments.Items {
		selector := d.Spec.Selector.MatchLabels

		pods := matchPods(pods.Items, selector)

		tree.Deployments = append(tree.Deployments, Deployment{d, pods})
	}

	return tree, nil
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
