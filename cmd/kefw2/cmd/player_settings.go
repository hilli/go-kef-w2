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

func (ui *playerLayoutUI) openSettings() {
	ui.cancelPointerInteractions()
	ui.settings, ui.settingsLibrary = true, false
	ui.settingsIndex = 0
	ui.focus.Push("settings")
	ui.focus.Set("settings-0")
}

func (ui *playerLayoutUI) closeSettings() {
	if ui.settings {
		ui.settings, ui.settingsLibrary = false, false
		ui.focus.Pop()
	}
}

func (ui *playerLayoutUI) openLibrarySettings(m *playerModel) {
	if !ui.settings {
		ui.openSettings()
	}
	ui.settingsLibrary, ui.settingsServer = true, 0
	ui.focus.Set("settings-server-0")
	ui.search.Blur()
	ui.searchActive = false
	if ui.discovery != nil {
		p := ui.discovery.presentation()
		ui.sendScoped(m.generation, playerDiscoveryServiceMsg{
			generation: p.generation, service: playerDiscoveryUPnP, browseRoot: true,
		})
	}
}

type playerSetting struct {
	label, shortcut string
	enabled         bool
}

func (ui *playerLayoutUI) settingChoices() []playerSetting {
	return []playerSetting{
		{"Choose speaker", "S", true},
		{"Choose input", "s", ui.controls != nil},
		{"UPnP library", "Enter", ui.discovery != nil},
		{"Pane arrangement", "p", true},
		{fmt.Sprintf("Reduced motion: %t", ui.reduced), "Enter", true},
	}
}

func (ui *playerLayoutUI) activateSetting(m *playerModel, index int) {
	choices := ui.settingChoices()
	if index < 0 || index >= len(choices) || !choices[index].enabled {
		return
	}
	switch index {
	case 0:
		ui.closeSettings()
		m.openSpeakerChooser()
	case 1:
		ui.closeSettings()
		ui.openSource()
	case 2:
		ui.openLibrarySettings(m)
	case 3:
		ui.closeSettings()
		ui.menu = true
		ui.focus.Push("pane-menu")
	case 4:
		ui.reduced = !ui.reduced
	}
}

func (ui *playerLayoutUI) settingsKey(m *playerModel, key tea.KeyPressMsg) bool {
	if key.Code == tea.KeyEsc {
		if ui.settingsLibrary {
			ui.settingsLibrary = false
			ui.settingsIndex = 2
			ui.focus.Set("settings-2")
		} else {
			ui.closeSettings()
		}
		return true
	}
	if ui.settingsLibrary {
		return ui.librarySettingsKey(key)
	}
	n := len(ui.settingChoices())
	switch key.String() {
	case "up", playerLayoutKeyShiftTab:
		ui.settingsIndex = (ui.settingsIndex + n - 1) % n
	case playerKeyDown, playerLayoutKeyTab:
		ui.settingsIndex = (ui.settingsIndex + 1) % n
	case KeyEnter:
		ui.activateSetting(m, ui.settingsIndex)
	}
	if ui.settings && !ui.settingsLibrary {
		ui.focus.Set(fmt.Sprintf("settings-%d", ui.settingsIndex))
	}
	return true
}

func (ui *playerLayoutUI) librarySettingsKey(key tea.KeyPressMsg) bool {
	if ui.discovery == nil {
		return true
	}
	p := ui.discovery.presentation()
	n := len(p.state.page.items)
	switch key.String() {
	case "up", playerLayoutKeyShiftTab:
		ui.settingsServer = max(0, ui.settingsServer-1)
	case playerKeyDown, playerLayoutKeyTab:
		ui.settingsServer = min(max(0, n-1), ui.settingsServer+1)
	case KeyEnter, "u":
		ui.useSettingsServer(p, ui.settingsServer)
	case "I":
		ui.sendScoped(p.generation, playerUPnPIndexMsg{generation: p.generation})
	case "x":
		ui.sendScoped(p.generation, playerUPnPCancelIndexMsg{generation: p.generation})
	case "r":
		ui.sendScoped(p.generation, playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryUPnP, browseRoot: true})
	case "<", ">":
		offset := p.previousOffset
		if key.String() == ">" {
			if !p.state.page.hasMore {
				return true
			}
			offset = p.state.page.nextOffset
		} else if !p.hasPrevious {
			return true
		}
		ui.settingsServer = 0
		ui.sendScoped(p.generation, playerDiscoveryPageMsg{
			generation: p.generation, request: p.state.request, offset: offset,
		})
	}
	ui.focus.Set(fmt.Sprintf("settings-server-%d", ui.settingsServer))
	return true
}

func (ui *playerLayoutUI) useSettingsServer(p playerDiscoveryPresentation, index int) {
	if playerUPnPOnServerList(p, index) && p.upnp.phase != playerUPnPIndexing {
		ui.sendScoped(p.generation, playerUPnPUseServerMsg{
			generation: p.generation, request: p.state.request, index: index,
		})
	}
}

func (ui *playerLayoutUI) settingsView(m *playerModel) bloom.Child {
	if ui.settingsLibrary && ui.discovery != nil {
		return ui.librarySettingsView(m)
	}
	children := []bloom.Child{
		bloom.Fixed(1, bloom.Text("SETTINGS").Style(playerTitleStyle(true))),
		bloom.Fixed(1, playerHintStyleText("Changes apply immediately; Esc closes.")),
		bloom.Fixed(1, nil),
	}
	for index, choice := range ui.settingChoices() {
		children = append(children, bloom.Fixed(1, bloom.Text(
			fmt.Sprintf("%-32s %s", choice.label, choice.shortcut)).
			Style(playerButtonStyle(ui.settingsIndex == index, choice.enabled)).
			ID(fmt.Sprintf("settings-%d", index)).Focusable().On(on.Click(func() tea.Cmd {
			ui.settingsIndex = index
			ui.activateSetting(m, index)
			return ui.takeOutbound()
		}))))
	}
	children = append(children, bloom.Fill(bloom.Text("")), bloom.Fixed(1, playerHint("↑↓ select · Enter open/toggle · Esc close")))
	return bloom.Column(children...)
}

