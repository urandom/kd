package ui

import (
	"fmt"
	"log"

	"github.com/urandom/kd/k8s"
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

type MainPresenter struct {
	clientFactory ClientFactory
	ui            *UI

	client        k8s.Client
	podsPresenter *PodsPresenter
}

func NewMainPresenter(ui *UI, clientFactory ClientFactory) *MainPresenter {
	return &MainPresenter{clientFactory: clientFactory, ui: ui}
}

func (p *MainPresenter) Run() error {
	go func() {
		if err := p.initClient(); err == nil {
			p.podsPresenter = NewPodsPresenter(p.ui, p.client)
			if err := p.podsPresenter.populateNamespaces(); err != nil {
				p.displayError(err)
			}
		} else {
			p.displayError(err)
		}
	}()

	return p.ui.app.Run()
}

const (
	buttonQuit  = "Quit"
	buttonClose = "Close"
	buttonRetry = "Retry"
)

func (p *MainPresenter) displayError(err error) {
	var buttons []string

	if xerrors.As(err, &FatalError{}) {
		buttons = append(buttons, buttonQuit)
	} else {
		buttons = append(buttons, buttonClose)
	}

	if xerrors.As(err, &UserRetryableError{}) {
		buttons = append(buttons, buttonRetry)
	}

	p.ui.app.QueueUpdate(func() {
		p.ui.errorModal.
			SetText(fmt.Sprintf("Error creating k8s client: %s", err)).
			ClearButtons().
			AddButtons(buttons).
			SetDoneFunc(func(idx int, label string) {
				switch label {
				case buttonQuit:
					p.ui.app.Stop()
				case buttonClose:
					p.ui.pages.HidePage(pageK8sError)
				case buttonRetry:
					p.ui.pages.HidePage(pageK8sError)
					go func() {
						if err := err.(UserRetryableError).RetryOp(); err != nil {
							p.displayError(err)
						}
					}()
				}
			})
		p.ui.pages.ShowPage(pageK8sError)
		p.ui.app.SetFocus(p.ui.errorModal)
		p.ui.app.Draw()
	})
}

func (p *MainPresenter) initClient() error {
	var err error
	log.Println("Creating k8s client")
	if p.client, err = p.clientFactory(); err != nil {
		log.Println("Error creating k8s client:", err)

		return FatalError{err}
	}

	return nil
}

type PodsPresenter struct {
	ui     *UI
	client k8s.Client
}

func NewPodsPresenter(ui *UI, client k8s.Client) *PodsPresenter {
	return &PodsPresenter{ui: ui, client: client}
}

func (p *PodsPresenter) populateNamespaces() error {
	log.Println("Getting cluster namespaces")
	p.ui.app.QueueUpdate(func() {
		p.ui.pages.ShowPage(pageLoading)
	})
	if namespaces, err := p.client.Namespaces(); err == nil {
		p.ui.app.QueueUpdate(func() {
			p.ui.namespaceDropDown.SetOptions(namespaces, func(text string, idx int) {
				log.Println(text, idx)
			})
			p.ui.namespaceDropDown.SetCurrentOption(0)
			p.ui.pages.SwitchToPage(pagePods)
			p.ui.app.SetFocus(p.ui.namespaceDropDown)
			p.ui.app.Draw()
		})

		return nil
	} else {
		log.Println("Error getting cluster namespaces:", err)
		return UserRetryableError{err, p.populateNamespaces}
	}
}
