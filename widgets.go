package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// FocusLossEntry is a custom Entry widget that triggers OnSubmitted when focus is lost
type FocusLossEntry struct {
	widget.Entry
}

// NewFocusLossEntry creates a new FocusLossEntry widget
func NewFocusLossEntry() *FocusLossEntry {
	e := &FocusLossEntry{}
	e.ExtendBaseWidget(e)
	return e
}

// FocusLost is called when the entry loses focus - triggers OnSubmitted
func (e *FocusLossEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.OnSubmitted != nil {
		e.OnSubmitted(e.Text)
	}
}

// HoverableCheck is a checkbox with tooltip support on hover
type HoverableCheck struct {
	widget.Check
	tooltip   string
	popUp     *widget.PopUp
	parentWin fyne.Window
}

// NewHoverableCheck creates a new HoverableCheck widget with a tooltip
func NewHoverableCheck(label string, changed func(bool), tooltip string, win fyne.Window) *HoverableCheck {
	h := &HoverableCheck{
		tooltip:   tooltip,
		parentWin: win,
	}
	h.Text = label
	h.OnChanged = changed
	h.ExtendBaseWidget(h)
	return h
}

// MouseIn is called when the mouse enters the widget - shows tooltip
func (h *HoverableCheck) MouseIn(e *desktop.MouseEvent) {
	if h.tooltip == "" || h.parentWin == nil {
		return
	}

	tooltipLabel := widget.NewLabel(h.tooltip)
	tooltipLabel.Wrapping = fyne.TextWrapOff
	tooltipContent := container.NewPadded(tooltipLabel)

	h.popUp = widget.NewPopUp(tooltipContent, h.parentWin.Canvas())
	h.popUp.ShowAtPosition(fyne.NewPos(e.AbsolutePosition.X+10, e.AbsolutePosition.Y+20))
}

// MouseMoved is called when the mouse moves within the widget
func (h *HoverableCheck) MouseMoved(_ *desktop.MouseEvent) {
	// Required to implement desktop.Hoverable interface
}

// MouseOut is called when the mouse leaves the widget - hides tooltip
func (h *HoverableCheck) MouseOut() {
	if h.popUp != nil {
		h.popUp.Hide()
		h.popUp = nil
	}
}
