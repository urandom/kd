package ext

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"

	"golang.org/x/xerrors"
)

type Loader struct {
	paths []string
}

func NewLoader(paths ...string) (Loader, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Loader{}, xerrors.Errorf("getting user home dir: %w", err)
	}
	defaultPaths := []string{
		filepath.Join(home, ".local", "share", "kd", "extensions"),
		filepath.Join("/", "usr", "local", "share", "kd", "extensions"),
		filepath.Join("/", "usr", "share", "kd", "extensions"),
	}

	pathSet := map[string]struct{}{}
	for _, p := range defaultPaths {
		pathSet[p] = struct{}{}
	}

	for _, p := range paths {
		if _, ok := pathSet[p]; !ok {
			defaultPaths = append(defaultPaths, p)
		}
	}

	return Loader{paths: defaultPaths}, nil
}

func (l Loader) Extensions() (map[string]string, error) {
	data := map[string]string{}
	for _, pa := range l.paths {
		log.Println("Looking up extensions in path", pa)
		paths, err := filepath.Glob(filepath.Join(pa, "*.js"))
		if err != nil {
			return nil, xerrors.Errorf("getting list of extensions: %w", err)
		}

		for _, p := range paths {
			name := path.Base(p)
			if _, ok := data[name]; ok {
				log.Println("The extension", p, "has already been loaded")
				continue
			}
			log.Println("Loading extension from", p)
			b, err := ioutil.ReadFile(p)
			if err != nil {
				log.Println("Error loading extension", p, ": ", err)
			}

			data[name] = string(b)
		}

	}
	return data, nil
}
