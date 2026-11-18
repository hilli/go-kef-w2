// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

//nolint:gocyclo // Builds independently conditional status and controls.
func (ui *playerLayoutUI) transportView(m *playerModel, status []string, height int) bloom.Child {
	state := ui.controls.State()
	generation := m.generation
	epoch := state.epoch
	var problems []string
	if state.pending {
		if len(status) > 0 {
			status = append(status[:1], append([]string{"Pending: " + state.pendingAction}, status[1:]...)...)
		} else {
			status = append(status, "Pending: "+state.pendingAction)
		}
	}
	if state.err != nil {
		problems = append(problems, "Control error: "+state.err.Error()+" (D dismiss)")
	}
	if state.volumeErr != nil {
		problems = append(problems, "Volume error: "+state.volumeErr.Error()+" (R retry; D dismiss)")
	}
	if state.maximumErr != nil {
		problems = append(problems, "Volume maximum unavailable; increases disabled")
	}
	if ui.controlReason != "" {
		problems = append(problems, "Control unavailable: "+ui.controlReason)
	}
	if len(status) > 0 && strings.HasPrefix(status[0], "UNCERTAIN") {
		status = append(append([]string{status[0]}, problems...), status[1:]...)
	} else {
		status = append(problems, status...)
	}
	for i := range status {
		status[i] = watchText(status[i])
	}
	control := func(id, label string, message tea.Msg, action playerControlAction) bloom.Child {
		reason := ""
		if !state.ready || state.confirmed == nil {
			reason = "speaker status unavailable"
		} else if err := playerControlUnavailable(*state.confirmed, action); err != nil {
			reason = err.Error()
		}
		if reason != "" {
			label = strings.TrimSuffix(label, "]") + "×]"
		}
		if ui.focus.Is(id) {
			label = ">" + label
		}
		return bloom.Fixed(ansi.StringWidth(label), bloom.Text(label).ID(id).Focusable().On(on.Click(func() tea.Cmd {
			if reason != "" {
				ui.controlReason = reason
				return nil
			}
			return playerScopedCmd(generation, playerMessage(message))
		})))
	}
	buttons := []bloom.Child{
		control("previous", "[<<]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlPrevious}, playerControlPrevious),
		control("play-pause", "[Play/Pause]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlPlayPause}, playerControlPlayPause),
		control("transport-next", "[>>]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlNext}, playerControlNext),
		control("mute", "[Mute]", playerMuteRequest{generation: generation, epoch: epoch}, playerControlMute),
		control("seek-back", "[Seek-]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlSeekBackward}, playerControlSeekBackward),
		control("seek-next", "[Seek+]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlSeekForward}, playerControlSeekForward),
	}
	var priority []bloom.Child
	if state.volumeCanRetry {
		priority = append(priority, bloom.Fixed(11, bloom.Text("[Retry vol]").ID("volume-retry").On(on.Click(func() tea.Cmd {
			return playerScopedCmd(generation, playerMessage(playerVolumeRetryRequest{generation: generation, epoch: epoch}))
		}))))
	}
	if state.volumeErr != nil || state.err != nil {
		priority = append(priority, bloom.Fixed(9, bloom.Text("[Dismiss]").ID("controls-dismiss").On(on.Click(func() tea.Cmd {
			return tea.Batch(
				playerScopedCmd(generation, playerMessage(playerVolumeDismissRequest{generation: generation, epoch: epoch})),
				playerScopedCmd(generation, playerMessage(playerControlDismissErrorRequest{generation: generation, epoch: epoch})),
			)
		}))))
	}
	buttons = append(priority, buttons...)
	value := state.volume
	if state.volumePending {
		value = state.sliderTarget
	}
	maximum := state.volume
	if state.maximum != nil {
		maximum = min(100, max(0, *state.maximum))
	}
	width := max(1, min(50, m.width-26))
	filled := 0
	if maximum > 0 {
		filled = max(0, min(width, value*width/maximum))
	}
	marker := " "
	if ui.focus.Is("volume") {
		marker = ">"
	}
	track := bloom.Text(strings.Repeat("=", filled)+strings.Repeat("-", width-filled)).
		ID("volume").Focusable().On(
		on.ClickAt(func(event on.Event) tea.Cmd {
			if ui.sliderBlocked {
				return nil
			}
			if ui.sliderSuppressClick {
				ui.sliderSuppressClick = false
				return nil
			}
			ui.focus.Set("volume")
			return playerScopedCmd(generation, playerMessage(playerVolumeSliderRequest{
				generation: generation, epoch: epoch, x: event.X, width: event.W, release: true,
			}))
		}),
		on.Drag(func(event on.Event) tea.Cmd {
			if event.Msg == nil || m.chooserOpen || ui.sourceOpen || ui.menu || ui.sliderBlocked {
				ui.sliderDragging = false
				return nil
			}
			if event.Done {
				if _, ok := event.Msg.(tea.MouseReleaseMsg); !ok {
					ui.sliderDragging = false
					return nil
				}
			}
			if !ui.sliderDragging {
				ui.sliderEpoch = epoch
			}
			ui.focus.Set("volume")
			ui.sliderDragging = !event.Done
			if event.Done {
				ui.sliderSuppressClick = true
			}
			return playerScopedCmd(generation, playerMessage(playerVolumeSliderRequest{
				generation: generation, epoch: ui.sliderEpoch, x: event.X, width: event.W, release: event.Done,
			}))
		}),
		on.Scroll(func(_, dy int) tea.Cmd {
			key := "-"
			if dy < 0 {
				key = "+"
			}
			return playerScopedCmd(generation, playerMessage(playerVolumeKeyRequest{generation: generation, epoch: epoch, key: key}))
		}),
	)
	volume := marker + "VOL --"
	if state.volumeKnown {
		volume = fmt.Sprintf("%sVOL %d", marker, state.volume)
	}
	if state.volumePending {
		volume += fmt.Sprintf(" → %d", state.sliderTarget)
	}
	if state.maximum != nil {
		volume += fmt.Sprintf("/%d", maximum)
	}
	slider := bloom.Row(bloom.Fixed(ansi.StringWidth(volume)+1, bloom.Text(volume+" ")), bloom.Fixed(width, track), bloom.Space())
	return bloom.Column(bloom.Fixed(max(1, height-2), bloom.Text(strings.Join(status, "\n"))),
		bloom.Fixed(1, bloom.Row(buttons...)), bloom.Fixed(1, slider))
}

func (ui *playerLayoutUI) sourceChooser(m *playerModel) bloom.Child {
	generation := m.generation
	epoch := ui.sourceEpoch
	lines := []string{"Choose input"}
	if m.target != nil {
		lines = playerIdentityLines("Choose input for ", *m.target, max(1, m.width-4), "")
	}
	var children []bloom.Child
	for _, line := range lines {
		children = append(children, bloom.Fixed(1, bloom.Text(line)))
	}
	ready := ui.controls.State().ready
	if !ready {
		children = append(children, bloom.Fixed(1, bloom.Text("Writes disabled: awaiting fresh speaker status")))
	}
	for index, choice := range ui.sources() {
		i := index
		marker := "  "
		if i == ui.sourceIndex {
			marker = "> "
		}
		line := marker + string(choice.source)
		if !choice.enabled {
			line += " — unsupported or availability unknown for this model"
		}
		children = append(children, bloom.Fixed(1, bloom.Text(line).ID(fmt.Sprintf("source-%d", i)).Focusable().
			On(on.Click(func() tea.Cmd { ui.sourceIndex = i; return nil }))))
	}
	children = append(children, bloom.Fixed(1, bloom.Text("[Switch]   Enter switch · Esc cancel").
		ID("source-switch").Focusable().On(on.Click(func() tea.Cmd {
		choices := ui.sources()
		if !ready || ui.sourceIndex < 0 || ui.sourceIndex >= len(choices) || !choices[ui.sourceIndex].enabled {
			return nil
		}
		source := choices[ui.sourceIndex].source
		ui.sourceOpen = false
		ui.focus.Pop()
		return playerScopedCmd(generation, playerMessage(playerSourceRequest{generation: generation, epoch: epoch, source: source}))
	}))))
	return bloom.Column(children...)
}
