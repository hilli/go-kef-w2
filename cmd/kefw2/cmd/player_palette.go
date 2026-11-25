// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

type playerPaletteCommand struct {
	id, label, shortcut, disabled string
	run                           func()
}

func playerPaletteShortcut(key tea.KeyPressMsg) bool {
	return (key.Code == 'p' || key.Code == 'P') &&
		(key.Mod == tea.ModCtrl || key.Mod == tea.ModCtrl|tea.ModShift)
}

func (ui *playerLayoutUI) openPalette() {
	ui.cancelPointerInteractions()
	ui.closeSettings()
	if ui.menu {
		ui.menu = false
		ui.focus.Pop()
	}
	ui.search.Blur()
	ui.searchActive = false
	ui.palette, ui.paletteIndex = true, 0
	ui.paletteSearch.SetValue("")
	ui.focus.Push("command-palette")
	ui.focus.Set("palette-search")
	ui.outbound = append(ui.outbound, ui.paletteSearch.Focus())
}

func (ui *playerLayoutUI) closePalette() {
	if ui.palette {
		ui.palette = false
		ui.paletteSearch.Blur()
		ui.focus.Pop()
	}
}

func (ui *playerLayoutUI) paletteMatches(m *playerModel) []playerPaletteCommand {
	commands := ui.paletteCommands(m)
	query := strings.TrimSpace(ui.paletteSearch.Value())
	if query == "" {
		return commands
	}
	labels := make([]string, len(commands))
	for i, command := range commands {
		labels[i] = command.label
	}
	matches := fuzzy.Find(query, labels)
	result := make([]playerPaletteCommand, len(matches))
	for i, match := range matches {
		result[i] = commands[match.Index]
	}
	return result
}

func (ui *playerLayoutUI) paletteKey(m *playerModel, key tea.KeyPressMsg) bool {
	matches := ui.paletteMatches(m)
	switch {
	case key.Code == tea.KeyEsc || playerPaletteShortcut(key):
		ui.closePalette()
	case key.Code == tea.KeyUp || key.String() == playerLayoutKeyShiftTab:
		ui.paletteIndex = max(0, ui.paletteIndex-1)
	case key.Code == tea.KeyDown || key.Code == tea.KeyTab:
		ui.paletteIndex = min(max(0, len(matches)-1), ui.paletteIndex+1)
	case key.Code == tea.KeyEnter:
		if len(matches) > 0 {
			index := min(ui.paletteIndex, len(matches)-1)
			ui.activatePalette(m, matches[index].id)
		}
	default:
		ui.updatePaletteInput(key)
	}
	return true
}

func (ui *playerLayoutUI) updatePaletteInput(msg tea.Msg) {
	before := ui.paletteSearch.Value()
	var cmd tea.Cmd
	ui.paletteSearch, cmd = ui.paletteSearch.Update(msg)
	if before != ui.paletteSearch.Value() {
		ui.paletteIndex = 0
	}
	if cmd != nil {
		ui.outbound = append(ui.outbound, cmd)
	}
}

// Resolve again at activation: speaker readiness or the selected row may have
// changed while the palette was open or since the last mouse frame.
func (ui *playerLayoutUI) activatePalette(m *playerModel, id string) {
	for _, command := range ui.paletteCommands(m) {
		if command.id != id {
			continue
		}
		if command.disabled == "" {
			ui.closePalette()
			command.run()
		}
		return
	}
}

