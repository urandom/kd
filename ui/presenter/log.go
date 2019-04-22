package presenter

import (
	"context"
	"fmt"
	"log"

	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
)

type Log struct {
	picker *Picker
	ui     *ui.UI
	client k8s.Client
}

func NewLog(ui *ui.UI, client k8s.Client) *Log {
	return &Log{
		picker: &Picker{ui: ui},
		ui:     ui,
		client: client,
	}
}

func (p *Log) show(ctx context.Context, object interface{}, container string) (tview.Primitive, error) {
	log.Println("Getting logs")
	p.ui.StatusBar.SpinText("Loading logs", p.ui.App)

	p.ui.App.QueueUpdateDraw(func() {
		p.ui.PodData.Clear().SetRegions(false).SetDynamicColors(true)
	})
	data, err := p.client.Logs(ctx, object, false, container, []string{"yellow", "aqua", "chartreuse"})
	if err != nil {
		if xerrors.As(err, &k8s.ErrMultipleContainers{}) {
			names := err.(k8s.ErrMultipleContainers).Containers
			container := <-p.picker.PickFrom("Containers", names)
			return p.show(ctx, object, container)
		} else {
			return p.ui.PodData, err
		}
	}

	if err != nil {
		return p.ui.PodData, err
	}

	go func() {
		initial := true
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-data:
				if initial {
					p.ui.StatusBar.StopSpin()
					initial = false
				}
				p.ui.App.QueueUpdateDraw(func() {
					fmt.Fprint(p.ui.PodData, tview.TranslateANSI(string(b)))
				})
			}
		}
	}()

	return p.ui.PodData, nil
}
