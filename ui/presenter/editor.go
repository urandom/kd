package presenter

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
	kerrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"
)

type Editor struct {
	ui     *ui.UI
	client k8s.Client
}

func NewEditor(ui *ui.UI, client k8s.Client) *Editor {
	return &Editor{
		ui:     ui,
		client: client,
	}
}

func (p *Editor) edit(object k8s.ObjectMetaGetter) (tview.Primitive, error) {
	objData, err := yaml.Marshal(object)
	if err != nil {
		return nil, err
	}

	preemble := []byte(`# Please edit the object below. Lines beginning with a '#' will be ignored,
# and an empty file will abort the edit. If an error occurs while saving this file will be
# reopened with the relevant failures.
#
`)
	p.ui.App.Suspend(func() {
		var msg string
		for {
			data := append([]byte(nil), preemble...)
			if msg != "" {
				data = append(data, '#', ' ')
				data = append(data, []byte(msg)...)
			}
			data = append(data, objData...)

			var updated []byte
			if updated, err = externalEditor(data, true); err != nil {
				return
			}

			if bytes.Equal(objData, updated) {
				// No updates
				return
			}

			var json []byte
			if json, err = yaml.YAMLToJSON(updated); err != nil {
				return
			}

			if err = p.client.UpdateObject(object, json); err != nil {
				if w := xerrors.Unwrap(err); w != nil {
					if statusError, ok := w.(*kerrs.StatusError); ok {
						msg = strings.SplitN(statusError.ErrStatus.Message, "\n", 2)[0] + "\n"
						continue
					}
				}
			}

			return
		}
	})

	return nil, err
}

func (p *Editor) viewLog() (err error) {
	log := p.ui.PodData.GetText(true)
	p.ui.App.Suspend(func() {
		_, err = externalEditor([]byte(log), false)
	})

	return err
}

func externalEditor(text []byte, readBack bool) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	f, err := ioutil.TempFile("", "*.log")
	if err != nil {
		return nil, xerrors.Errorf("creating temporary file: %w", err)
	}
	defer os.Remove(f.Name())
	_, err = f.Write(text)
	if err != nil {
		return nil, xerrors.Errorf("writing data to temporary file: %w", err)
	}
	f.Sync()

	// Clear the screnn
	fmt.Print("\033[H\033[2J")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt, os.Kill)
	defer signal.Stop(sig)
	go func() {
		<-sig
		cancel()
	}()

	cmd := exec.CommandContext(ctx, editor, f.Name())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err = cmd.Run(); err != nil {
		return nil, xerrors.Errorf("viewing data through %s: %w", editor, err)
	}

	if readBack {
		f.Seek(0, 0)
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, xerrors.Errorf("reading back data from temporary file: %w", editor, err)
		}

		return b, nil
	}

	return nil, nil
}
