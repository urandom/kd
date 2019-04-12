package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"

	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
)

var (
	configF     = flag.String("kubeconfig", "", "Path to the kubeconfig file")
	cpuProfileF = flag.String("cpuprofile", "", "write cpu profile to file")
)

func main() {
	flag.Parse()
	if *cpuProfileF != "" {
		f, err := os.Create(*cpuProfileF)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if err := setupLogging(); err != nil {
		log.Fatal(err)
	}

	p := ui.NewMainPresenter(ui.New(), func() (k8s.Client, error) { return k8s.New(*configF) })

	if err := p.Run(); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
}

func configPath() string {
	if *configF == "" {
		config := os.Getenv("KUBECONFIG")

		if config == "" {
			return filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}

		return config
	}

	return *configF
}

func setupLogging() error {
	f, err := os.OpenFile("/tmp/kd.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return xerrors.Errorf("opening log file: %w", err)
	}

	log.SetOutput(f)
	return nil
}
