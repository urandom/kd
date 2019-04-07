package ui

import (
	"fmt"
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
	stopC chan struct{}
}

func NewStatusBar() StatusBar {
	textView := tview.NewTextView().
		SetTextColor(tview.Styles.SecondaryTextColor).
		SetWrap(false)
	textView.SetBorderPadding(0, 0, 1, 1)
	return StatusBar{TextView: textView, stopC: make(chan struct{})}
}

var spinners = [...]string{"⠋ ", "⠙ ", "⠹ ", "⠸ ", "⠼ ", "⠴ ", "⠦ ", "⠧ ", "⠇ ", "⠏ "}

func (s StatusBar) SpinText(text string, app *tview.Application) {
	s.StopSpin()

	app.QueueUpdateDraw(func() {
		s.SetText(spinners[0] + text)
	})
	go func() {
		i := 1
		for {
			select {
			case <-s.stopC:
				app.QueueUpdateDraw(func() {
					s.SetText("")
				})
				return
			case <-time.After(100 * time.Millisecond):
				spin := i % len(spinners)
				app.QueueUpdateDraw(func() {
					s.SetText(spinners[spin] + text)
				})
				i++
			}
		}
	}()
}

func (s StatusBar) StopSpin() {
	select {
	case s.stopC <- struct{}{}:
	default:
	}
}
