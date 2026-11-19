// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"
	"time"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

func (ui *playerLayoutUI) confirmedSnapshot(m *playerModel) *statusSnapshot {
	if ui.controls == nil {
		return m.snapshot
	}
	matches := ui.hasControlSession && ui.controlGeneration == m.generation
	if session := ui.controls.active; session != nil {
		matches = session.generation == m.generation
	}
	if matches {
		if snapshot := ui.controls.State().confirmed; snapshot != nil {
			return snapshot
		}
	}
	return m.snapshot
}

func playerModal(content bloom.Child) bloom.Child {
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(playerAccentColor).Background(playerSurfaceColor).Padding(0, 1)
	return bloom.Transition(1, bloom.Box(style, content),
		playerSurfaceColors{foreground: playerTextColor, background: playerSurfaceColor})
}

//nolint:gocyclo // Composes the independent overlays and status states.
func (ui *playerLayoutUI) view(m *playerModel) tea.View {
	if m.width < 1 || m.height < 1 {
		return tea.View{AltScreen: true}
	}
	header := "KEF PLAYER"
	var headerExtra []string
	if m.target != nil {
		header = playerSpeakerHeader(m)
	}
	if ui.focus.Is(playerFocusSpeaker) {
		header = "> " + header
	}
	if m.target != nil {
		connection := " | " + string(m.connection)
		width := max(0, m.width-24-ansi.StringWidth(connection))
		if ansi.StringWidth(header) > width {
			target := *m.target
			if m.speakerInfo != nil && m.speakerInfo.Name != "" {
				target.Name = m.speakerInfo.Name
			}
			prefix := "Speaker: "
			if ui.focus.Is(playerFocusSpeaker) {
				prefix = "> " + prefix
			}
			tail := ""
			if !target.Configured {
				tail = " [session-only]"
			}
			identity := playerIdentityLines(prefix, target, width, tail)
			header = identity[0]
			if len(identity) > 1 {
				headerExtra = playerAddressLines(watchText(target.IPAddress), m.width)
			}
		}
		header += connection
	}
	speaker := bloom.Text(header).Style(playerButtonStyle(ui.focus.Is(playerFocusSpeaker), true)).
		ID(playerFocusSpeaker).Focusable().On(on.Click(func() tea.Cmd {
		ui.cancel()
		return func() tea.Msg { return playerOpenChooserMsg{} }
	}))
	headerRow := bloom.Row(bloom.Fill(speaker),
		bloom.Fixed(8, bloom.Text("[Queue]").Style(playerButtonStyle(ui.focus.Is("queue-open"), true)).ID("queue-open").Focusable().On(on.Click(func() tea.Cmd {
			ui.toggleQueue()
			return nil
		}))),
		bloom.Fixed(9, bloom.Text("[Details]").Style(playerButtonStyle(ui.focus.Is("details-open"), true)).ID("details-open").Focusable().On(on.Click(func() tea.Cmd {
			ui.pane = playerPaneDetails
			ui.focus.Set(ui.pane)
			return nil
		}))),
		bloom.Fixed(7, bloom.Text("[Panes]").Style(playerButtonStyle(ui.focus.Is("panes-open"), true)).ID("panes-open").Focusable().On(on.Click(func() tea.Cmd {
			ui.cancelPointerInteractions()
			ui.menu = true
			ui.focus.Push("pane-menu")
			return nil
		}))),
	)
	tooSmall := m.width < 60 || m.height < 18
	var body bloom.Child
	if tooSmall {
		body = bloom.Text("Terminal too small: minimum 60 × 18. q quit · m mute · S speaker")
	} else {
		body = ui.panes(m)
	}
	var status []string
	snapshot := ui.confirmedSnapshot(m)
	if snapshot != nil {
		source := string(snapshot.Data.Source)
		switch snapshot.Data.Source {
		case kefw2.SourceTV, kefw2.SourceOptical, kefw2.SourceCoaxial, kefw2.SourceAux, kefw2.SourceUSB:
			if snapshot.Data.Playback == nil || snapshot.Data.Playback.Track == nil {
				source += " / external audio"
			}
		}
		power := string(snapshot.Data.Power)
		if power == "" {
			power = "unknown"
		}
		status = append(status, fmt.Sprintf("INPUT %s | POWER %s", source, power))
		var playback, progress, track string
		var metadata []string
		for _, line := range appendWatchPlayback(nil, snapshot) {
			switch {
			case strings.HasPrefix(line, "Playback: "):
				playback = line
			case strings.HasPrefix(line, "Progress: "):
				progress = line
			case strings.HasPrefix(line, "Track: "):
				track = line
			default:
				metadata = append(metadata, line)
			}
		}
		if track != "" {
			status = append(status, track)
		}
		switch {
		case playback != "" && progress != "":
			status = append(status, playback+" | "+progress)
		case playback != "":
			status = append(status, playback)
		case progress != "":
			status = append(status, progress)
		}
		status = append(status, metadata...)
	} else {
		status = append(status, "Waiting for speaker status")
	}
	if m.readErr != nil {
		age := ""
		if !m.lastSuccess.IsZero() {
			age = fmt.Sprintf(" (%s old)", time.Since(m.lastSuccess).Round(time.Second))
		}
		status = append([]string{"STALE" + age + ": " + m.readErr.Error()}, status...)
	}
	if m.eventErr != nil {
		status = append([]string{"Events disconnected; polling: " + m.eventErr.Error()}, status...)
	}
	if ui.error != nil {
		detail := " | p then R to reset"
		if ui.invalid {
			detail = " | p then R replaces the invalid layout file"
		}
		status = append([]string{"LAYOUT ERROR: " + ui.error.Error() + detail}, status...)
	}
	if len(ui.notices) > 0 {
		notice := ui.notices[0]
		line := fmt.Sprintf("UNCERTAIN %s %s: %s: %v | Esc dismiss (%d)",
			notice.target.IPAddress, notice.target.Name, notice.action, notice.err, len(ui.notices))
		status = append([]string{line}, status...)
	}
	for i := range status {
		status[i] = watchText(status[i])
	}
	state := ui.controlsStateForView(m)
	dockStatus := ui.transportStatus(m, status, state)
	statusRows := 0
	for _, line := range dockStatus {
		statusRows += strings.Count(line, "\n") + 1
	}
	volumeRows := 0
	for _, line := range playerVolumeProblems(state, playerBottomVolumeWidth(m.width)-2) {
		volumeRows += strings.Count(line, "\n") + 1
	}
	maxDockHeight := max(7, m.height/3)
	if state.err != nil || state.volumeErr != nil || state.maximumErr != nil || ui.controlReason != "" {
		maxDockHeight = max(maxDockHeight, m.height/2)
	}
	dockHeight := max(6, min(maxDockHeight, max(statusRows+2, volumeRows+4)+2))
	dock := ui.transportView(m, dockStatus, state)
	help := playerHint("Tab panes · / search · S speaker · s input · Q queue · p panes · ? help · q quit").
		ID("help-open").Focusable().On(on.Click(func() tea.Cmd {
		ui.cancelPointerInteractions()
		ui.help = true
		ui.focus.Push("help")
		return nil
	}))
	children := []bloom.Child{bloom.Fixed(1, headerRow)}
	for _, line := range headerExtra {
		children = append(children, bloom.Fixed(1, bloom.Text(line)))
	}
	if ui.layout == playerDiscoveryBrowser && ui.discovery != nil {
		children = append(children, bloom.Fixed(1, ui.discoverySearch()))
	}
	children = append(children, bloom.Fill(body), bloom.Fixed(dockHeight, dock), bloom.Fixed(1, help))
	var tree bloom.Child = bloom.Column(children...)
	if tooSmall {
		tree = ui.smallView(m, status)
	}
	content := tree
	if ui.help {
		text := "PLAYER HELP\nTab / Shift+Tab: focus · Alt+1/2/3: panes\n" +
			"S: speaker · s: input · /: search · Q: queue · i: details\n" +
			"+/-: volume · m: mute · Space: play/pause · [ ]: previous/next · , .: seek\n" +
			"p: pane menu · z: zoom · arrows: navigate · Esc: cancel · q: quit"
		content = bloom.Overlay(content, bloom.Layer(playerModal(bloom.Text(text))).ID("help").Modal().
			Anchor(bloom.Center, bloom.Center))
	}
	if ui.menu {
		menu := ui.paneMenu()
		content = bloom.Overlay(content, bloom.Layer(playerModal(menu)).ID("pane-menu").Modal().
			Anchor(bloom.Center, bloom.Center))
	}
	if ui.sourceOpen && ui.controls != nil && ui.sourceEpoch == ui.controls.State().epoch {
		content = bloom.Overlay(content, bloom.Layer(playerModal(ui.sourceChooser(m))).ID("source-chooser").Modal().
			Anchor(bloom.Center, bloom.Center))
	}
	if ui.queueClear != nil && (ui.queue == nil || ui.queue.state().Ready) {
		lines := []string{"Clear the active queue?"}
		if ui.queue != nil {
			state := ui.queue.state()
			lines = playerIdentityLines("Clear the active queue on ", state.Target, max(1, m.width-4), "?")
		}
		request := *ui.queueClear
		var dialogChildren []bloom.Child
		for _, line := range lines {
			dialogChildren = append(dialogChildren, bloom.Fixed(1, bloom.Text(line)))
		}
		dialogChildren = append(dialogChildren,
			bloom.Fixed(1, bloom.Row(
				bloom.Fixed(12, bloom.Text(playerConfirmationLabel("Confirm", ui.queueConfirmAccept)).
					Style(playerButtonStyle(ui.queueConfirmAccept, true)).ID("queue-clear-confirm").On(on.Click(func() tea.Cmd {
					ui.closeQueueClear()
					request.Confirmed = true
					return playerMessage(request)
				}))),
				bloom.Fill(bloom.Text(playerConfirmationLabel("Cancel", !ui.queueConfirmAccept)).
					Style(playerButtonStyle(!ui.queueConfirmAccept, true)).ID("queue-clear-cancel").On(on.Click(func() tea.Cmd {
					ui.closeQueueClear()
					return nil
				}))),
			)),
			bloom.Fixed(1, playerHint("Tab/←/→ select · Enter activate · y confirm · n/Esc cancel")))
		dialog := bloom.Column(dialogChildren...)
		content = bloom.Overlay(content, bloom.Layer(playerModal(dialog)).ID("queue-clear").Modal().
			Anchor(bloom.Center, bloom.Center))
	}
	if ui.discovery != nil {
		ui.syncDiscoveryConfirmation()
		if confirmation := ui.discovery.presentation().confirmation; confirmation != nil {
			p := ui.discovery.presentation()
			dialogChildren := []bloom.Child{
				bloom.Fixed(1, bloom.Text(fmt.Sprintf("%s: %s",
					watchText(confirmation.action), watchText(confirmation.item.Title)))),
			}
			for _, line := range playerIdentityLines("Target: ", confirmation.target, max(1, m.width-4), "") {
				dialogChildren = append(dialogChildren, bloom.Fixed(1, bloom.Text(line)))
			}
			if confirmation.clearsQueue {
				dialogChildren = append(dialogChildren,
					bloom.Fixed(1, bloom.Text("WARNING: this radio action clears the active queue.")))
			}
			dialogChildren = append(dialogChildren,
				bloom.Fixed(1, bloom.Row(
					bloom.Fixed(12, bloom.Text(playerConfirmationLabel("Confirm", ui.discoveryAccept)).
						Style(playerButtonStyle(ui.discoveryAccept, true)).ID("discovery-confirm-yes").On(on.Click(func() tea.Cmd {
						return playerScopedCmd(p.generation, playerMessage(playerDiscoveryConfirmMsg{
							generation: p.generation, request: confirmation.request, accept: true,
						}))
					}))),
					bloom.Fill(bloom.Text(playerConfirmationLabel("Cancel", !ui.discoveryAccept)).
						Style(playerButtonStyle(!ui.discoveryAccept, true)).ID("discovery-confirm-no").On(on.Click(func() tea.Cmd {
						return playerScopedCmd(p.generation, playerMessage(playerDiscoveryConfirmMsg{
							generation: p.generation, request: confirmation.request,
						}))
					}))),
				)),
				bloom.Fixed(1, playerHint("Tab/←/→ select · Enter activate · y confirm · n/Esc cancel")))
			dialog := bloom.Column(dialogChildren...)
			content = bloom.Overlay(content, bloom.Layer(playerModal(dialog)).ID("discovery-confirm").Modal().
				Anchor(bloom.Center, bloom.Center))
		}
	}
	if m.chooserOpen {
		lines, rows := playerChooserLines(m)
		children := make([]bloom.Child, 0, len(lines))
		for i, line := range lines {
			node := bloom.Text(watchText(line))
			if i >= 2 && i-2 < len(rows) {
				index := rows[i-2]
				node = node.Style(playerSelectionStyle(index == m.chooserIndex, true)).
					ID(fmt.Sprintf("speaker-%d", index)).Focusable().On(on.Click(func() tea.Cmd {
					return func() tea.Msg { return playerChooseSpeakerMsg{index: index} }
				}))
			}
			children = append(children, bloom.Fixed(1, node))
		}
		dialog := bloom.Column(children...)
		content = bloom.Overlay(content, bloom.Layer(playerModal(dialog)).ID("speaker-chooser").Modal().
			Anchor(bloom.Center, bloom.Center))
	}
	view := ui.root.View(m.width, m.height, bloom.Transition(1, content,
		playerSurfaceColors{foreground: playerTextColor, background: playerSurfaceColor}))
	view.AltScreen = true
	return view
}

