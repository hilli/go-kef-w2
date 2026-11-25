// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

func playerDialogTestModel(t *testing.T) *playerModel {
	t.Helper()
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func TestPlayerSettingsCompactLibraryKeepsProgressErrorsAndRecoveryVisible(t *testing.T) {
	for _, failed := range []bool{false, true} {
		m := playerDialogTestModel(t)
		d := newPlayerDiscoveryUI()
		m.ui.discovery = d
		d.service = playerDiscoveryUPnP
		d.upnp = playerUPnPSetup{checked: true, config: playerUPnPConfig{server: "Plex", path: "upnp:/plex"}}
		d.state.hasPage = true
		d.state.page = playerDiscoveryPage{
			kind: playerDiscoveryBrowsePage, path: playerDiscoveryUPnPRootPath,
			items: []kefw2.ContentItem{{Title: "Plex", Type: kefw2.ContentTypeContainer, Path: "upnp:/plex"}},
		}
		if failed {
			d.upnp.err = errors.New("failed to browse media server: context deadline exceeded")
			d.state.err = errors.New("failed to load servers: network connection unavailable")
		} else {
			d.upnp.indexing = true
			d.upnp.progress = &playerUPnPProgress{}
			d.upnp.progress.report(214, 12840, "Music/Jazz")
			d.upnp.elapsed = 83 * time.Second
		}
		m.ui.openSettings()
		m.ui.settingsLibrary = true
		m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
		content := ansi.Strip(m.View().Content)
		content = strings.Join(strings.Fields(strings.ReplaceAll(content, "│", "")), " ")
		wants := []string{"[I Build/refresh]", "[x Cancel]", "[r Reload]", "Esc back"}
		if failed {
			wants = append(wants, "context deadline exceeded", "network connection unavailable")
		} else {
			wants = append(wants, "12,840 tracks", "214 folders", "1:23 elapsed")
		}
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("failed=%t: compact library lost %q: %s", failed, want, content)
			}
		}
	}
}

func TestPlayerSettingsFocusIsolationAndToggle(t *testing.T) {
	m := playerDialogTestModel(t)
	m.ui.focus.Set(playerPaneDiscover)
	m.Update(tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl})
	if !m.ui.settings || !m.ui.pointerModal(m) {
		t.Fatal("settings shortcut did not open a modal")
	}
	for _, key := range []rune{'q', 'm', '+', 'S', '/'} {
		_, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		if len(playerLayoutTestMessages(cmd)) != 0 || m.chooserOpen || m.ui.searchActive {
			t.Fatalf("settings leaked %q into the player", key)
		}
	}
	for i := 0; i < 4; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.ui.reduced || !m.ui.settings {
		t.Fatal("reduced motion did not toggle in place")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.ui.settings || !m.ui.focus.Is(playerPaneDiscover) {
		t.Fatal("closing settings did not restore pane focus")
	}
}

func TestPlayerSettingsMouseAndCompactView(t *testing.T) {
	m := playerDialogTestModel(t)
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("settings-open")
	if !ok {
		t.Fatal("settings entry point missing")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
	if !m.ui.settings {
		t.Fatal("click did not open settings")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	content := ansi.Strip(m.View().Content)
	for _, label := range []string{"SETTINGS", "Choose speaker", "Choose input", "UPnP library", "Reduced motion", "Esc close"} {
		if !strings.Contains(content, label) {
			t.Fatalf("compact settings missing %q: %s", label, content)
		}
	}
	m.ui.settingsIndex = 0
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chooserOpen || m.ui.settings {
		t.Fatal("speaker setting did not transfer to speaker chooser")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.ui.focus.Scope() != nil {
		t.Fatal("speaker chooser left an orphaned settings focus scope")
	}
}

func TestPlayerSettingsUPnPUsesExistingConfigurationAndIndexMessages(t *testing.T) {
	server := kefw2.ContentItem{Title: "Plex", Type: kefw2.ContentTypeContainer, Path: "upnp:/plex"}
	lib := &playerUPnPTestLibrary{}
	m, d := playerUPnPTestModel(t, lib, []kefw2.ContentItem{server})
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = d
	m.addExtension(m.ui.extension())
	m.ui.openSettings()
	m.ui.settingsIndex = 2
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, message := range playerLayoutTestMessages(cmd) {
		_, read := m.Update(message)
		playerDiscoveryUITestComplete(t, m, read)
	}
	if !m.ui.settingsLibrary || !m.ui.focus.Is("settings-server-0") {
		t.Fatal("library setting did not open server selection")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	if content := ansi.Strip(m.View().Content); !strings.Contains(content, "Plex") ||
		!strings.Contains(content, "[I Build/refresh]") {
		t.Fatalf("compact library controls not visible: %s", content)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, message := range playerLayoutTestMessages(cmd) {
		m.Update(message)
	}
	if d.upnp.config.path != server.Path {
		t.Fatal("server selection did not persist through existing library boundary")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	messages := playerLayoutTestMessages(cmd)
	if len(messages) != 1 {
		t.Fatalf("index produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok {
		t.Fatalf("index command was not scoped: %#v", messages[0])
	}
	if _, ok := scoped.message.(playerUPnPIndexMsg); !ok {
		t.Fatalf("wrong index message: %#v", scoped.message)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.ui.settings || m.ui.settingsLibrary {
		t.Fatal("Esc did not return to main settings")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.ui.settings {
		t.Fatal("Esc did not close main settings")
	}
}
