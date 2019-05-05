package ext

import (
	"fmt"
	"log"

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
	cb func(goja.FunctionCall) goja.Value,
) {
	normalized := func(obj k8s.ObjectMetaGetter) (ObjectSelectedData, error) {
		type payload struct {
			data ObjectSelectedData
			err  error
		}
		payloadC := make(chan payload)

		rt.ops <- func() {
			var err error
			defer func() {
				if r := recover(); r != nil {
					err = xerrors.Errorf("js error on object selected registration: %v", r)
					payloadC <- payload{err: err}
				}
			}()

			val := cb(goja.FunctionCall{Arguments: []goja.Value{rt.vm.ToValue(obj)}})
			data := ObjectSelectedData{}
			if m, ok := val.Export().(map[string]interface{}); ok {
				if rawCB, ok := m["cb"].(func(goja.FunctionCall) goja.Value); ok {
					data.Callback = func() (err error) {
						defer func() {
							if r := recover(); r != nil {
								err = xerrors.Errorf("js error on object selected callback: %v", r)
							}
						}()

						rawCB(goja.FunctionCall{})

						return err
					}
				}

				if label, ok := m["label"].(string); ok {
					data.Label = label
				}

			}

			payloadC <- payload{data, err}
		}

		p := <-payloadC
		return p.data, p.err
	}

	rt.options.objectSelectedChan <- normalized
}

func (rt *runtime) Client() k8s.Client {
	return rt.options.client
}

func (rt *runtime) Choose(title string, choices []string) string {
	return <-rt.options.pickFromFunc(title, choices)
}

func (rt *runtime) SetData() {
	rt.vm.Set("kd", rt)
	rt.vm.Set("log", log.Println)
	rt.vm.Set("logf", log.Printf)
	rt.vm.Set("sprintf", fmt.Sprintf)
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
