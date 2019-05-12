package ext

import (
	"fmt"
	"log"
	"sort"

	"github.com/dop251/goja"
	"github.com/urandom/kd/k8s"
	"golang.org/x/xerrors"
	"sigs.k8s.io/yaml"
)

type runtime struct {
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

func (rt *runtime) RegisterControllerOperator(typeName k8s.ControllerType, op k8s.ControllerOperator) {
	rt.Client().RegisterControllerOperator(typeName, op)
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
