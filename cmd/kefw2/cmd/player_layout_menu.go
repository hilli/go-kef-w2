// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
)

func (ui *playerLayoutUI) paneMenu() bloom.Child {
	choices := ui.paneChoices()
	children := make([]bloom.Child, 0, len(choices)+7)
	children = append(children, bloom.Fixed(1, bloom.Text("PANE MENU")))
	option := func(label string, action func()) bloom.Child {
		return bloom.Fixed(1, playerHint(label).Style(playerButtonStyle(ui.focus.Is("menu-"+label), true)).
			ID("menu-"+label).Focusable().On(on.Click(func() tea.Cmd {
			action()
			return nil
		})))
	}
	children = append(children,
		option("Move (m)", func() { ui.startMode(playerLayoutMove) }),
		option("Resize (r)", func() { ui.startMode("resize") }),
		option("Zoom (z)", func() { ui.zoomPane(); ui.menu = false; ui.focus.Pop() }),
		option("Reset (R)", func() {
			ui.arrangement = playerDefaultArrangement(ui.layout)
			ui.menu = false
			ui.focus.Pop()
			ui.save(true)
		}),
		option(fmt.Sprintf("Reduced motion: %t (a)", ui.reduced), func() { ui.reduced = !ui.reduced }),
	)
	if ui.layout == playerDiscoveryBrowser {
		children = append(children, option("Dock / undock queue (d)", func() {
			ui.toggleQueueDock()
			ui.menu = false
			ui.focus.Pop()
			ui.focus.Set(ui.pane)
		}))
	}
	for i, pane := range choices {
		name := pane
		children = append(children, option(fmt.Sprintf("%d Select %s", i+1, name), func() {
			ui.pane = name
			ui.menu = false
			ui.focus.Pop()
			ui.focus.Set(name)
		}))
	}
	children = append(children, bloom.Fixed(1, playerHint("Esc closes · arrows preview · Enter commits")))
	return bloom.Column(children...)
}
