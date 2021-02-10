package k8s

import (
	"fmt"
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
	rest "k8s.io/client-go/rest"

	"k8s.io/client-go/tools/clientcmd"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// Client provides functions around the k8s clientset api.
type Client struct {
	*kubernetes.Clientset

	mu                  sync.RWMutex
	controllerOperators ControllerOperators
	config              *rest.Config
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
		return nil, fmt.Errorf("building config with path %s: %w", configPath, err)
	}

	return NewForConfig(config)
}

func NewForConfig(config *rest.Config) (*Client, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating k8s clientset: %w", err)
	}

	client := &Client{controllerOperators: ControllerOperators{}, config: config}
	client.Clientset = clientset

	client.registerDefaults()

	return client, nil
}

func (c *Client) Namespaces() ([]string, error) {
	ns, err := c.CoreV1().Namespaces().List(meta.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting list of namespaces: %w", NormalizeError(err))
	}

	namespaces := make([]string, len(ns.Items)+1)
	namespaces[0] = meta.NamespaceAll
	for i := range ns.Items {
		namespaces[i+1] = ns.Items[i].GetName()
	}

	return namespaces, nil
}

func (c *Client) Events(obj ObjectMetaGetter) ([]cv1.Event, error) {
	name, ns := obj.GetObjectMeta().GetName(), obj.GetObjectMeta().GetNamespace()
	core := c.CoreV1()
	events := core.Events(ns)
	selector := events.GetFieldSelector(&name, &ns, nil, nil)
	opts := meta.ListOptions{FieldSelector: selector.String()}
	list, err := events.List(opts)
	if err != nil {
		return nil, fmt.Errorf("getting list of events for object %s: %w", name, NormalizeError(err))
	}
	return list.Items, nil
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
				return statefulSetFactory(o, tree)
			}

			return nil
		},
		List: func(c *Client, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.AppsV1().StatefulSets(ns).List(opts)
			if err != nil {
				return nil, fmt.Errorf("getting list for %s: %w", StatefulSetType, NormalizeError(err))
			}

			return func(tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = statefulSetFactory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c *Client, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.AppsV1().StatefulSets(ns).Watch(opts)
			if err != nil {
				err = fmt.Errorf("getting watcher for %s: %w", StatefulSetType, NormalizeError(err))
			}
			return w, err
		},
		Update: func(c *Client, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*av1.StatefulSet); ok {
				_, err = c.AppsV1().StatefulSets(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return fmt.Errorf("updating %s %s: %w", StatefulSetType, o.GetObjectMeta().GetName(), NormalizeError(err))
			}
			return err
		},
	})

	c.RegisterControllerOperator(DeploymentType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*av1.Deployment); ok {
				return deploymentFactory(o, tree)
			}

			return nil
		},
		List: func(c *Client, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.AppsV1().Deployments(ns).List(opts)
			if err != nil {
				return nil, fmt.Errorf("getting list for %s: %w", DeploymentType, NormalizeError(err))
			}

			return func(tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = deploymentFactory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c *Client, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.AppsV1().Deployments(ns).Watch(opts)
			if err != nil {
				err = fmt.Errorf("getting watcher for %s: %w", DeploymentType, NormalizeError(err))
			}
			return w, err
		},
		Update: func(c *Client, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*av1.Deployment); ok {
				_, err = c.AppsV1().Deployments(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return fmt.Errorf("updating %s %s: %w", DeploymentType, o.GetObjectMeta().GetName(), NormalizeError(err))
			}
			return err
		},
	})

	c.RegisterControllerOperator(DaemonSetType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*av1.DaemonSet); ok {
				return daemonSetFactory(o, tree)
			}

			return nil
		},
		List: func(c *Client, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.AppsV1().DaemonSets(ns).List(opts)
			if err != nil {
				return nil, fmt.Errorf("getting list for %s: %w", DaemonSetType, NormalizeError(err))
			}

			return func(tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = daemonSetFactory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c *Client, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.AppsV1().DaemonSets(ns).Watch(opts)
			if err != nil {
				err = fmt.Errorf("getting watcher for %s: %w", DaemonSetType, NormalizeError(err))
			}
			return w, err
		},
		Update: func(c *Client, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*av1.DaemonSet); ok {
				_, err = c.AppsV1().DaemonSets(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return fmt.Errorf("updating %s %s: %w", DaemonSetType, o.GetObjectMeta().GetName(), NormalizeError(err))
			}
			return err
		},
	})

	c.RegisterControllerOperator(JobType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*bv1.Job); ok {
				return jobFactory(o, tree)
			}

			return nil
		},
		List: func(c *Client, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.BatchV1().Jobs(ns).List(opts)
			if err != nil {
				return nil, fmt.Errorf("getting list for %s: %w", JobType, NormalizeError(err))
			}

			return func(tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = jobFactory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c *Client, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.BatchV1().Jobs(ns).Watch(opts)
			if err != nil {
				err = fmt.Errorf("getting watcher for %s: %w", JobType, NormalizeError(err))
			}
			return w, err
		},
		Update: func(c *Client, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*bv1.Job); ok {
				_, err = c.BatchV1().Jobs(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return fmt.Errorf("updating %s %s: %w", JobType, o.GetObjectMeta().GetName(), NormalizeError(err))
			}
			return err
		},
	})

	c.RegisterControllerOperator(CronJobType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*bv1b1.CronJob); ok {
				return cronJobFactory(o, tree)
			}

			return nil
		},
		List: func(c *Client, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.BatchV1beta1().CronJobs(ns).List(opts)
			if err != nil {
				return nil, fmt.Errorf("getting list for %s: %w", CronJobType, NormalizeError(err))
			}

			return func(tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = cronJobFactory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c *Client, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.BatchV1beta1().CronJobs(ns).Watch(opts)
			if err != nil {
				err = fmt.Errorf("getting watcher for %s: %w", CronJobType, NormalizeError(err))
			}
			return w, err
		},
		Update: func(c *Client, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*bv1b1.CronJob); ok {
				_, err = c.BatchV1beta1().CronJobs(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return fmt.Errorf("updating %s %s: %w", CronJobType, o.GetObjectMeta().GetName(), NormalizeError(err))
			}
			return err
		},
	})

	c.RegisterControllerOperator(ServiceType, ControllerOperator{
		Factory: func(o ObjectMetaGetter, tree PodTree) Controller {
			if o, ok := o.(*cv1.Service); ok {
				return serviceFactory(o, tree)
			}

			return nil
		},
		List: func(c *Client, ns string, opts meta.ListOptions) (
			ControllerGenerator, error,
		) {
			l, err := c.CoreV1().Services(ns).List(opts)
			if err != nil {
				return nil, fmt.Errorf("getting list for %s: %w", ServiceType, NormalizeError(err))
			}

			return func(tree PodTree) Controllers {
				controllers := make(Controllers, len(l.Items))

				for i := range l.Items {
					controllers[i] = serviceFactory(&l.Items[i], tree)
				}

				return controllers
			}, nil
		},
		Watch: func(c *Client, ns string, opts meta.ListOptions) (watch.Interface, error) {
			w, err := c.CoreV1().Services(ns).Watch(opts)
			if err != nil {
				err = fmt.Errorf("getting watcher for %s: %w", ServiceType, NormalizeError(err))
			}
			return w, err
		},
		Update: func(c *Client, o ObjectMetaGetter) (err error) {
			if o, ok := o.(*cv1.Service); ok {
				_, err = c.CoreV1().Services(o.GetObjectMeta().GetNamespace()).Update(o)
			}
			if err != nil {
				return fmt.Errorf("updating %s %s: %w", ServiceType, o.GetObjectMeta().GetName(), NormalizeError(err))
			}
			return err
		},
	})
}

func statefulSetFactory(o *av1.StatefulSet, tree PodTree) Controller {
	return NewGenericCtrl(o, CategoryStatefulSet, o.Spec.Selector.MatchLabels, tree)
}

func deploymentFactory(o *av1.Deployment, tree PodTree) Controller {
	return NewGenericCtrl(o, CategoryDeployment, o.Spec.Selector.MatchLabels, tree)
}

func daemonSetFactory(o *av1.DaemonSet, tree PodTree) Controller {
	return NewGenericCtrl(o, CategoryDaemonSet, o.Spec.Selector.MatchLabels, tree)
}

func jobFactory(o *bv1.Job, tree PodTree) Controller {
	return NewGenericCtrl(o, CategoryJob, o.Spec.Selector.MatchLabels, tree)
}

func cronJobFactory(o *bv1b1.CronJob, tree PodTree) Controller {
	return NewInheritCtrl(o, CategoryCronJob, tree)
}

func serviceFactory(o *cv1.Service, tree PodTree) Controller {
	return NewGenericCtrl(o, CategoryService, o.Spec.Selector, tree)
}
