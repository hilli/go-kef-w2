// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func playerView(m *playerModel) tea.View {
	if m.ui != nil {
		return m.ui.view(m)
	}
	if m.width < 1 || m.height < 1 {
		return tea.View{AltScreen: true}
	}
	var lines []string
	var chooserRows []int
	if m.chooserOpen {
		lines, chooserRows = playerChooserLines(m)
	} else {
		lines = playerMainLines(m)
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(watchText(lines[i]), m.width, "")
	}
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.OnMouse = playerMouseHandler(m.chooserOpen, chooserRows)
	return view
}

func playerChooserLines(m *playerModel) ([]string, []int) {
	lines := []string{"Choose a speaker", "Filter: " + m.chooserFilter + "  |  Enter: connect  |  Esc: cancel"}
	if len(m.speakers) == 0 {
		return append(lines, "No configured speakers. Use 'kefw2 config speaker add <ip-address>'."), nil
	}
	matches := m.chooserMatches()
	if len(matches) == 0 {
		return append(lines, "No matching speakers."), nil
	}
	frameHeight := 0
	if m.ui != nil {
		frameHeight = 2
	}
	visible := max(0, m.height-len(lines)-frameHeight)
	selected := 0
	for i, index := range matches {
		if index == m.chooserIndex {
			selected = i
			break
		}
	}
	start := max(0, selected-visible+1)
	var rows []int
	for _, index := range matches[start:] {
		if len(rows) >= visible {
			break
		}
		lines = append(lines, playerChooserRow(m, index))
		rows = append(rows, index)
	}
	return lines, rows
}

func playerChooserRow(m *playerModel, index int) string {
	speaker := m.speakers[index]
	marker := "  "
	if index == m.chooserIndex {
		marker = "> "
	}
	name := speaker.Name
	if name == "" {
		name = "Unnamed"
	}
	model := speaker.Model
	if model == "" {
		model = "Unknown model"
	}
	line := fmt.Sprintf("%s%s | %s | %s", marker, watchText(name), watchText(model), watchText(speaker.IPAddress))
	if speaker.Default {
		line += " | default"
	}
	if m.target != nil && speaker.IPAddress == m.target.IPAddress {
		line += " | current"
	}
	width := m.width
	if m.ui != nil {
		width -= 4
	}
	if ansi.StringWidth(line) > width {
		suffix := " | " + watchText(speaker.IPAddress)
		nameWidth := max(0, width-ansi.StringWidth(marker+suffix))
		line = marker + playerTruncateName(watchText(name), nameWidth) + suffix
	}
	return line
}

func playerTruncateName(name string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(name, width, "…")
}

func playerIdentityLines(prefix string, speaker playerSpeaker, width int, tail string) []string {
	name, address := watchText(speaker.Name), watchText(speaker.IPAddress)
	if name == "" {
		name = "Unnamed"
	}
	suffix := " (" + address + ")" + tail
	nameWidth := width - ansi.StringWidth(prefix+suffix)
	if nameWidth >= 0 {
		return []string{prefix + playerTruncateName(name, nameWidth) + suffix}
	}
	nameWidth = max(0, width-ansi.StringWidth(prefix+tail))
	addressLines := playerAddressLines(address, width)
	lines := make([]string, 0, 1+len(addressLines))
	lines = append(lines, prefix+playerTruncateName(name, nameWidth)+tail)
	return append(lines, addressLines...)
}

func playerAddressLines(address string, width int) []string {
	wrapped := strings.Split(ansi.Hardwrap(address, max(1, width-4), true), "\n")
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, "IP: "+line)
	}
	return lines
}

func playerMainLines(m *playerModel) []string {
	lines := []string{"KEF Player | " + m.layout}
	if m.target == nil {
		lines = append(lines, "Speaker: none selected  [S choose]")
	} else {
		lines = append(lines, playerSpeakerHeader(m), "State: "+string(m.connection))
		lines = append(lines, playerStatusLines(m)...)
	}
	return append(lines, "S choose speaker  |  q quit")
}

func playerSpeakerHeader(m *playerModel) string {
	speaker := *m.target
	if m.speakerInfo != nil && m.speakerInfo.Name != "" {
		speaker.Name = m.speakerInfo.Name
	}
	name := speaker.Name
	if name == "" {
		name = speaker.IPAddress
	}
	line := fmt.Sprintf("Speaker: %s (%s)", watchText(name), watchText(speaker.IPAddress))
	if !speaker.Configured {
		line += " [session-only]"
	}
	return line + "  [S choose]"
}

func playerStatusLines(m *playerModel) []string {
	var lines []string
	if m.snapshot != nil {
		status := m.snapshot.Data
		lines = append(lines, fmt.Sprintf("Source: %s | Power: %s", status.Source, status.Power),
			fmt.Sprintf("Volume: %d | Muted: %t", status.Volume, status.Muted))
		lines = appendWatchPlayback(lines, m.snapshot)
	} else {
		lines = append(lines, "Waiting for speaker status...")
	}
	switch {
	case m.maxVolume != nil:
		lines = append(lines, fmt.Sprintf("Maximum volume: %d", *m.maxVolume))
	case m.maxVolumeErr != nil:
		lines = append(lines, "Maximum volume unavailable: "+m.maxVolumeErr.Error())
	default:
		lines = append(lines, "Maximum volume: loading")
	}
	if m.readErr != nil {
		lines = append(lines, "Status unavailable (stale): "+m.readErr.Error(), "r: retry  |  S: choose speaker")
	}
	if m.eventErr != nil {
		lines = append(lines, "Events disconnected; polling: "+m.eventErr.Error())
	}
	return lines
}

func playerMouseHandler(chooserOpen bool, chooserRows []int) func(tea.MouseMsg) tea.Cmd {
	return func(message tea.MouseMsg) tea.Cmd {
		click, ok := message.(tea.MouseClickMsg)
		if !ok || click.Button != tea.MouseLeft {
			return nil
		}
		if chooserOpen && click.Y >= 2 && click.Y < 2+len(chooserRows) {
			index := chooserRows[click.Y-2]
			return func() tea.Msg { return playerChooseSpeakerMsg{index: index} }
		}
		if !chooserOpen && click.Y == 1 {
			return func() tea.Msg { return playerOpenChooserMsg{} }
		}
		return nil
	}
}
