package presenter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"gitlab.com/tslocum/cview"
	av1 "k8s.io/api/apps/v1"
	kerrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"
)

type Editor struct {
	ui      *ui.UI
	client  *k8s.Client
	confirm Confirm
	form    Form
}

func NewEditor(ui *ui.UI, client *k8s.Client) *Editor {
	return &Editor{
		ui:      ui,
		client:  client,
		confirm: NewConfirm(ui),
		form:    NewForm(ui),
	}
}

func (p *Editor) edit(object k8s.ObjectMetaGetter) (cview.Primitive, error) {
	if c, ok := object.(k8s.Controller); ok {
		object = c.Controller()
	}
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
		var ctx context.Context
		var cancel context.CancelFunc
		var msg string
		for {
			if cancel != nil {
				cancel()
			}

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

			var jsonData []byte
			if jsonData, err = yaml.YAMLToJSON(updated); err != nil {
				return
			}

			ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err = p.client.UpdateObject(ctx, object, jsonData); err != nil {
				var statusError *kerrs.StatusError

				if errors.As(err, &statusError) {
					msg = strings.SplitN(statusError.ErrStatus.Message, "\n", 2)[0] + "\n"
					continue
				}

				var unsupportedErr k8s.UnsupportedObjectError
				if errors.As(err, &unsupportedErr) {
					err = fmt.Errorf(
						"Update not supported on %s: %w", unsupportedErr.TypeName, err)
					return
				}
			}

			return
		}
	})

	return nil, err
}

func (p *Editor) viewText() (err error) {
	log := p.ui.PodData.GetText(true)
	p.ui.App.Suspend(func() {
		_, err = externalEditor([]byte(log), false)
	})

	return err
}

func (p *Editor) delete(object k8s.ObjectMetaGetter) error {
	if !<-p.confirm.DisplayConfirm(
		"Warning",
		"Are you sure you want to delete "+object.GetObjectMeta().GetName()+"?",
	) {
		return nil
	}
	p.ui.StatusBar.SpinText("Deleting " + object.GetObjectMeta().GetName())
	defer p.ui.StatusBar.StopSpin()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.client.DeleteObject(ctx, object, time.Minute); err != nil {
		var unsupportedErr k8s.UnsupportedObjectError
		if errors.As(err, &unsupportedErr) {
			return fmt.Errorf(
				"Update not supported on %s: %w", unsupportedErr.TypeName, err)
		}
		return err
	}

	return nil
}

func (p *Editor) scaleDeployment(c k8s.Controller) (err error) {
	var replicas int
	if d, ok := c.Controller().(*av1.Deployment); ok {
		replicas = int(*d.Spec.Replicas)
	}
	newReplicas := replicas
	done := make(chan struct{})

	p.form.DisplayForm(func(form *cview.Form) {
		form.SetTitle("Scale deployment")

		form.AddInputField("Replicas", strconv.Itoa(replicas), 20, func(text string, lastChar rune) bool {
			return unicode.IsDigit(lastChar)
		}, func(text string) {
			if text == "" {
				return
			}
			newReplicas, err = strconv.Atoi(text)
			if err != nil {
				err = fmt.Errorf("converting %s to number: %w", text, err)
			}
		})
		form.AddButton(buttonClose, func() {
			newReplicas = replicas
			close(done)
			go p.form.Close()
		})
		form.AddButton(buttonOk, func() {
			close(done)
			go p.form.Close()
		})
	})

	<-done
	if err != nil || newReplicas == replicas {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = p.client.ScaleDeployment(ctx, c, newReplicas)
	if err != nil {
		return UserRetryableError{err, func() error {
			return p.scaleDeployment(c)
		}}
	}

	return nil
}

func externalEditor(text []byte, readBack bool) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	f, err := ioutil.TempFile("", "*.log")
	if err != nil {
		return nil, fmt.Errorf("creating temporary file: %w", err)
	}
	defer os.Remove(f.Name())
	_, err = f.Write(text)
	if err != nil {
		return nil, fmt.Errorf("writing data to temporary file: %w", err)
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
		return nil, fmt.Errorf("viewing data through %s: %w", editor, err)
	}

	if readBack {
		f.Seek(0, 0)
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("reading back data from temporary file: %w", editor, err)
		}

		return b, nil
	}

	return nil, nil
}
