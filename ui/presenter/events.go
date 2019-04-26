package presenter

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"k8s.io/apimachinery/pkg/util/duration"
)

type Events struct {
	ui     *ui.UI
	client k8s.Client
}

func NewEvents(ui *ui.UI, client k8s.Client) *Events {
	return &Events{
		ui:     ui,
		client: client,
	}
}

func (p *Events) show(object k8s.ObjectMetaGetter) (tview.Primitive, error) {
	meta := object.GetObjectMeta()

	log.Printf("Getting events for object %s", meta.GetName())
	p.ui.StatusBar.SpinText("Loading events", p.ui.App)
	defer p.ui.StatusBar.StopSpin()

	list, err := p.client.Events(meta)
	if err != nil {
		log.Printf("Error getting events for object %s: %s", meta.GetName(), err)
		return p.ui.PodEvents, UserRetryableError{err, func() error {
			_, err := p.show(object)
			return err
		}}
	}

	p.ui.App.QueueUpdateDraw(func() {
		p.ui.PodEvents.Clear()
		headers := []string{}
		if len(list) == 0 {
			headers = append(headers, "No events")
		} else {
			headers = append(headers, "Type", "Reason", "Age", "From", "Message")
		}

		for i, h := range headers {
			p.ui.PodEvents.SetCell(0, i, tview.NewTableCell(h).
				SetAlign(tview.AlignCenter).
				SetTextColor(tcell.ColorYellow))

		}

		if len(list) == 0 {
			return
		}

		for i, event := range list {
			for j := range headers {
				switch j {
				case 0:
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(event.Type))
				case 1:
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(event.Reason))
				case 2:
					first := duration.HumanDuration(time.Since(event.FirstTimestamp.Time))
					interval := first
					if event.Count > 1 {
						last := duration.HumanDuration(time.Since(event.LastTimestamp.Time))
						interval = fmt.Sprintf("%s (x%d since %s)", last, event.Count, first)
					}
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(interval))
				case 3:
					from := event.Source.Component
					if len(event.Source.Host) > 0 {
						from += ", " + event.Source.Host
					}
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(from))
				case 4:
					p.ui.PodEvents.SetCell(i+1, j,
						tview.NewTableCell(strings.TrimSpace(event.Message)))
				}
			}
		}
	})

	return p.ui.PodEvents, nil
}
