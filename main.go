package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"

	"net/http"
	_ "net/http/pprof"

	"github.com/urandom/kd/ext"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"github.com/urandom/kd/ui/presenter"
	"k8s.io/klog"
)

var (
	configF        = flag.String("kubeconfig", "", "path to kubeconfig file")
	cpuProfileF    = flag.String("cpuprofile", "", "write cpu profile to file")
	cpuPortF       = flag.Int("cpuport", 0, "serve cpu profile on port")
	extensionPathF = flag.String("extensions", "", "path(s) to extensions directory. Comma separated")
)

func main() {
	klog.InitFlags(flag.CommandLine)
	flag.Parse()
	if *cpuProfileF != "" {
		f, err := os.Create(*cpuProfileF)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *cpuPortF != 0 {
		go func() {
			log.Println(http.ListenAndServe("localhost:"+strconv.Itoa(*cpuPortF), nil))
		}()
	}

	if err := setupLogging(); err != nil {
		log.Fatal(err)
	}

	loader, err := ext.NewLoader(strings.Split(*extensionPathF, ",")...)
	if err != nil {
		log.Fatal(err)
	}
	manager := ext.NewManager(loader)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := presenter.NewMain(ui.New(), manager, func() (*k8s.Client, error) { return k8s.New(ctx, *configF) })

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
		return fmt.Errorf("opening log file: %w", err)
	}

	log.SetOutput(f)
	return nil
}
