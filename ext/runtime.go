package ext

import (
	"log"

	"github.com/dop251/goja"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui/presenter"
	"golang.org/x/xerrors"
	"sigs.k8s.io/yaml"
)

type runtime struct {
	Client k8s.Client

	vm               *goja.Runtime
	objectSelectChan chan<- presenter.ObjectSelectAction
	picker           presenter.Picker
	displayFunc      func(string) error
}

func (rt runtime) RegisterActionOnObjectSelected(
	cb func(goja.FunctionCall) goja.Value,
) {
	normalized := func(obj k8s.ObjectMetaGetter) (data presenter.ObjectSelectedData, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = xerrors.Errorf("js error on object selected registration: %v", r)
			}
		}()

		val := cb(goja.FunctionCall{Arguments: []goja.Value{rt.vm.ToValue(obj)}})
		data = presenter.ObjectSelectedData{}
		if m, ok := val.Export().(map[string]interface{}); ok {
			if rawCB, ok := m["cb"].(func(goja.FunctionCall) goja.Value); ok {
				data.Callback = func() (err error) {
					if r := recover(); r != nil {
						err = xerrors.Errorf("js error on object selected callback: %v", r)
					}

					rawCB(goja.FunctionCall{})

					return err
				}
			}

			if label, ok := m["label"].(string); ok {
				data.Label = label
			}

		}

		return data, err
	}

	rt.objectSelectChan <- normalized
}

func (rt runtime) Choose(title string, choices []string) string {
	return <-rt.picker.PickFrom(title, choices)
}

func (rt runtime) SetData() {
	rt.vm.Set("kd", rt)
	rt.vm.Set("log", log.Println)
	rt.vm.Set("logf", log.Printf)
}

func (rt runtime) ToYAML(v interface{}) (string, error) {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", xerrors.Errorf("marshaling object to yaml: %w", err)
	}

	return string(b), nil
}

func (rt runtime) Display(text string) error {
	return rt.displayFunc(text)
}