func playerHintStyleText(text string) bloom.TextNode {
	return bloom.Text(text).Style(playerHintStyle).Truncate()
}

func (ui *playerLayoutUI) takeOutbound() tea.Cmd {
	cmd := tea.Batch(ui.outbound...)
	ui.outbound = nil
	return cmd
}

func (ui *playerLayoutUI) librarySettingsView(m *playerModel) bloom.Child {
	p := ui.discovery.presentation()
	width := max(1, min(68, m.width-6))
	fixedRows := 1
	children := []bloom.Child{
		bloom.Fixed(1, bloom.Text("SETTINGS / UPnP LIBRARY").Style(playerTitleStyle(true))),
	}
	card := ui.upnpSetupCard(p, "settings-upnp-status")
	if p.upnp.phase == playerUPnPIndexing {
		card[0] = bloom.Fixed(1, bloom.Text("INDEXING "+watchText(p.upnp.config.server)).
			Style(playerButtonStyle(false, true)).Truncate())
		card[1] = bloom.Fixed(1, playerHintStyleText(fmt.Sprintf("%s tracks · %s folders · %s elapsed",
			playerCount(p.upnp.tracks), playerCount(p.upnp.containers), playerClock(p.upnp.elapsed))))
	}
	if p.upnp.phase == playerUPnPFailed {
		card[0] = bloom.Fixed(1, bloom.Text("INDEX FAILED").Style(playerHintStyle.Foreground(playerWarningColor).Bold(true)))
	}
	children = append(children, card...)
	fixedRows += len(card)
	appendProblem := func(text string) {
		lines := strings.Split(ansi.Wrap(watchText(text), width, ""), "\n")
		if len(lines) > 2 {
			lines = append(lines[:1], ansi.Truncate(lines[1], width-1, "")+"…")
		}
		children = append(children, bloom.Fixed(len(lines), bloom.Text(strings.Join(lines, "\n")).
			Style(playerHintStyle.Foreground(playerWarningColor))))
		fixedRows += len(lines)
	}
	if p.upnp.err != nil {
		appendProblem(playerUPnPError(p.upnp.err))
	}
	folder := p.upnp.config.container
	if folder == "" {
		folder = "automatic (CLI configuration)"
	}
	children = append(children, bloom.Fixed(1, playerHintStyleText("Index folder: "+watchText(folder))))
	fixedRows++
	if p.upnp.phase == playerUPnPIndexing && p.upnp.current != "" {
		children = append(children, bloom.Fixed(1, playerHintStyleText("Scanning: "+watchText(p.upnp.current))))
		fixedRows++
	}
	switch {
	case p.action != nil && p.action.err != nil:
		appendProblem(p.action.action + ": " + p.action.err.Error())
	case p.state.err != nil:
		appendProblem("Servers: " + p.state.err.Error())
	case p.state.status == playerDiscoveryLoading:
		children = append(children, bloom.Fixed(1, playerHintStyleText("Loading media servers…")))
		fixedRows++
	}
	children = append(children, bloom.Fixed(1, nil))
	ui.settingsServer = min(max(0, ui.settingsServer), max(0, len(p.state.page.items)-1))
	fixedRows += 3
	rows := max(1, min(7, min(20, m.height-2)-2-fixedRows))
	start := max(0, ui.settingsServer-rows+1)
	for index := start; index < len(p.state.page.items) && index < start+rows; index++ {
		item := p.state.page.items[index]
		label := watchText(item.Title)
		if item.Path == p.upnp.config.path {
			label += " · configured"
		}
		children = append(children, bloom.Fixed(1, bloom.Text(ansi.Truncate(label, width, "…")).
			Style(playerButtonStyle(index == ui.settingsServer, playerUPnPOnServerList(p, index))).
			ID(fmt.Sprintf("settings-server-%d", index)).Focusable().On(on.Click(func() tea.Cmd {
			ui.settingsServer = index
			ui.useSettingsServer(p, index)
			return ui.takeOutbound()
		}))))
	}
	children = append(children, bloom.Fill(bloom.Text("")))
	button := func(label string, message tea.Msg, enabled bool) bloom.Child {
		return bloom.Fixed(len(label)+1, bloom.Text(label).Style(playerButtonStyle(false, enabled)).
			ID("settings-"+label).On(on.Click(func() tea.Cmd {
			if !enabled {
				return nil
			}
			return playerScopedCmd(p.generation, playerMessage(message))
		})))
	}
	children = append(children, bloom.Fixed(1, bloom.Row(
		button("[I Build/refresh]", playerUPnPIndexMsg{generation: p.generation},
			p.upnp.config.path != "" && p.upnp.phase != playerUPnPIndexing),
		button("[x Cancel]", playerUPnPCancelIndexMsg{generation: p.generation}, p.upnp.phase == playerUPnPIndexing),
		button("[r Reload]", playerDiscoveryServiceMsg{generation: p.generation, service: playerDiscoveryUPnP, browseRoot: true}, true),
	)))
	hint := "↑↓ select · Enter use server · < > pages · Esc back"
	children = append(children, bloom.Fixed(1, playerHint(hint)))
	return bloom.Column(children...)
}