func (ui *playerLayoutUI) paletteView(m *playerModel) bloom.Child {
	width := max(1, min(72, m.width-2)-4)
	input := ui.paletteSearch
	input.SetWidth(max(1, width-2))
	children := []bloom.Child{
		bloom.Fixed(1, bloom.Text("COMMAND PALETTE").Style(playerTitleStyle(true))),
		bloom.Fixed(1, bloom.Text("> "+input.View()).ID("palette-search").Focusable()),
		bloom.Fixed(1, nil),
	}
	matches := ui.paletteMatches(m)
	ui.paletteIndex = min(max(0, ui.paletteIndex), max(0, len(matches)-1))
	rows := max(1, min(8, m.height-9))
	start := max(0, ui.paletteIndex-rows+1)
	for index := start; index < len(matches) && index < start+rows; index++ {
		command := matches[index]
		keyWidth := min(24, ansi.StringWidth(command.shortcut))
		labelWidth := max(1, width-keyWidth-2)
		label := ansi.Truncate(command.label, labelWidth, "…")
		text := label + strings.Repeat(" ", max(0, labelWidth-ansi.StringWidth(label))) +
			"  " + command.shortcut
		children = append(children, bloom.Fixed(1, bloom.Text(text).
			Style(playerButtonStyle(index == ui.paletteIndex, command.disabled == "")).
			ID("palette-"+command.id).On(on.Click(func() tea.Cmd {
			ui.paletteIndex = index
			ui.activatePalette(m, command.id)
			return ui.takeOutbound()
		}))))
	}
	if len(matches) == 0 {
		children = append(children, bloom.Fixed(1, playerHintStyleText("No matching commands. Try a shorter query.")))
	}
	children = append(children, bloom.Fill(bloom.Text("")))
	detail := fmt.Sprintf("%d commands · type to filter", len(matches))
	if len(matches) > 0 && matches[ui.paletteIndex].disabled != "" {
		detail = matches[ui.paletteIndex].disabled
	}
	children = append(children,
		bloom.Fixed(1, playerHintStyleText(detail)),
		bloom.Fixed(1, playerHint("↑↓ select · Enter run · Esc close")))
	return bloom.Column(children...)
}

