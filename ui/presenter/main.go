package presenter

import (
	"fmt"
	"log"

	"github.com/rivo/tview"
	"github.com/urandom/kd/ext"
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

type Modal struct {
	ui *ui.UI

	isModalVisible bool
	focused        tview.Primitive
}

func newModal(ui *ui.UI) *Modal {
	return &Modal{ui: ui}
}

func (p *Modal) display(prim tview.Primitive) {
	p.ui.App.QueueUpdateDraw(func() {
		p.isModalVisible = true
		p.focused = p.ui.App.GetFocus()

		p.ui.Pages.AddPage(ui.PageModal, prim, true, false)
		p.ui.Pages.ShowPage(ui.PageModal)
		p.ui.App.SetFocus(prim)
	})
}

func (p *Modal) Close() {
	p.ui.App.QueueUpdateDraw(func() {
		if p.isModalVisible {
			p.ui.Pages.RemovePage(ui.PageModal)
			p.isModalVisible = false
			p.ui.App.SetFocus(p.focused)
		}
	})
}

type Error struct {
	*Modal
}

const (
	buttonQuit    = "Quit"
	buttonClose   = "Close"
	buttonConfirm = "Confirm"
	buttonOk      = "Ok"
	buttonRetry   = "Retry"
)

func NewError(ui *ui.UI) Error {
	return Error{Modal: newModal(ui)}
}

func (p Error) DisplayError(err error) bool {
	if err == nil {
		p.Close()
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

	errorModal := tview.NewModal()
	errorModal.SetTitle("Error")
	errorModal.
		SetText(fmt.Sprintf("Error: %s", err)).
		AddButtons(buttons).
		SetDoneFunc(func(idx int, label string) {
			switch label {
			case buttonQuit:
				p.ui.App.Stop()
			case buttonRetry:
				go func() {
					p.DisplayError(err.(UserRetryableError).RetryOp())
				}()
			}
			go p.Close()
		})
	p.display(errorModal)

	return true
}

type Confirm struct {
	*Modal
}

func NewConfirm(ui *ui.UI) Confirm {
	return Confirm{Modal: newModal(ui)}
}

func (p Confirm) DisplayConfirm(title, message string) <-chan bool {
	buttons := []string{buttonClose, buttonConfirm}

	modal := tview.NewModal()
	modal.SetTitle(title)
	ch := make(chan bool)
	modal.
		SetText(message).
		AddButtons(buttons).
		SetDoneFunc(func(idx int, label string) {
			ch <- label == buttonConfirm
			go p.Close()
		})
	p.display(modal)

	return ch
}

type Picker struct {
	*Modal
}

func NewPicker(ui *ui.UI) Picker {
	return Picker{Modal: newModal(ui)}
}

func (p Picker) PickFrom(title string, items []string) <-chan string {
	choice := make(chan string)

	picker := ui.NewModalList()
	picker.List().SetBackgroundColor(tview.Styles.ContrastBackgroundColor).SetBorder(true)

	list := picker.List().SetSelectedFunc(
		func(idx int, main, sec string, sk rune) {
			choice <- main
			close(choice)
			go p.Close()
		})

	for i := range items {
		list.AddItem(items[i], "", rune(97+i), nil)
	}

	list.SetTitle(title)
	p.display(picker)

	return choice
}

type Form struct {
	*Modal
}

func NewForm(ui *ui.UI) Form {
	return Form{Modal: newModal(ui)}
}

func (p Form) DisplayForm(populate func(*tview.Form)) {
	form := ui.NewModalForm()
	populate(form.Form())

	p.display(form)
}

type Main struct {
	Error

	clientFactory ClientFactory
	extManager    ext.Manager

	client k8s.Client
	Pods   *Pods
}

func NewMain(ui *ui.UI, extManager ext.Manager, clientFactory ClientFactory) *Main {
	return &Main{
		Error:         NewError(ui),
		clientFactory: clientFactory,
		extManager:    extManager,
	}
}

func (p *Main) Run() error {
	go func() {
		if !p.DisplayError(p.initClient()) {
			p.Pods = NewPods(p.ui, p.client, p.extManager)
			p.Pods.initKeybindings()
			p.DisplayError(p.Pods.populateNamespaces())
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
