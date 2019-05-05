package presenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/rivo/tview"
	"github.com/urandom/kd/ext"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
	kerrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/yaml"
)

type Editor struct {
	ui           *ui.UI
	client       k8s.Client
	confirm      Confirm
	form         Form
	mu           sync.RWMutex
	actionGroups map[string]map[ext.ObjectMutateAction]ext.ObjectMutateActionFunc
}

func NewEditor(ui *ui.UI, client k8s.Client) *Editor {
	return &Editor{
		ui:           ui,
		client:       client,
		confirm:      NewConfirm(ui),
		form:         NewForm(ui),
		actionGroups: map[string]map[ext.ObjectMutateAction]ext.ObjectMutateActionFunc{},
	}
}

func (p *Editor) RegisterObjectMutateActions(
	typeName string,
	actions map[ext.ObjectMutateAction]ext.ObjectMutateActionFunc,
) {
	p.mu.Lock()
	p.actionGroups[typeName] = actions
	p.mu.Unlock()
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

			var jsonData []byte
			if jsonData, err = yaml.YAMLToJSON(updated); err != nil {
				return
			}

			if err = p.client.UpdateObject(object, jsonData); err != nil {
				var statusError *kerrs.StatusError

				if xerrors.As(err, &statusError) {
					msg = strings.SplitN(statusError.ErrStatus.Message, "\n", 2)[0] + "\n"
					continue
				}

				var unsupportedErr k8s.UnsupportedObjectError
				if xerrors.As(err, &unsupportedErr) {
					p.mu.RLock()
					defer p.mu.RUnlock()
					if actions, ok := p.actionGroups[unsupportedErr.TypeName]; ok && actions[ext.MutateUpdate] != nil {
						objInt := reflect.New(reflect.TypeOf(object).Elem()).Interface()
						if err = json.Unmarshal(jsonData, objInt); err != nil {
							err = xerrors.Errorf("unmarshaling data into %s: %w", unsupportedErr.TypeName, err)
							return
						}

						err = actions[ext.MutateUpdate](objInt.(k8s.ObjectMetaGetter))
						if err != nil {
							var statusError *kerrs.StatusError

							if xerrors.As(err, &statusError) {
								msg = strings.SplitN(statusError.ErrStatus.Message, "\n", 2)[0] + "\n"
								continue
							}
						}
					} else {
						err = xerrors.Errorf(
							"Update not supported on %s: %w", unsupportedErr.TypeName, err)
						return
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

func (p *Editor) delete(object k8s.ObjectMetaGetter) (err error) {
	if !<-p.confirm.DisplayConfirm(
		"Warning",
		"Are you sure you want to delete "+object.GetObjectMeta().GetName()+"?",
	) {
		return nil
	}
	p.ui.StatusBar.SpinText("Deleting " + object.GetObjectMeta().GetName())
	defer p.ui.StatusBar.StopSpin()

	return p.client.DeleteObject(object, time.Minute)
}

func (p *Editor) scaleDeployment(d *k8s.Deployment) (err error) {
	replicas := int(*d.Spec.Replicas)
	newReplicas := replicas
	done := make(chan struct{})

	p.form.DisplayForm(func(form *tview.Form) {
		form.SetBorder(true).SetTitle("Scale deployment")

		form.
			AddInputField("Replicas", strconv.Itoa(replicas), 20, func(text string, lastChar rune) bool {
				return unicode.IsDigit(lastChar)
			}, func(text string) {
				if text == "" {
					return
				}
				newReplicas, err = strconv.Atoi(text)
				if err != nil {
					err = xerrors.Errorf("converting %s to number: %w", text, err)
				}
			}).
			AddButton(buttonClose, func() {
				newReplicas = replicas
				close(done)
				go p.form.Close()
			}).
			AddButton(buttonOk, func() {
				close(done)
				go p.form.Close()
			})
	})

	<-done
	if err != nil || newReplicas == replicas {
		return err
	}

	err = p.client.ScaleDeployment(d, newReplicas)
	if err != nil {
		return UserRetryableError{err, func() error {
			return p.scaleDeployment(d)
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
