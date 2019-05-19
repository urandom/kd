package mock

//go:generate mockgen -package mock -destination clientset_mock.go github.com/urandom/kd/k8s ClientSet
//go:generate mockgen -package mock -destination objectmetagetter_mock.go github.com/urandom/kd/k8s ObjectMetaGetter

//go:generate mockgen -package mock -destination k8s_corev1_mock.go k8s.io/client-go/kubernetes/typed/core/v1 CoreV1Interface
//go:generate mockgen -package mock -destination k8s_namespace_mock.go k8s.io/client-go/kubernetes/typed/core/v1 NamespaceInterface
//go:generate mockgen -package mock -destination k8s_event_mock.go k8s.io/client-go/kubernetes/typed/core/v1 EventInterface
//go:generate mockgen -package mock -destination k8s_pod_mock.go k8s.io/client-go/kubernetes/typed/core/v1 PodInterface

//go:generate mockgen -package mock -destination k8s_appsv1_mock.go k8s.io/client-go/kubernetes/typed/apps/v1 AppsV1Interface
//go:generate mockgen -package mock -destination k8s_deployment_mock.go k8s.io/client-go/kubernetes/typed/apps/v1 DeploymentInterface

//go:generate mockgen -package mock -destination k8s_selector_mock.go k8s.io/apimachinery/pkg/fields Selector
