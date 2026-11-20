// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	playerBottomNowPlaying = "now-playing"
	playerBottomVolume     = "volume"
)

func (ui *playerLayoutUI) bottomFocus() string {
	id, _ := ui.focus.Current()
	switch id {
	case string(playerControlPrevious), playerFocusPlayPause, "transport-next", "seek-back", "seek-next", "controls-dismiss":
		return playerBottomNowPlaying
	case "mute", "volume", "volume-retry", "volume-dismiss":
		return playerBottomVolume
	default:
		return ""
	}
}

func (ui *playerLayoutUI) focusBottom(id string) {
	ui.search.Blur()
	ui.searchActive = false
	ui.focus.Set(id)
}

func (ui *playerLayoutUI) bottomTitle(m *playerModel, pane, label, focus string) bloom.Child {
	style := playerHintStyle.Bold(true)
	if ui.bottomFocus() == pane {
		style = style.Foreground(playerAccentColor)
	}
	if ui.drag == playerLayoutMove && ui.dragPane == pane {
		label = "MOVE " + label
	}
	return bloom.Text(":: "+label).Style(style).Truncate().ID("title-"+pane).Focusable().On(
		on.Click(func() tea.Cmd { ui.focusBottom(focus); return nil }),
		on.Drag(func(event on.Event) tea.Cmd { return ui.dragTitle(m, pane, event) }),
	)
}

func (ui *playerLayoutUI) bottomRow(now, volume bloom.Child, volumeWidth int) bloom.Child {
	nowSlot := bloom.Fill(ui.bottomFrame(playerBottomNowPlaying, now))
	volumeSlot := bloom.Fixed(volumeWidth, ui.bottomFrame(playerBottomVolume, volume))
	if ui.arrangementView().BottomOrder[0] == playerBottomVolume {
		return bloom.Row(volumeSlot, nowSlot)
	}
	return bloom.Row(nowSlot, volumeSlot)
}

func (ui *playerLayoutUI) bottomFrame(pane string, content bloom.Child) bloom.Child {
	color := playerBorderColor
	border := lipgloss.NormalBorder()
	if ui.bottomFocus() == pane {
		color = playerAccentColor
		border = lipgloss.DoubleBorder()
	}
	return bloom.Box(lipgloss.NewStyle().Border(border).BorderForeground(color), content).ID(pane + "-pane")
}

func (ui *playerLayoutUI) controlsStateForView(m *playerModel) playerControlsUIState {
	if ui.controls == nil {
		return playerControlsUIState{}
	}
	if session := ui.controls.active; session != nil {
		if session.generation != m.generation {
			return playerControlsUIState{}
		}
	} else if ui.hasControlSession && ui.controlGeneration != m.generation {
		return playerControlsUIState{}
	}
	return ui.controls.State()
}

func playerBottomVolumeWidth(width int) int {
	return min(52, max(24, width/3+6))
}

func (ui *playerLayoutUI) transportStatus(m *playerModel, status []string, state playerControlsUIState) []string {
	var problems []string
	if state.err != nil {
		problems = append(problems, "Control error: "+state.err.Error()+" (D dismiss)")
	}
	if ui.controlReason != "" {
		problems = append(problems, "Control unavailable: "+ui.controlReason)
	}
	if state.pending {
		problems = append(problems, "Pending: "+state.pendingAction)
	}
	if len(status) > 0 && strings.HasPrefix(status[0], "UNCERTAIN") {
		status = append(append([]string{status[0]}, problems...), status[1:]...)
	} else {
		status = append(problems, status...)
	}
	for i := range status {
		status[i] = watchText(status[i])
		for _, prefix := range []string{
			"UNCERTAIN", "STALE", "Events disconnected", "LAYOUT ERROR", "Control error", "Control unavailable",
		} {
			if strings.HasPrefix(status[i], prefix) {
				status[i] = ansi.Wordwrap(status[i], max(1, m.width-playerBottomVolumeWidth(m.width)-2), "")
				break
			}
		}
	}
	return status
}

