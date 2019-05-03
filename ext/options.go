package ext

import "github.com/urandom/kd/k8s"

type Option struct {
	f func(*options)
}

type DisplayTextFunc func(string) error
type PickFromFunc func(title string, choices []string) <-chan string

type ObjectSelectedAction func(k8s.ObjectMetaGetter) (ObjectSelectedData, error)
type ObjectSelectedCallback func() error
type ObjectSelectedData struct {
	Label    string
	Callback ObjectSelectedCallback
}

type options struct {
	client             k8s.Client
	displayTextFunc    DisplayTextFunc
	pickFromFunc       PickFromFunc
	objectSelectedChan chan<- ObjectSelectedAction
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

func PickFrom(f PickFromFunc) Option {
	return Option{func(o *options) {
		o.pickFromFunc = f
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