//nolint:gocyclo // Responsive presets and reachable collapsed panes are distinct.
func (ui *playerLayoutUI) panes(m *playerModel) bloom.Child {
	arrangement := ui.arrangementView()
	order := arrangement.Order
	if ui.zoom != "" {
		return ui.framedPane(m, ui.zoom)
	}
	if m.width < 90 {
		pane := ui.pane
		if pane == "" {
			pane = playerPaneDiscover
		}
		return ui.framedPane(m, pane)
	}
	if m.width < 120 {
		if ui.layout == playerListeningDesk {
			pane := ui.pane
			if pane != ModeQueue && pane != playerPaneDetails {
				pane = playerPaneDiscover
			}
			return bloom.Row(
				bloom.Fixed(min(arrangement.Width[playerPaneSources], m.width/3), ui.framedPane(m, playerPaneSources)).Min(8),
				bloom.Fixed(1, ui.divider(m, playerPaneSources)), bloom.Fill(ui.framedPane(m, pane)).Min(38),
			)
		}
		if ui.pane == playerPaneSources || ui.pane == ModeQueue {
			return ui.framedPane(m, ui.pane)
		}
		detailsWidth := arrangement.Width[playerPaneDetails]
		if detailsWidth == 0 {
			detailsWidth = playerDefaultArrangement(playerDiscoveryBrowser).Width[playerPaneDetails]
		}
		return bloom.Row(bloom.Fill(ui.framedPane(m, playerPaneDiscover)).Min(38),
			bloom.Fixed(1, ui.divider(m, playerPaneDiscover)),
			bloom.Fixed(min(detailsWidth, m.width/2), ui.framedPane(m, playerPaneDetails)).Min(8))
	}
	var children []bloom.Child
	for i, id := range order {
		if i > 0 {
			children = append(children, bloom.Fixed(1, ui.divider(m, order[i-1])))
		}
		visible := id
		if ui.layout == playerDiscoveryBrowser && id == playerPaneDetails && ui.pane == ModeQueue {
			visible = ModeQueue
		}
		if ui.layout == playerDiscoveryBrowser && id == ModeQueue && ui.pane == playerPaneDetails {
			visible = playerPaneDetails
		}
		if ui.layout == playerListeningDesk && id == ModeQueue && ui.pane == playerPaneDetails {
			visible = playerPaneDetails
		}
		pane := ui.framedPane(m, visible)
		if id == playerPaneDiscover {
			children = append(children, bloom.Fill(pane).Min(arrangement.Width[id]))
		} else {
			children = append(children, bloom.Fixed(arrangement.Width[id], pane).Min(8))
		}
	}
	return bloom.Row(children...)
}