func playerVolumeProblems(state playerControlsUIState, width int) []string {
	var problems []string
	if state.volumeErr != nil {
		problems = append(problems, "Volume error: "+state.volumeErr.Error())
	}
	if state.maximumErr != nil {
		problems = append(problems, "Max unknown; no increase")
	}
	for i := range problems {
		problems[i] = ansi.Wordwrap(watchText(problems[i]), max(1, width), "")
	}
	return problems
}

func (ui *playerLayoutUI) transportView(m *playerModel, status []string, state playerControlsUIState) bloom.Child {
	volumeWidth := playerBottomVolumeWidth(m.width)
	if ui.controls == nil {
		now := bloom.Column(bloom.Fixed(1, ui.bottomTitle(m, playerBottomNowPlaying, "NOW PLAYING", playerFocusPlayPause)),
			bloom.Fill(bloom.Text(strings.Join(status, "\n")).ID("transport")))
		volume := bloom.Column(bloom.Fixed(1, ui.bottomTitle(m, playerBottomVolume, "VOLUME", "volume")),
			bloom.Fill(bloom.Text("Volume unavailable")))
		return ui.bottomRow(now, volume, volumeWidth)
	}
	generation, epoch := m.generation, state.epoch
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
		return bloom.Fixed(ansi.StringWidth(label), bloom.Text(label).
			Style(playerButtonStyle(ui.focus.Is(id), reason == "")).ID(id).Focusable().On(on.Click(func() tea.Cmd {
			ui.focusBottom(id)
			if reason != "" {
				ui.controlReason = reason
				return nil
			}
			return playerScopedCmd(generation, playerMessage(message))
		})))
	}
	button := func(id, label string, message tea.Msg) bloom.Child {
		if ui.focus.Is(id) {
			label = ">" + label
		}
		return bloom.Fixed(ansi.StringWidth(label), bloom.Text(label).
			Style(playerButtonStyle(ui.focus.Is(id), true)).ID(id).Focusable().On(on.Click(func() tea.Cmd {
			ui.focusBottom(id)
			return playerScopedCmd(generation, playerMessage(message))
		})))
	}
	playLabel, seekBack, seekNext := "[Play/Pause]", "[Seek-]", "[Seek+]"
	if m.width-volumeWidth-2 < 35 {
		playLabel = "[Play]"
		seekBack, seekNext = "[<10s]", "[10s>]"
	}
	var playbackButtons []bloom.Child
	if state.err != nil {
		playbackButtons = append(playbackButtons, button("controls-dismiss", "[Dismiss]",
			playerControlDismissErrorRequest{generation: generation, epoch: epoch}))
	}
	playbackButtons = append(playbackButtons,
		control(string(playerControlPrevious), "[<<]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlPrevious}, playerControlPrevious),
		control(playerFocusPlayPause, playLabel, playerTransportRequest{generation: generation, epoch: epoch, action: playerControlPlayPause}, playerControlPlayPause),
		control("transport-next", "[>>]", playerTransportRequest{generation: generation, epoch: epoch, action: playerControlNext}, playerControlNext),
		control("seek-back", seekBack, playerTransportRequest{generation: generation, epoch: epoch, action: playerControlSeekBackward}, playerControlSeekBackward),
		control("seek-next", seekNext, playerTransportRequest{generation: generation, epoch: epoch, action: playerControlSeekForward}, playerControlSeekForward),
	)
	now := bloom.Column(bloom.Fixed(1, ui.bottomTitle(m, playerBottomNowPlaying, "NOW PLAYING", playerFocusPlayPause)),
		bloom.Fill(bloom.Text(strings.Join(status, "\n")).ID("transport")),
		bloom.Fixed(1, bloom.Row(playbackButtons...)))
	volume := ui.volumeView(m, state, volumeWidth, control, button)
	return ui.bottomRow(now, volume, volumeWidth)
}

