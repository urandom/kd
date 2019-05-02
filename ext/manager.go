package ext

import (
	"context"

	"github.com/dop251/goja"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui/presenter"
	"golang.org/x/xerrors"
)

type Manager struct {
	loader Loader
}

func NewManager(loader Loader) Manager {
	return Manager{loader: loader}
}

func (m Manager) Start(
	ctx context.Context,
	objectSelectChan chan<- presenter.ObjectSelectAction,
	picker presenter.Picker,
	client k8s.Client,
	displayFunc func(string) error,
) error {
	ext, err := m.loader.Extensions()
	if err != nil {
		return xerrors.Errorf("loading extensions: %v", err)
	}

	for _, e := range ext {
		go func(e string) {
			rt := runtime{
				Client:           client,
				vm:               goja.New(),
				objectSelectChan: objectSelectChan,
				picker:           picker,
				displayFunc:      displayFunc,
			}
			rt.SetData()
			m.Run(ctx, e, rt)
		}(e)
	}

	return nil
}

func (m Manager) Run(ctx context.Context, ext string, rt runtime) {
	go func() {
		<-ctx.Done()
		rt.vm.Interrupt(ctx.Err())
	}()
	rt.vm.RunString(ext)
}
