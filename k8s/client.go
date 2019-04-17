package k8s

import (
	"os"
	"path/filepath"

	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	batch "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchBeta "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	core "k8s.io/client-go/kubernetes/typed/core/v1"

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
