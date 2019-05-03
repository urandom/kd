package ext

import (
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
			m.Run(e, rt)
		}(e)
	}

	return nil
}

func (m Manager) Run(ext string, rt runtime) {
	rt.vm.RunString(ext)
}
