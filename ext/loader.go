package ext

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"
)

type Loader struct {
	path string
}

func NewLoader(path string) (Loader, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Loader{}, xerrors.Errorf("getting user home dir: %w", err)
		}
		path = filepath.Join(home, ".local", "share", "kd", "extensions")
	}

	return Loader{path: path}, nil
}

func (l Loader) Extensions() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(l.path, "*.js"))
	if err != nil {
		return nil, xerrors.Errorf("getting list of extensions: %w", err)
	}

	data := make([]string, 0, len(paths))
	for _, p := range paths {
		log.Println("Loading extension from", p)
		b, err := ioutil.ReadFile(p)
		if err != nil {
			return nil, xerrors.Errorf("reading extension at %s: %w", p, err)
		}
		data = append(data, string(b))
	}

	return data, nil
}
