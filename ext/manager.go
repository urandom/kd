package ext

import (
	"context"

	"github.com/dop251/goja"
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
	opts ...Option,
) error {
	ext, err := m.loader.Extensions()
	if err != nil {
		return xerrors.Errorf("loading extensions: %v", err)
	}

	o := options{}
	o.apply(opts...)

	for _, e := range ext {
		go func(e string) {
			rt := runtime{
				options: o,
				vm:      goja.New(),
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
