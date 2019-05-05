package ext

import (
	"sort"

	"github.com/urandom/kd/k8s"
)

type Option struct {
	f func(*options)
}

type DisplayTextFunc func(string)
type DisplayObjectFunc func(k8s.ObjectMetaGetter)
type PickFromFunc func(title string, choices []string) <-chan string

type ObjectSelectedAction func(k8s.ObjectMetaGetter) (ObjectSelectedData, error)
type ObjectSelectedCallback func() error
type ObjectSelectedData struct {
	Label    string
	Callback ObjectSelectedCallback
}
type ObjectSelectedDataSlice []ObjectSelectedData

type ObjectMutateAction string

const (
	MutateUpdate ObjectMutateAction = "update"
	MutateDelete ObjectMutateAction = "delete"
)

type ObjectMutateActionFunc func(obj k8s.ObjectMetaGetter) error
type RegisterObjectMutateActionsFunc func(
	objTypeName string,
	actions map[ObjectMutateAction]ObjectMutateActionFunc,
)

type options struct {
	client                          k8s.Client
	displayTextFunc                 DisplayTextFunc
	displayObjectFunc               DisplayObjectFunc
	pickFromFunc                    PickFromFunc
	registerObjectMutateActionsFunc RegisterObjectMutateActionsFunc
	objectSelectedChan              chan<- ObjectSelectedAction
}

func Client(c k8s.Client) Option {
	return Option{func(o *options) {
		o.client = c
	}}
}

func DisplayText(f DisplayTextFunc) Option {
	return Option{func(o *options) {
		o.displayTextFunc = f
	}}
}

func DisplayObject(f DisplayObjectFunc) Option {
	return Option{func(o *options) {
		o.displayObjectFunc = f
	}}
}

func PickFrom(f PickFromFunc) Option {
	return Option{func(o *options) {
		o.pickFromFunc = f
	}}
}

func RegisterObjectMutateActions(f RegisterObjectMutateActionsFunc) Option {
	return Option{func(o *options) {
		o.registerObjectMutateActionsFunc = f
	}}
}

func ObjectSelectedActionChan(ch chan<- ObjectSelectedAction) Option {
	return Option{func(o *options) {
		o.objectSelectedChan = ch
	}}
}

func (o *options) apply(opts ...Option) {
	for i := range opts {
		opts[i].f(o)
	}
}

func (d ObjectSelectedData) Valid() bool {
	return d.Label != "" && d.Callback != nil
}

func (s ObjectSelectedDataSlice) Labels() []string {
	labels := make([]string, len(s))
	for i := range s {
		labels[i] = s[i].Label
	}

	return labels
}

func (s ObjectSelectedDataSlice) FindForLabel(label string) ObjectSelectedData {
	for i := range s {
		if label == s[i].Label {
			return s[i]
		}
	}
	return ObjectSelectedData{}
}

func (s ObjectSelectedDataSlice) SortByLabel() {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Label < s[j].Label
	})
}
