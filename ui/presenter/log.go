package presenter

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
)

type Log struct {
	picker Picker
	ui     *ui.UI
	client k8s.Client
}

func NewLog(ui *ui.UI, client k8s.Client) *Log {
	return &Log{
		picker: NewPicker(ui),
		ui:     ui,
		client: client,
	}
}

func (p *Log) show(ctx context.Context, object k8s.ObjectMetaGetter, container string) (tview.Primitive, error) {
	log.Println("Getting logs")
	p.ui.StatusBar.SpinText("Loading logs")

	p.ui.App.QueueUpdateDraw(func() {
		p.ui.PodData.Clear().SetRegions(false).SetDynamicColors(true)
	})
	data, err := p.client.Logs(ctx, object, container, []string{"yellow", "aqua", "chartreuse"})
	if err != nil {
		if xerrors.As(err, &k8s.ErrMultipleContainers{}) {
			names := err.(k8s.ErrMultipleContainers).Containers
			p.ui.StatusBar.StopSpin()
			container := <-p.picker.PickFrom("Containers", names)
			return p.show(ctx, object, container)
		} else {
			return p.ui.PodData, err
		}
	}

	if err != nil {
		return p.ui.PodData, err
	}

	if data == nil {
		p.ui.StatusBar.StopSpin()
		p.ui.StatusBar.ShowTextFor("No containers with logs", 5*time.Second)
		return p.ui.PodData, nil
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
