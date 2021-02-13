package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/urandom/kd/ui/event"
	"gitlab.com/tslocum/cview"
)

type item struct {
	fn                 func() bool
	number, start, len int
}

type ActionBar struct {
	*cview.TextView

	input *event.Input
	items []item
}

func NewActionBar(input *event.Input) *ActionBar {
	textView := cview.NewTextView()
	textView.SetWrap(false)
	textView.SetRegions(true)
	textView.SetDynamicColors(true)

	a := &ActionBar{textView, input, nil}

	a.SetMouseCapture(func(action cview.MouseAction, e *tcell.EventMouse) (cview.MouseAction, *tcell.EventMouse) {
		if action&cview.MouseLeftDown == 0 || e.Buttons()&tcell.Button1 == 0 || !a.InRect(e.Position()) {
			return action, e
		}
		x, _, _, _ := a.GetInnerRect()
		evx, _ := e.Position()
		for _, item := range a.items {
			if evx-x >= item.start && evx-x < item.start+item.len {
				item.fn()
			}
		}
		return action, nil
	})
	return a
}

func (a *ActionBar) AddAction(number int, text string, ev *tcell.EventKey, fn func() bool) {
	padding := ""
	if a.GetText(false) != "" {
		padding = " "
	}
	numberStr := strconv.Itoa(number)
	a.items = append(a.items, item{fn, number, len(a.GetText(true)), len(text) + len(padding) + len(numberStr)})
	fmt.Fprintf(a.TextView, `["%s"][white:black]%s%d[black:aqua]%s[""]`, numberStr, padding, number, text)
	a.input.Add(generateId(number), func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == ev.Key() && event.Modifiers() == ev.Modifiers() {
			if fn() {
				return nil
			}
		}
		return event
	})
}

func (a *ActionBar) Clear() *ActionBar {
	a.TextView.Clear()
	for _, item := range a.items {
		a.input.Remove(generateId(item.number))
	}
	a.items = nil
	return a
}

func generateId(number int) string {
	return "action-" + strconv.Itoa(number)
}

type statusState struct {
	app      *cview.Application
	spinning bool
	idx      int
	timer    *time.Timer
}

type StatusBar struct {
	*cview.TextView
	stopC chan struct{}
	ops   chan func(*statusState)
}

func NewStatusBar(app *cview.Application) StatusBar {
	textView := cview.NewTextView()
	textView.SetTextColor(cview.Styles.SecondaryTextColor)
	textView.SetWrap(false)
	textView.SetBorderPadding(0, 0, 1, 1)

	s := StatusBar{TextView: textView, stopC: make(chan struct{}), ops: make(chan func(*statusState))}

	go s.loop(app)

	return s
}

var spinners = [...]string{"⠋ ", "⠙ ", "⠹ ", "⠸ ", "⠼ ", "⠴ ", "⠦ ", "⠧ ", "⠇ ", "⠏ "}

func (s StatusBar) SpinText(text string) {
	s.StopSpin()

	s.ops <- func(state *statusState) {
		state.idx = 1
		state.spinning = true
		state.app.QueueUpdateDraw(func() {
			s.TextView.SetText(spinners[0] + text)
		})
	}

	s.spin(text)
}

func (s StatusBar) spin(text string) {
	s.ops <- func(state *statusState) {
		state.timer = time.AfterFunc(100*time.Millisecond, func() {
			go func() {
				s.ops <- func(state *statusState) {
					if !state.spinning {
						return
					}

					spin := state.idx % len(spinners)
					state.idx++

					state.app.QueueUpdateDraw(func() {
						s.TextView.SetText(spinners[spin] + text)
					})

					go s.spin(text)
				}
			}()
		})
	}

}

func (s StatusBar) SetText(text string) {
	s.ops <- func(state *statusState) {
		state.app.QueueUpdateDraw(func() {
			s.TextView.SetText(text)
		})
	}
}

func (s StatusBar) Clear() {
	s.ops <- func(state *statusState) {
		state.app.QueueUpdateDraw(func() {
			s.TextView.Clear()
		})
	}
}

func (s StatusBar) StopSpin() {
	s.ops <- func(state *statusState) {
		if state.timer != nil {
			state.timer.Stop()
			state.timer = nil
		}

		if state.spinning {
			state.spinning = false
			state.app.QueueUpdateDraw(func() {
				s.TextView.SetText("")
			})
		}
	}
}

func (s StatusBar) ShowTextFor(text string, d time.Duration) {
	text = strings.TrimSpace(text)
	s.SetText(text)

	time.AfterFunc(d, func() {
		s.ops <- func(state *statusState) {
			state.app.QueueUpdateDraw(func() {
				if strings.TrimSpace(s.GetText(false)) == text {
					s.TextView.Clear()
				}
			})
		}
	})
}

func (s StatusBar) loop(app *cview.Application) {
	state := statusState{
		app: app,
	}

	for {
		select {
		case op := <-s.ops:
			op(&state)
		}
	}
}

type Modal struct {
	*cview.Grid
}

func NewModal(p cview.Primitive) Modal {
	grid := cview.NewGrid()
	grid.SetRows(0, 0, 0)
	grid.SetColumns(0, 0, 0)
	grid.AddItem(p, 1, 1, 1, 1, 0, 0, true)
	return Modal{grid}
}

type ModalList struct {
	Modal

	list *cview.List
}

func NewModalList() ModalList {
	list := cview.NewList()
	return ModalList{NewModal(list), list}
}

func (m ModalList) List() *cview.List {
	return m.list
}