func (ui *playerLayoutUI) volumeView(
	m *playerModel, state playerControlsUIState, paneWidth int,
	control func(string, string, tea.Msg, playerControlAction) bloom.Child,
	button func(string, string, tea.Msg) bloom.Child,
) bloom.Child {
	generation, epoch := m.generation, state.epoch
	value := state.volume
	known := state.volumeKnown
	snapshot := ui.confirmedSnapshot(m)
	if !known && snapshot != nil {
		value, known = snapshot.Data.Volume, true
	}
	if state.volumePending {
		value = state.sliderTarget
	}
	maximum := value
	if state.maximum != nil {
		maximum = min(100, max(0, *state.maximum))
	}
	width := max(1, min(50, paneWidth-2))
	filled := 0
	if maximum > 0 {
		filled = max(0, min(width, value*width/maximum))
	}
	track := ui.volumeSlider(m, state, width, filled)
	marker := " "
	if ui.focus.Is("volume") {
		marker = ">"
	}
	volume := marker + "VOL --"
	if known {
		confirmed := state.volume
		if !state.volumeKnown {
			confirmed = snapshot.Data.Volume
		}
		volume = fmt.Sprintf("%sVOL %d", marker, confirmed)
	}
	if state.maximum != nil {
		volume += fmt.Sprintf("/%d", maximum)
	}
	if (snapshot != nil && snapshot.Data.Muted) || (snapshot == nil && state.mutedKnown && state.muted) {
		volume += " MUTED"
	}
	if state.volumePending {
		volume += fmt.Sprintf(" → %d", state.sliderTarget)
	}
	problems := playerVolumeProblems(state, paneWidth-2)
	buttons := []bloom.Child{control("mute", "[Mute]",
		playerMuteRequest{generation: generation, epoch: epoch}, playerControlMute)}
	if state.volumeCanRetry {
		buttons = append(buttons, button("volume-retry", "[Retry]",
			playerVolumeRetryRequest{generation: generation, epoch: epoch}))
	}
	if state.volumeErr != nil {
		buttons = append(buttons, button("volume-dismiss", "[Dismiss]",
			playerVolumeDismissRequest{generation: generation, epoch: epoch}))
	}
	return bloom.Column(
		bloom.Fixed(1, ui.bottomTitle(m, playerBottomVolume, "VOLUME", "volume")),
		bloom.Fixed(1, bloom.Text(volume).Truncate()),
		bloom.Fixed(1, track),
		bloom.Fill(bloom.Text(strings.Join(problems, "\n"))),
		bloom.Fixed(1, bloom.Row(buttons...)),
	)
}

func (ui *playerLayoutUI) volumeSlider(m *playerModel, state playerControlsUIState, width, filled int) bloom.Child {
	generation, epoch := m.generation, state.epoch
	return bloom.Text(playerButtonStyle(false, state.ready).Render(strings.Repeat("=", filled))+
		playerHintStyle.Render(strings.Repeat("-", width-filled))).
		Style(playerSelectionStyle(ui.focus.Is("volume"), true)).
		ID("volume").Focusable().On(
		on.ClickAt(func(event on.Event) tea.Cmd {
			if ui.sliderBlocked {
				return nil
			}
			if ui.sliderSuppressClick {
				ui.sliderSuppressClick = false
				return nil
			}
			ui.focusBottom("volume")
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
			ui.focusBottom("volume")
			ui.sliderDragging = !event.Done
			if event.Done {
				ui.sliderSuppressClick = true
			}
			return playerScopedCmd(generation, playerMessage(playerVolumeSliderRequest{
				generation: generation, epoch: ui.sliderEpoch, x: event.X, width: event.W, release: event.Done,
			}))
		}),
		on.Scroll(func(_, dy int) tea.Cmd {
			ui.focusBottom("volume")
			key := "-"
			if dy < 0 {
				key = "+"
			}
			return playerScopedCmd(generation, playerMessage(playerVolumeKeyRequest{generation: generation, epoch: epoch, key: key}))
		}),
	)
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
		if choice.source == kefw2.SourceStandby {
			line = marker + "Off / Standby"
		}
		if !choice.enabled {
			line += " — unsupported or availability unknown for this model"
		}
		children = append(children, bloom.Fixed(1, bloom.Text(line).Style(playerSelectionStyle(i == ui.sourceIndex, true)).
			ID(fmt.Sprintf("source-%d", i)).Focusable().
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