//nolint:gocyclo // Each command keeps its existing action and availability guard.
func (ui *playerLayoutUI) paletteCommands(m *playerModel) []playerPaletteCommand {
	var commands []playerPaletteCommand
	add := func(id, label, shortcut string, enabled bool, reason string, run func()) {
		disabled := ""
		if !enabled {
			disabled = reason
		}
		commands = append(commands, playerPaletteCommand{id, label, shortcut, disabled, run})
	}
	navigate := func(pane string) {
		ui.pane = pane
		ui.focus.Set(pane)
	}
	add("settings", "Settings: open", "Ctrl+,", true, "", ui.openSettings)
	add("speaker", "Speaker: choose", "S", true, "", m.openSpeakerChooser)
	add("input", "Input: choose", "s", ui.controls != nil, "No speaker controls available.", ui.openSource)
	add("library", "UPnP: configure server and index", "Settings > Library", ui.discovery != nil,
		"Discovery is unavailable.", func() { ui.openLibrarySettings(m) })
	add("queue", "Queue: focus", "Q", true, "", func() { navigate(ModeQueue) })
	add("details", "Details: focus", "i", true, "", func() { navigate(playerPaneDetails) })
	add("sources", "Sources: focus", "Alt+1 (default)", true, "", func() { navigate(playerPaneSources) })
	add("help", "Help: keyboard shortcuts", "?", true, "", func() {
		ui.help = true
		ui.focus.Push("help")
	})
	add("panes", "Layout: pane arrangement", "p", true, "", func() {
		ui.menu = true
		ui.focus.Push("pane-menu")
	})
	add("zoom", "Layout: toggle focused pane zoom", "z", true, "", ui.zoomPane)
	add("move-pane", "Layout: move focused pane", "p then m", true, "", func() {
		ui.menu = true
		ui.focus.Push("pane-menu")
		ui.startMode(playerLayoutMove)
	})
	add("resize-pane", "Layout: resize focused pane", "p then r", true, "", func() {
		ui.menu = true
		ui.focus.Push("pane-menu")
		ui.startMode(playerLayoutResize)
	})
	add("reset-layout", "Layout: reset saved arrangement", "p then R", true, "", func() {
		ui.arrangement = playerDefaultArrangement(ui.layout)
		ui.save(true)
	})
	add("swap-bottom", "Layout: swap Now Playing and Volume", "p then b", true, "", ui.swapBottomPanes)
	if ui.layout == playerDiscoveryBrowser {
		add("dock-queue", "Layout: dock or undock queue", "p then d", true, "", func() {
			ui.toggleQueueDock()
			ui.focus.Set(ui.pane)
		})
	}
	add("motion", "Settings: toggle reduced motion", "Settings > Enter", true, "", func() { ui.reduced = !ui.reduced })
	if ui.discovery != nil {
		p := ui.discovery.presentation()
		add("search", "Discover: search library or service", "/", p.searchable(),
			"Build an index in Settings > UPnP library first.", func() {
				navigate(playerPaneDiscover)
				ui.searchActive = true
				ui.focus.Set("search")
				ui.outbound = append(ui.outbound, ui.search.Focus())
			})
		for index, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast, playerDiscoveryUPnP} {
			add("service-"+string(service), "Discover: browse "+string(service), fmt.Sprintf("%d (Discover)", index+1),
				true, "", func() {
					navigate(playerPaneDiscover)
					ui.sendScoped(m.generation, playerDiscoveryServiceMsg{
						generation: m.generation, service: service, browseRoot: true,
					})
				})
		}
		add("index", "UPnP: build or refresh search index", "I (UPnP)", p.upnp.config.path != "" &&
			p.upnp.phase != playerUPnPIndexing && ui.discovery.ready(),
			"Choose a UPnP server and connect a speaker; cancel any active build first.", func() {
				ui.sendScoped(m.generation, playerUPnPIndexMsg{generation: m.generation})
			})
		add("cancel-index", "UPnP: cancel indexing", "x (UPnP)", p.upnp.phase == playerUPnPIndexing,
			"No index build is running.", func() {
				ui.sendScoped(m.generation, playerUPnPCancelIndexMsg{generation: m.generation})
			})
		discover := func(id, label, shortcut string, enabled bool, reason string, key rune) {
			add(id, label, shortcut, enabled, reason, func() {
				navigate(playerPaneDiscover)
				ui.discoveryKey(tea.KeyPressMsg{Code: key})
			})
		}
		selected := p.selection >= 0 && p.selection < len(p.state.page.items)
		browsable := selected && playerDiscoveryIsBrowsable(p, p.state.page.items[p.selection])
		discover("open", "Discover: open or play selected result", "Enter (Discover)",
			browsable || (selected && p.service != playerDiscoveryUPnP && p.writeReady),
			"Select a browsable result or a playable radio/podcast with fresh speaker status.", tea.KeyEnter)
		discover("enqueue", "UPnP: enqueue selected track", "e (Discover)", playerDiscoveryCanEnqueue(p, p.selection),
			"Select a UPnP track and wait for fresh speaker status.", 'e')
		discover("favorite", "Discover: add selected favorite", "f (Discover)", selected && p.service != playerDiscoveryUPnP && p.writeReady,
			"Select a radio/podcast result and wait for fresh speaker status.", 'f')
		discover("unfavorite", "Discover: remove selected favorite", "F (Discover)", selected && p.service != playerDiscoveryUPnP && p.writeReady,
			"Select a radio/podcast result and wait for fresh speaker status.", 'F')
		discover("favorites", "Discover: browse favorites", "v (Discover)", p.service != playerDiscoveryUPnP,
			"UPnP does not support favorites.", 'v')
		discover("retry-discovery", "Discover: retry failed read", "r (Discover)", p.canRetry,
			"No failed read to retry.", 'r')
	}
	if ui.controls != nil {
		state := ui.controls.State()
		for _, control := range []struct {
			id, label, shortcut string
			action              playerControlAction
		}{
			{"play-pause", "Playback: play or pause", "Space", playerControlPlayPause},
			{"previous", "Playback: previous track", "[", playerControlPrevious},
			{"playback-next", "Playback: next track", "]", playerControlNext},
			{"playback-seek-back", "Playback: seek backward", ",", playerControlSeekBackward},
			{"seek-forward", "Playback: seek forward", ".", playerControlSeekForward},
			{"volume-mute", "Volume: toggle mute", "m", playerControlMute},
		} {
			reason := ""
			if !state.ready || state.confirmed == nil {
				reason = "Wait for fresh speaker status."
			} else if err := playerControlUnavailable(*state.confirmed, control.action); err != nil {
				reason = err.Error()
			}
			add(control.id, control.label, control.shortcut, reason == "", reason, func() {
				if control.action == playerControlMute {
					ui.controlsKey(m, tea.KeyPressMsg{Code: 'm', Text: "m"})
				} else {
					ui.sendTransport(m.generation, control.action)
				}
			})
		}
		for _, delta := range []struct{ label, key string }{{"Volume: increase", "+"}, {"Volume: decrease", "-"}} {
			add("volume-"+delta.key, delta.label, delta.key, state.ready, "Wait for fresh speaker status.", func() {
				ui.sendScoped(m.generation, playerVolumeKeyRequest{
					generation: m.generation, epoch: state.epoch, key: delta.key,
				})
			})
		}
		add("retry-volume", "Volume: retry failed change", "R", state.volumeCanRetry,
			"No volume change to retry.", func() { ui.controlsKey(m, tea.KeyPressMsg{Code: 'R', Text: "R"}) })
		add("dismiss-controls", "Controls: dismiss error", "D", state.volumeErr != nil || state.err != nil,
			"No control error to dismiss.", func() { ui.controlsKey(m, tea.KeyPressMsg{Code: 'D', Text: "D"}) })
	}
	if ui.queue != nil {
		state := ui.queue.state()
		selected := ui.queueIndex >= 0 && ui.queueIndex < len(state.Items)
		writable := state.Ready && !state.Busy && !state.Loading && !state.Uncertain
		for _, action := range []struct {
			id, label, shortcut string
			key                 rune
			enabled             bool
		}{
			{"queue-play", "Queue: play selected track", "Enter (Queue)", tea.KeyEnter, writable && selected},
			{"queue-remove", "Queue: remove selected track", "Del (Queue)", tea.KeyDelete, writable && selected},
			{"queue-clear", "Queue: clear (confirmation)", "c (Queue)", 'c', writable && len(state.Items) > 0},
			{"queue-shuffle", "Queue: toggle shuffle", "h (Queue)", 'h', writable && state.ModeKnown},
			{"queue-repeat", "Queue: cycle repeat", "t (Queue)", 't', writable && state.ModeKnown},
			{"queue-reload", "Queue: reload", "r (Queue)", 'r', !state.Busy && !state.Loading},
			{"queue-review", "Queue: acknowledge reviewed order", "a (Queue)", 'a', state.ReviewReady},
		} {
			add(action.id, action.label, action.shortcut, action.enabled,
				"Queue action unavailable: select a row, reload, or wait for fresh status.", func() {
					navigate(ModeQueue)
					ui.queueKey(tea.KeyPressMsg{Code: action.key})
				})
		}
		for _, move := range []struct {
			id, label, shortcut string
			code                rune
			enabled             bool
		}{
			{"queue-up", "Queue: move selected track up", "Ctrl+k (Queue)", 'k', writable && selected && ui.queueIndex > 0},
			{"queue-down", "Queue: move selected track down", "Ctrl+j (Queue)", 'j', writable && selected && ui.queueIndex < len(state.Items)-1},
		} {
			add(move.id, move.label, move.shortcut, move.enabled,
				"Select a movable row in a ready queue.", func() {
					navigate(ModeQueue)
					ui.queueKey(tea.KeyPressMsg{Code: move.code, Mod: tea.ModCtrl})
				})
		}
	}
	add("quit", "Player: quit", "q", true, "", func() { ui.send(tea.QuitMsg{}) })
	return commands
}
