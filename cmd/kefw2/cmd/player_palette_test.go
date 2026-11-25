// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestPlayerPaletteBindingsFilteringAndFocus(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: 'p', Mod: tea.ModCtrl},
		{Code: 'p', Mod: tea.ModCtrl | tea.ModShift},
		{Code: 'P', Mod: tea.ModCtrl | tea.ModShift},
	} {
		t.Run(key.String(), func(t *testing.T) {
			m := playerDialogTestModel(t)
			m.ui.focus.Set(playerPaneDiscover)
			m.Update(key)
			if !m.ui.palette || !m.ui.pointerModal(m) {
				t.Fatal("palette shortcut did not open a protected modal")
			}
			m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
			if !m.ui.palette || m.ui.paletteSearch.Value() != "q" {
				t.Fatal("typing q escaped the palette")
			}
			m.ui.paletteSearch.SetValue("spkr")
			matches := m.ui.paletteMatches(m)
			if len(matches) != 1 || matches[0].id != "speaker" {
				t.Fatalf("fuzzy query did not match non-contiguous letters: %+v", matches)
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.ui.palette || !m.chooserOpen || m.ui.focus.Scope() != "speaker-chooser" {
				t.Fatal("command did not transfer focus to speaker chooser")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			if !m.ui.focus.Is(playerPaneDiscover) {
				t.Fatal("closing the command's dialog did not restore original focus")
			}
		})
	}
}

func TestPlayerPaletteEmptyAndCompactMouseActivation(t *testing.T) {
	m := playerDialogTestModel(t)
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m.ui.paletteSearch.SetValue("zzzzzz")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.ui.palette || m.chooserOpen || m.ui.settings {
		t.Fatal("empty palette activated a command")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "No matching commands") {
		t.Fatalf("no-match state not rendered: %s", ansi.Strip(m.View().Content))
	}
	m.ui.paletteSearch.SetValue("settings open")
	view := m.View()
	content := ansi.Strip(view.Content)
	for _, label := range []string{"COMMAND PALETTE", "Ctrl+,", "Enter run", "Esc close"} {
		if !strings.Contains(content, label) {
			t.Fatalf("compact palette missing %q: %s", label, content)
		}
	}
	rect, ok := m.ui.root.Frame().Rect("palette-settings")
	if !ok {
		t.Fatal("settings command not visible")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
	if !m.ui.settings || m.ui.palette || m.ui.focus.Scope() != "settings" {
		t.Fatal("mouse command did not transfer to settings")
	}
}

func TestPlayerPaletteRechecksQueueReadinessAndPreservesConfirmation(t *testing.T) {
	m := playerDialogTestModel(t)
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current = playerQueueUIState{
		Generation: 3, Ready: true, Snapshot: &playerQueueSnapshot{
			rows: []kefw2.ContentItem{{Title: "Track"}},
		},
	}
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m.ui.paletteSearch.SetValue("queue clear")
	m.View()
	m.ui.queue.current.Ready = false
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.ui.palette || m.ui.queueClear != nil {
		t.Fatal("stale ready state allowed a queue action")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Queue action unavailable") {
		t.Fatalf("disabled action did not explain its condition: %s", ansi.Strip(m.View().Content))
	}
	m.ui.queue.current.Ready = true
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.ui.palette || m.ui.queueClear == nil || m.ui.queueConfirmAccept {
		t.Fatal("clear command bypassed the cancel-default confirmation")
	}
}

func TestPlayerPaletteDoesNotStealQueueReorderingOrSearchText(t *testing.T) {
	m := playerDialogTestModel(t)
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current = playerQueueUIState{
		Generation: 3, Ready: true, Snapshot: &playerQueueSnapshot{
			rows: []kefw2.ContentItem{{Title: "First"}, {Title: "Second"}},
		},
	}
	m.ui.pane = ModeQueue
	m.ui.focus.Set(ModeQueue)
	m.ui.queueIndex = 1
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	messages := playerLayoutTestMessages(cmd)
	if m.ui.palette || len(messages) != 1 {
		t.Fatal("Ctrl+k was stolen from queue reordering")
	}
	if move, ok := messages[0].(playerQueueMoveMsg); !ok || move.From != 1 || move.To != 0 {
		t.Fatalf("wrong queue move: %#v", messages[0])
	}
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.searchActive = true
	m.ui.search.Focus()
	m.ui.search.SetValue("existing query")
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModShift})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.ui.palette || m.ui.searchActive || m.ui.search.Value() != "existing query" {
		t.Fatal("palette corrupted the discovery query or left its input focused")
	}
}

func TestPlayerPaletteQueuePlayUsesExistingSnapshotMessage(t *testing.T) {
	m := playerDialogTestModel(t)
	snapshot := &playerQueueSnapshot{rows: []kefw2.ContentItem{{Title: "Track"}}}
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current = playerQueueUIState{Generation: 7, Ready: true, Snapshot: snapshot}
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m.ui.paletteSearch.SetValue("queue play")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	messages := playerLayoutTestMessages(cmd)
	if len(messages) != 1 {
		t.Fatalf("queue play produced %d messages; matches=%+v index=%d palette=%t", len(messages), m.ui.paletteMatches(m), m.ui.paletteIndex, m.ui.palette)
	}
	play, ok := messages[0].(playerQueuePlayMsg)
	if !ok || play.Generation != 7 || play.Snapshot != snapshot || play.Index != 0 {
		t.Fatalf("command bypassed existing queue snapshot validation: %#v", messages[0])
	}
}
