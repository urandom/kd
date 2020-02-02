package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell"
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
	a := &ActionBar{cview.NewTextView().
		SetWrap(false).
		SetRegions(true).
		SetDynamicColors(true), input, nil}

	a.SetMouseCapture(func(e *cview.EventMouse) *cview.EventMouse {
		if e.Action()&cview.MouseDown == 0 || e.Buttons()&tcell.Button1 == 0 {
			return e
		}
		x, _ := e.Position()
		for _, item := range a.items {
			if x >= item.start && x < item.start+item.len {
				item.fn()
			}
		}
		return nil
	})
	return a
}

func (a *ActionBar) AddAction(number int, text string, ev *tcell.EventKey, fn func() bool) {
	padding := ""
	if a.GetText(false) != "" {
		padding = " "
	}
	a.items = append(a.items, item{fn, number, len(a.GetText(true)), len(text)})
	fmt.Fprintf(a.TextView, `["%d"][white:black]%s%d[black:aqua]%s[""]`, number, padding, number, text)
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
	textView := cview.NewTextView().
		SetTextColor(cview.Styles.SecondaryTextColor).
		SetWrap(false)
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
	return Modal{
		cview.NewGrid().SetRows(0, 0, 0).SetColumns(0, 0, 0).
			AddItem(p, 1, 1, 1, 1, 0, 0, true),
	}
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

type ModalForm struct {
	Modal

	form *cview.Form
}

func NewModalForm() ModalForm {
	form := cview.NewForm().
		SetButtonsAlign(cview.AlignCenter).
		SetButtonBackgroundColor(cview.Styles.PrimitiveBackgroundColor).
		SetButtonTextColor(cview.Styles.PrimaryTextColor)
	form.
		SetBackgroundColor(cview.Styles.ContrastBackgroundColor).
		SetBorderPadding(0, 0, 0, 0)

	return ModalForm{NewModal(form), form}
}

func (m ModalForm) Form() *cview.Form {
	return m.form
}