func (ui *playerLayoutUI) framedPane(m *playerModel, pane string) bloom.Child {
	color := playerBorderColor
	border := lipgloss.NormalBorder()
	if ui.paneActive(pane) {
		color = playerAccentColor
		border = lipgloss.DoubleBorder()
	}
	return bloom.Box(lipgloss.NewStyle().Border(border).BorderForeground(color), ui.paneView(m, pane))
}

func (ui *playerLayoutUI) paneView(m *playerModel, pane string) bloom.Child {
	var content string
	switch pane {
	case playerPaneSources:
		return ui.sourcesView(m)
	case playerPaneDiscover:
		if ui.discovery != nil {
			return ui.discoveryView(m)
		}
		content = "Radio · Podcasts · UPnP\n\n/ search"
	case playerPaneDetails:
		if ui.discovery != nil {
			content = ui.discoveryDetails()
		} else {
			content = "Select a result to inspect details"
		}
	case ModeQueue:
		if ui.queue != nil {
			return ui.queueView(m)
		}
		content = "Speaker queue\n\nQ: open queue"
	}
	return bloom.Column(
		bloom.Fixed(1, ui.title(m, pane)),
		bloom.Fill(bloom.Text(content).ID(pane).Focusable().On(on.Click(func() tea.Cmd {
			ui.pane = pane
			return nil
		}))),
	)
}
