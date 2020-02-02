package event

import "github.com/gdamore/tcell"

type callbackFn func(event *tcell.EventKey) *tcell.EventKey

type callback struct {
	id string
	fn callbackFn
}

type Input struct {
	callbacks []callback
}

func NewInput() *Input {
	return &Input{}
}

func (i *Input) Add(id string, fn callbackFn) {
	i.Remove(id)
	i.callbacks = append(i.callbacks, callback{id: id, fn: fn})
}

func (i *Input) Remove(id string) {
	for idx, c := range i.callbacks {
		if c.id == id {
			i.callbacks = append(i.callbacks[:idx], i.callbacks[idx+1:]...)
			return
		}
	}
}

func (i *Input) GetInputCapture() callbackFn {
	return func(event *tcell.EventKey) *tcell.EventKey {
		for _, c := range i.callbacks {
			event = c.fn(event)
			if event == nil {
				break
			}
		}

		return event
	}
}
