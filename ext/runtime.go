package ext

import (
	"fmt"
	"log"
	"reflect"
	"sort"

	"github.com/dop251/goja"
	"github.com/urandom/kd/k8s"
	"golang.org/x/xerrors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/yaml"
)

type runtime struct {
	name    string
	options options
	vm      *goja.Runtime
	ops     chan func()
}

func (rt *runtime) RegisterActionOnObjectSelected(
	cb func(k8s.ObjectMetaGetter) (ObjectSelectedData, error),
) {
	if rt.options.registerObjectSelectActionFunc == nil {
		return
	}

	normalized := func(obj k8s.ObjectMetaGetter) (ObjectSelectedData, error) {
		type payload struct {
			data ObjectSelectedData
			err  error
		}
		payloadC := make(chan payload)

		rt.ops <- func() {
			data, err := cb(obj)
			if data.Callback != nil {
				cb := data.Callback
				data.Callback = func() (err error) {
					errC := make(chan error)

					rt.ops <- func() {
						errC <- cb()
					}

					return <-errC
				}
			}

			payloadC <- payload{data, err}
		}

		p := <-payloadC
		return p.data, p.err
	}

	rt.options.registerObjectSelectActionFunc(normalized)
}

func (rt *runtime) RegisterObjectSummaryProvider(typeName string, provider func(k8s.ObjectMetaGetter) (string, error)) {
	if rt.options.registerObjectSummaryProviderFunc == nil {
		return
	}

	normalized := func(obj k8s.ObjectMetaGetter) (string, error) {
		type payload struct {
			data string
			err  error
		}
		payloadC := make(chan payload)

		rt.ops <- func() {
			val, err := provider(obj)
			payloadC <- payload{data: val, err: err}
		}

		p := <-payloadC
		return p.data, p.err
	}

	rt.options.registerObjectSummaryProviderFunc(typeName, normalized)
}

type ControllerOperator struct {
	Factory k8s.ControllerFactory
	List    k8s.ControllerList
	Watch   func(goja.FunctionCall) goja.Value
	Update  k8s.ControllerUpdate
	Delete  k8s.ControllerDelete
}

func (rt *runtime) RegisterControllerOperator(typeName k8s.ControllerType, op ControllerOperator) {
	var k8sOp k8s.ControllerOperator
	if op.Factory != nil {
		factory := op.Factory
		k8sOp.Factory = func(o k8s.ObjectMetaGetter, tree k8s.PodTree) k8s.Controller {
			ctrlC := make(chan k8s.Controller)

			rt.ops <- func() {
				ctrlC <- factory(o, tree)
			}

			return <-ctrlC
		}
	}

	if op.List != nil {
		list := op.List
		k8sOp.List = func(c k8s.ClientSet, ns string, opts meta.ListOptions) (k8s.ControllerGenerator, error) {

			type payload struct {
				gen k8s.ControllerGenerator
				err error
			}
			payloadC := make(chan payload)
			rt.ops <- func() {
				gen, err := list(c, ns, opts)

				var normGen k8s.ControllerGenerator
				if gen != nil {
					normGen = func(tree k8s.PodTree) k8s.Controllers {
						controllersC := make(chan k8s.Controllers)
						rt.ops <- func() {
							controllersC <- gen(tree)
						}

						return <-controllersC
					}
				}

				payloadC <- payload{normGen, err}
			}

			p := <-payloadC
			return p.gen, p.err
		}
	}

	if op.Watch != nil {
		w := op.Watch
		k8sOp.Watch = func(c k8s.ClientSet, ns string, opts meta.ListOptions) (watch.Interface, error) {
			type payload struct {
				watch watch.Interface
				err   error
			}
			payloadC := make(chan payload)

			rt.ops <- func() {
				defer func() {
					if err := recover(); err != nil {
						payloadC <- payload{err: xerrors.Errorf("%v", err)}
					}
				}()
				val := w(goja.FunctionCall{Arguments: []goja.Value{
					rt.vm.ToValue(c),
					rt.vm.ToValue(ns),
					rt.vm.ToValue(opts),
				}})

				if watch, ok := val.Export().(watch.Interface); ok {
					payloadC <- payload{watch: watch}
				} else {
					payloadC <- payload{err: xerrors.Errorf("invalid watch return type: %T", val.Export())}
				}
			}

			p := <-payloadC
			return p.watch, p.err
		}
	}

	if op.Update != nil {
		update := op.Update
		k8sOp.Update = func(c k8s.ClientSet, o k8s.ObjectMetaGetter) error {
			errC := make(chan error)

			rt.ops <- func() {
				errC <- update(c, o)
			}

			return <-errC
		}
	}

	if op.Delete != nil {
		delete := op.Delete
		k8sOp.Delete = func(c k8s.ClientSet, o k8s.ObjectMetaGetter, opts meta.DeleteOptions) error {
			errC := make(chan error)

			rt.ops <- func() {
				errC <- delete(c, o, opts)
			}

			return <-errC
		}
	}

	rt.Client().RegisterControllerOperator(typeName, k8sOp)
}

func (rt *runtime) Client() *k8s.Client {
	return rt.options.client
}

func (rt *runtime) PickFrom(title string, choices []string) string {
	sort.Strings(choices)
	return <-rt.options.pickFromFunc(title, choices)
}

func (rt *runtime) SetData() {
	rt.vm.Set("kd", rt)
	rt.vm.Set("log", log.Println)
	rt.vm.Set("logf", log.Printf)
	rt.vm.Set("sprintf", fmt.Sprintf)
	rt.vm.Set("derefString", func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	})
	rt.vm.Set("ptr", func(o interface{}) interface{} {
		v := reflect.ValueOf(o)
		vp := reflect.New(v.Type())
		vp.Elem().Set(v)

		return vp.Interface()
	})
	rt.vm.Set("GenericCtrl", k8s.NewGenericCtrl)
}

func (rt *runtime) ToYAML(v interface{}) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", xerrors.Errorf("marshaling object to yaml: %w", err)
	}

	return string(b), nil
}

func (rt *runtime) Display(v interface{}) {
	switch vv := v.(type) {
	case string:
		rt.options.displayTextFunc(vv)
	case []byte:
		rt.options.displayTextFunc(string(vv))
	case k8s.ObjectMetaGetter:
		rt.options.displayObjectFunc(vv)
	}
}

func (rt *runtime) loop() {
	for {
		select {
		case op := <-rt.ops:
			op()
		}
	}
}
