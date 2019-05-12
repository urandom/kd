package ext

import (
	"log"

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

	for name, e := range ext {
		go func(name, e string) {
			rt := &runtime{
				name:    name,
				options: o,
				vm:      goja.New(),
				ops:     make(chan func()),
			}
			rt.SetData()
			log.Println("Running extension", name)
			m.Run(e, rt)
		}(name, e)
	}

	return nil
}

func (m Manager) Run(ext string, rt *runtime) {
	if _, err := rt.vm.RunString(ext); err == nil {
		rt.loop()
	} else {
		log.Printf("Error running %s: %v", rt.name, err)
	}
}
