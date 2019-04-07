package ui

import (
	"fmt"

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
