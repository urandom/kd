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

type RegisterObjectSelectActionFunc func(
	callback ObjectSelectedAction,
)

type ObjectSummaryProvider func(k8s.ObjectMetaGetter) (string, error)
type RegisterObjectSummaryProviderFunc func(
	typeName string,
	provider ObjectSummaryProvider,
)

type options struct {
	client                            *k8s.Client
	displayTextFunc                   DisplayTextFunc
	displayObjectFunc                 DisplayObjectFunc
	pickFromFunc                      PickFromFunc
	registerObjectSelectActionFunc    RegisterObjectSelectActionFunc
	registerObjectSummaryProviderFunc RegisterObjectSummaryProviderFunc
}

func Client(c *k8s.Client) Option {
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

func RegisterObjectSelectAction(f RegisterObjectSelectActionFunc) Option {
	return Option{func(o *options) {
		o.registerObjectSelectActionFunc = f
	}}
}

func RegisterObjectSummaryProvider(f RegisterObjectSummaryProviderFunc) Option {
	return Option{func(o *options) {
		o.registerObjectSummaryProviderFunc = f
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
