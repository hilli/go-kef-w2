// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerSourceNavItem struct {
	label   string
	source  kefw2.Source
	service playerDiscoveryService
	enabled bool
}

const playerSourcePodcasts = "Podcasts"

func (ui *playerLayoutUI) sourceNavItems() []playerSourceNavItem {
	var items []playerSourceNavItem
	if ui.controls != nil {
		for _, choice := range ui.sources() {
			label := string(choice.source)
			switch choice.source {
			case kefw2.SourceWiFi:
				label = "Wi-Fi"
			case kefw2.SourceBluetooth:
				label = "Bluetooth"
			case kefw2.SourceTV:
				label = "TV / HDMI"
			case kefw2.SourceUSB:
				label = "USB"
			case kefw2.SourceAux:
				label = "Aux"
			case kefw2.SourceOptical:
				label = "Optical"
			case kefw2.SourceCoaxial:
				label = "Coaxial"
			case kefw2.SourceStandby:
				label = "Off / Standby"
			}
			items = append(items, playerSourceNavItem{label: label, source: choice.source, enabled: choice.enabled})
		}
	}
	if ui.discovery != nil {
		items = append(items,
			playerSourceNavItem{label: "Radio", service: playerDiscoveryRadio, enabled: true},
			playerSourceNavItem{label: playerSourcePodcasts, service: playerDiscoveryPodcast, enabled: true},
			playerSourceNavItem{label: "UPnP library", service: playerDiscoveryUPnP, enabled: true},
		)
	}
	return items
}

func (ui *playerLayoutUI) activateSourceNav(
	generation, epoch uint64, ready bool, current kefw2.Source, item playerSourceNavItem,
) tea.Cmd {
	if !item.enabled {
		return nil
	}
	if item.service != "" {
		ui.pane = playerPaneDiscover
		ui.focus.Set(playerPaneDiscover)
		ui.searchActive = false
		ui.search.Blur()
		ui.search.SetValue("")
		return playerScopedCmd(generation, playerMessage(playerDiscoveryServiceMsg{
			generation: generation, service: item.service, browseRoot: true,
		}))
	}
	if !ready || current == item.source {
		return nil
	}
	return playerScopedCmd(generation, playerMessage(playerSourceRequest{
		generation: generation, epoch: epoch, source: item.source,
	}))
}

func (ui *playerLayoutUI) sourceNavKey(m *playerModel, key tea.KeyPressMsg) bool {
	items := ui.sourceNavItems()
	if len(items) == 0 {
		return false
	}
	switch key.Code {
	case tea.KeyUp, 'k':
		ui.sourceNavIndex = max(0, ui.sourceNavIndex-1)
	case tea.KeyDown, 'j':
		ui.sourceNavIndex = min(len(items)-1, ui.sourceNavIndex+1)
	case tea.KeyHome:
		ui.sourceNavIndex = 0
	case tea.KeyEnd:
		ui.sourceNavIndex = len(items) - 1
	case tea.KeyEnter:
		epoch, ready, current := ui.sourceNavStatus(m)
		ui.outbound = append(ui.outbound, ui.activateSourceNav(
			m.generation, epoch, ready, current, items[min(ui.sourceNavIndex, len(items)-1)]))
	default:
		return false
	}
	return true
}

func (ui *playerLayoutUI) sourcesView(m *playerModel) bloom.Child {
	items := ui.sourceNavItems()
	selected := min(ui.sourceNavIndex, max(0, len(items)-1))
	epoch, ready, current := ui.sourceNavStatus(m)
	generation := m.generation
	children := []bloom.Child{}
	selectedLine := 0
	for index, item := range items {
		if index == 0 || item.source == kefw2.SourceStandby || (item.service != "" && items[index-1].service == "") {
			label := "INPUTS  (* on)"
			switch {
			case item.source == kefw2.SourceStandby:
				label = "POWER"
			case item.service != "":
				label = "BROWSE WI-FI"
			}
			children = append(children, bloom.Fixed(1, bloom.Text(label).Style(playerHintStyle.Bold(true))))
		}
		if index == selected {
			selectedLine = len(children)
		}
		marker := " "
		if index == selected && ui.pane == playerPaneSources {
			marker = ">"
		}
		playing := " "
		if item.source != "" && item.source == current {
			playing = "*"
		}
		label := marker + playing + " " + item.label
		if !item.enabled {
			label += " -"
		}
		style := playerSelectionStyle(index == selected && ui.pane == playerPaneSources, true)
		if !item.enabled || (item.service == "" && !ready) {
			style = style.Foreground(playerMutedColor)
		}
		i := index
		activate := func() tea.Cmd {
			return ui.activateSourceNav(generation, epoch, ready, current, item)
		}
		row := bloom.Text(label).Style(style).Truncate().ID(fmt.Sprintf("source-nav-%d", index)).Focusable().On(
			on.Click(func() tea.Cmd {
				ui.pane, ui.sourceNavIndex = playerPaneSources, i
				ui.focus.Set(playerPaneSources)
				if item.service != "" {
					return activate()
				}
				return nil
			}),
			on.DoubleClick(func() tea.Cmd {
				ui.sourceNavIndex = i
				return activate()
			}),
		)
		children = append(children, bloom.Fixed(1, row))
	}
	visible := max(1, m.height-max(3, m.height/3)-7)
	offset := max(0, selectedLine-visible+1)
	open := bloom.Text("[Open]").Style(playerButtonStyle(ui.focus.Is("source-nav-open"), len(items) > 0)).
		ID("source-nav-open").Focusable().On(on.Click(func() tea.Cmd {
		if len(items) == 0 {
			return nil
		}
		return ui.activateSourceNav(generation, epoch, ready, current, items[selected])
	}))
	return bloom.Column(
		bloom.Fixed(1, ui.title(m, playerPaneSources)),
		bloom.Fill(bloom.Scroll(bloom.Column(children...), offset).ID("sources-list")),
		bloom.Fixed(1, playerHint("↑↓ select")),
		bloom.Fixed(1, bloom.Row(bloom.Fixed(7, open), bloom.Fill(playerHint("Enter open")))),
	)
}

func (ui *playerLayoutUI) sourceNavStatus(m *playerModel) (epoch uint64, ready bool, current kefw2.Source) {
	if ui.controls != nil {
		state := ui.controls.State()
		epoch, ready = state.epoch, state.ready
	}
	if snapshot := ui.confirmedSnapshot(m); snapshot != nil {
		current = snapshot.Data.Source
		if snapshot.Data.Power == kefw2.SpeakerStatusStandby {
			current = kefw2.SourceStandby
		}
	}
	return epoch, ready, current
}
