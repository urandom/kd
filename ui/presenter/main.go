package presenter

import (
	"fmt"
	"log"
	"strconv"

	"github.com/rivo/tview"
	"github.com/urandom/kd/k8s"
	"github.com/urandom/kd/ui"
	"golang.org/x/xerrors"
)

type FatalError struct {
	error
}

type UserRetryableError struct {
	error
	RetryOp func() error
}

type ClientFactory func() (k8s.Client, error)

type Error struct {
	ui *ui.UI

	isModalVisible bool
	focused        tview.Primitive
}

const (
	buttonQuit    = "Quit"
	buttonClose   = "Close"
	buttonRetry   = "Retry"
	buttonRefresh = "Refre"
	buttonEmpty   = "      "
)

func (p *Error) displayError(err error) bool {
	if err == nil {
		if p.isModalVisible {
			p.ui.App.QueueUpdateDraw(func() {
				p.ui.Pages.HidePage(ui.PageK8sError)
			})
		}
		return false
	}

	var buttons []string

	if xerrors.As(err, &FatalError{}) {
		buttons = append(buttons, buttonQuit)
	} else {
		buttons = append(buttons, buttonClose)
	}

	if xerrors.As(err, &UserRetryableError{}) {
		buttons = append(buttons, buttonRetry)
	}

	p.ui.App.QueueUpdateDraw(func() {
		p.ui.ErrorModal.
			SetText(fmt.Sprintf("Error: %s", err)).
			//ClearButtons().
			AddButtons(buttons).
			SetDoneFunc(func(idx int, label string) {
				switch label {
				case buttonQuit:
					p.ui.App.Stop()
				case buttonRetry:
					go func() {
						p.displayError(err.(UserRetryableError).RetryOp())
					}()
					fallthrough
				case buttonClose:
					p.isModalVisible = false
					p.ui.Pages.HidePage(ui.PageK8sError)
					p.ui.App.SetFocus(p.focused)
				}
			})
		p.isModalVisible = true
		p.focused = p.ui.App.GetFocus()
		p.ui.Pages.ShowPage(ui.PageK8sError)
		p.ui.App.SetFocus(p.ui.ErrorModal)
	})

	return true
}

type Picker struct {
	ui *ui.UI

	isModalVisible bool
	focused        tview.Primitive
}

func (p *Picker) PickFrom(title string, items []string) <-chan string {
	choice := make(chan string)

	p.ui.App.QueueUpdateDraw(func() {
		list := p.ui.Picker.List().Clear().SetSelectedFunc(
			func(idx int, main, sec string, sk rune) {
				choice <- main
				close(choice)
				p.ui.Pages.HidePage(ui.PagePicker)
				p.ui.App.SetFocus(p.focused)
			})
		for i := range items {
			list.AddItem(items[i], "", rune(strconv.Itoa(i)[0]), nil)
		}

		list.SetTitle(title)

		p.isModalVisible = true
		p.focused = p.ui.App.GetFocus()
		p.ui.Pages.ShowPage(ui.PagePicker)
		p.ui.App.SetFocus(p.ui.Picker)
	})

	return choice
}

type Main struct {
	*Error

	clientFactory ClientFactory

	client k8s.Client
	Pods   *Pods
}

func NewMain(ui *ui.UI, clientFactory ClientFactory) *Main {
	return &Main{
		Error:         &Error{ui: ui},
		clientFactory: clientFactory,
	}
}

func (p *Main) Run() error {
	go func() {
		if !p.displayError(p.initClient()) {
			p.Pods = NewPods(p.ui, p.client)
			p.Pods.initKeybindings()
			p.displayError(p.Pods.populateNamespaces())
		}
	}()

	return p.ui.App.Run()
}

func (p *Main) initClient() error {
	var err error
	log.Println("Creating k8s client")
	if p.client, err = p.clientFactory(); err != nil {
		log.Println("Error creating k8s client:", err)

		return FatalError{err}
	}

	return nil
}
