// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
)

func (ui *playerLayoutUI) smallView(m *playerModel, status []string) bloom.Child {
	generation := m.generation
	var epoch uint64
	if ui.controls != nil {
		epoch = ui.controls.State().epoch
	}
	mute := bloom.Text("[Mute]").ID("small-mute").On(on.Click(func() tea.Cmd {
		if ui.controls == nil || !ui.transportAllowed(playerControlMute) {
			return nil
		}
		return playerScopedCmd(generation, playerMessage(playerMuteRequest{generation: generation, epoch: epoch}))
	}))
	quit := bloom.Text("[Quit]").ID("small-quit").On(on.Click(func() tea.Cmd { return tea.Quit }))
	return bloom.Column(
		bloom.Fixed(1, bloom.Text("Terminal too small: minimum 60 × 18")),
		bloom.Fixed(1, bloom.Row(bloom.Fixed(7, mute), bloom.Fixed(7, quit), bloom.Fill(bloom.Text("m mute · q quit")))),
		bloom.Fill(bloom.Text(strings.Join(status, "\n"))),
	)
}
