package ui

import (
	"fmt"
	"strings"
	"time"

	tview "github.com/rivo/tview"
)

type ActionBar struct {
	*tview.TextView
}

func NewActionBar() ActionBar {
	return ActionBar{tview.NewTextView().
		SetWrap(false).
		SetRegions(true).
		SetDynamicColors(true)}
}

func (a ActionBar) AddAction(number int, text string) {
	padding := ""
	if a.GetText(false) != "" {
		padding = " "
	}
	fmt.Fprintf(a.TextView, `["%d"][white:black]%s%d[black:aqua]%s[""]`, number, padding, number, text)
}

type statusState struct {
	app      *tview.Application
	spinning bool
	idx      int
	timer    *time.Timer
}

type StatusBar struct {
	*tview.TextView
	stopC chan struct{}
	ops   chan func(*statusState)
}

func NewStatusBar(app *tview.Application) StatusBar {
	textView := tview.NewTextView().
		SetTextColor(tview.Styles.SecondaryTextColor).
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

func (s StatusBar) loop(app *tview.Application) {
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
	*tview.Grid
}

func NewModal(p tview.Primitive) Modal {
	return Modal{
		tview.NewGrid().SetRows(0, 0, 0).SetColumns(0, 0, 0).
			AddItem(p, 1, 1, 1, 1, 0, 0, true),
	}
}

type ModalList struct {
	Modal

	list *tview.List
}

func NewModalList() ModalList {
	list := tview.NewList()
	return ModalList{NewModal(list), list}
}

func (m ModalList) List() *tview.List {
	return m.list
}

type ModalForm struct {
	Modal

	form *tview.Form
}

func NewModalForm() ModalForm {
	form := tview.NewForm().
		SetButtonsAlign(tview.AlignCenter).
		SetButtonBackgroundColor(tview.Styles.PrimitiveBackgroundColor).
		SetButtonTextColor(tview.Styles.PrimaryTextColor)
	form.
		SetBackgroundColor(tview.Styles.ContrastBackgroundColor).
		SetBorderPadding(0, 0, 0, 0)

	return ModalForm{NewModal(form), form}
}

func (m ModalForm) Form() *tview.Form {
	return m.form
}
