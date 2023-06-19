package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("Hello")

	hello := widget.NewLabel("Hello Fyne!")
	pb := theme.MediaPlayIcon()
	w.SetContent(container.New(
		layout.NewMaxLayout(),
		hello,
		widget.NewButtonWithIcon("Hi!", pb, func() {
			hello.SetText("Welcome :)")
		}),
	))

	w.ShowAndRun()
}