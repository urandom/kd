package k8s

import (
	"os"
	"path/filepath"
	"sync"

	av1 "k8s.io/api/apps/v1"
	bv1 "k8s.io/api/batch/v1"
	bv1b1 "k8s.io/api/batch/v1beta1"
	cv1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	apps "k8s.io/client-go/kubernetes/typed/apps/v1"
	batch "k8s.io/client-go/kubernetes/typed/batch/v1"
	batchBeta "k8s.io/client-go/kubernetes/typed/batch/v1beta1"
	core "k8s.io/client-go/kubernetes/typed/core/v1"
	ev1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"

	"golang.org/x/xerrors"

	"k8s.io/client-go/tools/clientcmd"
)

type ClientSet interface {
	AppsV1() apps.AppsV1Interface
	CoreV1() core.CoreV1Interface
	BatchV1() batch.BatchV1Interface
	BatchV1beta1() batchBeta.BatchV1beta1Interface
	ExtensionsV1beta1() ev1.ExtensionsV1beta1Interface
}

// Client provides functions around the k8s clientset api.
type Client struct {
	ClientSet

	mu                  sync.RWMutex
	controllerOperators ControllerOperators
}

// New returns a new k8s Client, using the kubeconfig specified by the path, or
// by reading the KUBECONFIG environment variable.
func New(configPath string) (*Client, error) {
	if configPath == "" {
		configPath = os.Getenv("KUBECONFIG")
	}

	if configPath == "" {
		configPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		return nil, xerrors.Errorf("building config with path %s: %w", configPath, err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, xerrors.Errorf("creating k8s clientset: %w", err)
	}

	client := &Client{controllerOperators: ControllerOperators{}}
	client.ClientSet = clientset

	client.registerDefaults()

	return client, nil
}

func (c *Client) Namespaces() ([]string, error) {
	ns, err := c.CoreV1().Namespaces().List(meta.ListOptions{})
	if err != nil {
		return nil, xerrors.Errorf("getting list of namespaces: %w", err)
	}

	namespaces := make([]string, len(ns.Items)+1)
	namespaces[0] = meta.NamespaceAll
	for i := range ns.Items {
		namespaces[i+1] = ns.Items[i].GetName()
	}

	return namespaces, nil
}

func (c *Client) Events(obj meta.Object) ([]cv1.Event, error) {
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

func (c *Client) RegisterControllerOperator(kind ControllerType, op ControllerOperator) {
	c.mu.Lock()
	c.controllerOperators[kind] = op
	c.mu.Unlock()
}

func (c *Client) registerDefaults() {
	c.RegisterControllerOperator(StatefulSetType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*av1.StatefulSet); ok {
				return NewGenericCtrl(o, CategoryStatefulSet, o.Spec.Selector.MatchLabels, tree)
			}

			return nil
		},
		List: func(c ClientSet, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.AppsV1().StatefulSets(ns).List(opts)
			if err != nil {
				return nil, xerrors.Errorf("getting list for %s: %w", StatefulSetType, err)
			}

			return func(factory ControllerFactory, tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = factory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.AppsV1().StatefulSets(ns).Watch(opts)
			if err != nil {
				err = xerrors.Errorf("getting watcher for %s: %w", StatefulSetType, err)
			}
			return w, err
		},
		Update: func(c ClientSet, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*av1.StatefulSet); ok {
				_, err = c.AppsV1().StatefulSets(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return xerrors.Errorf("updating %s %s: %w", StatefulSetType, o.GetObjectMeta().GetName(), err)
			}
			return err
		},
	})

	c.RegisterControllerOperator(DeploymentType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*av1.Deployment); ok {
				return NewGenericCtrl(o, CategoryDeployment, o.Spec.Selector.MatchLabels, tree)
			}

			return nil
		},
		List: func(c ClientSet, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.AppsV1().Deployments(ns).List(opts)
			if err != nil {
				return nil, xerrors.Errorf("getting list for %s: %w", DeploymentType, err)
			}

			return func(factory ControllerFactory, tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = factory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.AppsV1().Deployments(ns).Watch(opts)
			if err != nil {
				err = xerrors.Errorf("getting watcher for %s: %w", DeploymentType, err)
			}
			return w, err
		},
		Update: func(c ClientSet, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*av1.Deployment); ok {
				_, err = c.AppsV1().Deployments(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return xerrors.Errorf("updating %s %s: %w", DeploymentType, o.GetObjectMeta().GetName(), err)
			}
			return err
		},
	})

	c.RegisterControllerOperator(DaemonSetType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*av1.DaemonSet); ok {
				return NewGenericCtrl(o, CategoryDaemonSet, o.Spec.Selector.MatchLabels, tree)
			}

			return nil
		},
		List: func(c ClientSet, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.AppsV1().DaemonSets(ns).List(opts)
			if err != nil {
				return nil, xerrors.Errorf("getting list for %s: %w", DaemonSetType, err)
			}

			return func(factory ControllerFactory, tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = factory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.AppsV1().DaemonSets(ns).Watch(opts)
			if err != nil {
				err = xerrors.Errorf("getting watcher for %s: %w", DaemonSetType, err)
			}
			return w, err
		},
		Update: func(c ClientSet, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*av1.DaemonSet); ok {
				_, err = c.AppsV1().DaemonSets(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return xerrors.Errorf("updating %s %s: %w", DaemonSetType, o.GetObjectMeta().GetName(), err)
			}
			return err
		},
	})

	c.RegisterControllerOperator(JobType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*bv1.Job); ok {
				return NewGenericCtrl(o, CategoryJob, o.Spec.Selector.MatchLabels, tree)
			}

			return nil
		},
		List: func(c ClientSet, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.BatchV1().Jobs(ns).List(opts)
			if err != nil {
				return nil, xerrors.Errorf("getting list for %s: %w", JobType, err)
			}

			return func(factory ControllerFactory, tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = factory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.BatchV1().Jobs(ns).Watch(opts)
			if err != nil {
				err = xerrors.Errorf("getting watcher for %s: %w", JobType, err)
			}
			return w, err
		},
		Update: func(c ClientSet, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*bv1.Job); ok {
				_, err = c.BatchV1().Jobs(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return xerrors.Errorf("updating %s %s: %w", JobType, o.GetObjectMeta().GetName(), err)
			}
			return err
		},
	})

	c.RegisterControllerOperator(CronJobType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*bv1b1.CronJob); ok {
				return NewInheritCtrl(o, CategoryCronJob, tree)
			}

			return nil
		},
		List: func(c ClientSet, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.BatchV1beta1().CronJobs(ns).List(opts)
			if err != nil {
				return nil, xerrors.Errorf("getting list for %s: %w", CronJobType, err)
			}

			return func(factory ControllerFactory, tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = factory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.BatchV1beta1().CronJobs(ns).Watch(opts)
			if err != nil {
				err = xerrors.Errorf("getting watcher for %s: %w", CronJobType, err)
			}
			return w, err
		},
		Update: func(c ClientSet, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*bv1b1.CronJob); ok {
				_, err = c.BatchV1beta1().CronJobs(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return xerrors.Errorf("updating %s %s: %w", CronJobType, o.GetObjectMeta().GetName(), err)
			}
			return err
		},
	})

	c.RegisterControllerOperator(ServiceType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*cv1.Service); ok {
				return NewGenericCtrl(o, CategoryService, o.Spec.Selector, tree)
			}

			return nil
		},
		List: func(c ClientSet, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.CoreV1().Services(ns).List(opts)
			if err != nil {
				return nil, xerrors.Errorf("getting list for %s: %w", ServiceType, err)
			}

			return func(factory ControllerFactory, tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = factory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.CoreV1().Services(ns).Watch(opts)
			if err != nil {
				err = xerrors.Errorf("getting watcher for %s: %w", ServiceType, err)
			}
			return w, err
		},
		Update: func(c ClientSet, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*cv1.Service); ok {
				_, err = c.CoreV1().Services(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return xerrors.Errorf("updating %s %s: %w", ServiceType, o.GetObjectMeta().GetName(), err)
			}
			return err
		},
	})
}
