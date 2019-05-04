package ui

import (
	"fmt"
	"log"
	"time"

	"github.com/rivo/tview"
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

type StatusBar struct {
	*tview.TextView
	app   *tview.Application
	stopC chan struct{}
}

func NewStatusBar(app *tview.Application) StatusBar {
	textView := tview.NewTextView().
		SetTextColor(tview.Styles.SecondaryTextColor).
		SetWrap(false)
	textView.SetBorderPadding(0, 0, 1, 1)
	return StatusBar{TextView: textView, app: app, stopC: make(chan struct{})}
}

var spinners = [...]string{"⠋ ", "⠙ ", "⠹ ", "⠸ ", "⠼ ", "⠴ ", "⠦ ", "⠧ ", "⠇ ", "⠏ "}

func (s StatusBar) SpinText(text string) {
	s.StopSpin()

	s.app.QueueUpdateDraw(func() {
		s.SetText(spinners[0] + text)
	})
	log.Println("0")
	go func() {
		i := 1
		for {
			select {
			case <-s.stopC:
				s.app.QueueUpdateDraw(func() {
					log.Println("3")
					s.SetText("")
				})
				log.Println("4")
				return
			case <-time.After(100 * time.Millisecond):
				spin := i % len(spinners)
				s.app.QueueUpdateDraw(func() {
					s.SetText(spinners[spin] + text)
				})
				i++
			}
		}
	}()
}

func (s StatusBar) StopSpin() {
	log.Println("2")
	select {
	case s.stopC <- struct{}{}:
		log.Println("!!!!!!!!!!!!")
	default:
		log.Println("#########")
	}
}

func (s StatusBar) ShowTextFor(text string, d time.Duration, app *tview.Application) {
	time.AfterFunc(250*time.Microsecond, func() {
		app.QueueUpdateDraw(func() {
			log.Println("1")
			s.SetText(text)
		})
	})

	time.AfterFunc(d, func() {
		app.QueueUpdateDraw(func() {
			if s.GetText(false) == text {
				s.Clear()
			}
		})
	})
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
