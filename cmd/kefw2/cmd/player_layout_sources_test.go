// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

func playerSourcesTestModel(t *testing.T, layout string, width, height int) *playerModel {
	t.Helper()
	m := newPlayerModel(context.Background(), layout, playerSelection{}, nil)
	m.generation = 7
	m.snapshot = &statusSnapshot{Data: statusResponse{Source: kefw2.SourceWiFi}}
	m.ui = newPlayerLayoutUI(layout, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.controls.state = playerControlsUIState{
		ready: true, epoch: 42, supportedSources: playerControlsSourcesForModel("ls60"),
	}
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.generation = 7
	m.ui.pane = playerPaneSources
	m.ui.focus.Set(playerPaneSources)
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func TestPlayerSourcesNavigationContainsInputsServicesAndStandby(t *testing.T) {
	for _, layout := range []string{playerListeningDesk, playerDiscoveryBrowser} {
		t.Run(layout, func(t *testing.T) {
			m := playerSourcesTestModel(t, layout, 160, 44)
			content := ansi.Strip(m.View().Content)
			for _, label := range []string{"Wi-Fi", "Bluetooth", "TV / HDMI", "Optical", "Coaxial", "Aux",
				"Off / Standby", "BROWSE WI-FI", "Radio", "Podcasts", "UPnP library", "* Wi-Fi"} {
				if !strings.Contains(content, label) {
					t.Fatalf("source navigation omitted %q", label)
				}
			}
			rect, ok := m.ui.root.Frame().Rect("sources-list")
			if !ok || rect.Dx() > 18 {
				t.Fatalf("source list is not compact: %v", rect)
			}
			for index, item := range m.ui.sourceNavItems() {
				m.ui.sourceNavIndex = index
				_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				messages := playerLayoutTestMessages(command)
				if !item.enabled || item.source == kefw2.SourceWiFi {
					if len(messages) != 0 {
						t.Fatalf("disabled or current input dispatched: %#v", messages)
					}
				} else {
					assertPlayerSourceNavActivation(t, messages, item, 7, 42)
				}
				m.ui.pane = playerPaneSources
				m.ui.focus.Set(playerPaneSources)
			}
		})
	}
}

func assertPlayerSourceNavActivation(t *testing.T, messages []tea.Msg, item playerSourceNavItem, generation, epoch uint64) {
	t.Helper()
	if len(messages) != 1 {
		t.Fatalf("%s produced %d messages", item.label, len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok || scoped.generation != generation {
		t.Fatalf("%s lost target generation: %#v", item.label, messages[0])
	}
	if item.service != "" {
		request, ok := scoped.message.(playerDiscoveryServiceMsg)
		if !ok || request.generation != generation || request.service != item.service || !request.browseRoot {
			t.Fatalf("browsing dispatched the wrong action: %#v", scoped.message)
		}
		return
	}
	request, ok := scoped.message.(playerSourceRequest)
	if !ok || request.generation != generation || request.epoch != epoch || request.source != item.source {
		t.Fatalf("%s lost its input/epoch: %#v", item.label, scoped.message)
	}
}

func TestPlayerSourcesBrowseWiFiLoadsDiscover(t *testing.T) {
	for _, layout := range []string{playerListeningDesk, playerDiscoveryBrowser} {
		for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast, playerDiscoveryUPnP} {
			for _, mouse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/mouse=%t", layout, service, mouse), func(t *testing.T) {
					root := map[playerDiscoveryService]string{
						playerDiscoveryRadio: "ui:/airableradios", playerDiscoveryPodcast: "airable:linkService_airable.feeds",
						playerDiscoveryUPnP: "ui:/upnp",
					}[service]
					backend := &playerDiscoveryUITestBackend{
						rows: func(_ context.Context, _ playerSpeaker, path string, from, to int) (*kefw2.RowsResponse, error) {
							if path != root || from != 0 || to != playerDiscoveryDefaultPageSize-1 {
								return nil, fmt.Errorf("unexpected browse: %s %d..%d", path, from, to)
							}
							return &kefw2.RowsResponse{RowsCount: 1, Rows: []kefw2.ContentItem{{
								Title: "Browse menu content", Type: kefw2.ContentTypeContainer, Path: "opaque:/category",
							}}}, nil
						},
					}
					m := playerSourcesBrowseTestModel(t, backend, layout, service)
					if mouse {
						m.ui.searchActive = true
						m.ui.search.SetValue("previous search")
						m.ui.search.Focus()
					}
					m.View()
					var command tea.Cmd
					if mouse {
						var ok bool
						command, ok = m.ui.root.Click(fmt.Sprintf("source-nav-%d", m.ui.sourceNavIndex))
						if !ok {
							t.Fatal("browse entry is not clickable")
						}
					} else {
						_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
					}
					messages := playerLayoutTestMessages(command)
					if len(messages) != 1 {
						t.Fatalf("browse activation produced %d messages", len(messages))
					}
					_, command = m.Update(messages[0])
					playerDiscoveryUITestComplete(t, m, command)
					p := m.ui.discovery.presentation()
					if m.ui.pane != playerPaneDiscover || p.service != service || m.ui.searchActive ||
						m.ui.search.Value() != "" ||
						p.state.status != playerDiscoveryReady || len(p.state.page.items) != 1 ||
						!strings.Contains(ansi.Strip(m.View().Content), "Browse menu content") {
						t.Fatalf("browse menu not shown in Discover: %+v", p)
					}
					if calls := backend.calls(); len(calls) != 1 || calls[0].name != "rows" {
						t.Fatalf("browsing did more than read a menu: %+v", calls)
					}
				})
			}
		}
	}
}

func playerSourcesBrowseTestModel(
	t *testing.T, backend *playerDiscoveryUITestBackend, layout string, service playerDiscoveryService,
) *playerModel {
	t.Helper()
	m, discovery, _, _ := playerDiscoveryUITestModel(t, backend)
	m.layout = layout
	m.ui = newPlayerLayoutUI(layout, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = discovery
	m.ui.pane = playerPaneSources
	m.ui.focus.Set(playerPaneSources)
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 44})
	for i, item := range m.ui.sourceNavItems() {
		if item.service == service {
			m.ui.sourceNavIndex = i
		}
	}
	return m
}

func TestPlayerSourcesNavigationKeepsCompactSelectionVisible(t *testing.T) {
	m := playerSourcesTestModel(t, playerListeningDesk, 60, 18)
	m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if !strings.Contains(ansi.Strip(m.View().Content), ">  UPnP library") {
		t.Fatal("End did not scroll the service selection into view")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if !strings.Contains(ansi.Strip(m.View().Content), ">  Podcasts") {
		t.Fatal("Up did not keep the selected service visible")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if !strings.Contains(ansi.Strip(m.View().Content), ">* Wi-Fi") {
		t.Fatal("Home did not restore the current-input row")
	}
}

func TestPlayerSourcesMouseSelectionAndCapturedStandby(t *testing.T) {
	m := playerSourcesTestModel(t, playerListeningDesk, 160, 44)
	index := -1
	for i, item := range m.ui.sourceNavItems() {
		if item.source == kefw2.SourceStandby {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("Off / Standby is missing")
	}
	m.View()
	click, ok := m.ui.root.Click(fmt.Sprintf("source-nav-%d", index))
	if !ok {
		t.Fatal("Off / Standby is not mouse accessible")
	}
	if messages := playerLayoutTestMessages(click); len(messages) != 0 {
		t.Fatalf("selection changed speaker state: %#v", messages)
	}
	m.View()
	m.generation = 8
	m.ui.controls.state.epoch = 43
	click, ok = m.ui.root.Click("source-nav-open")
	if !ok {
		t.Fatal("Open button is not mouse accessible")
	}
	assertPlayerSourceNavActivation(t, playerLayoutTestMessages(click),
		playerSourceNavItem{label: "Off / Standby", source: kefw2.SourceStandby}, 7, 42)
}

func TestPlayerSourcesDisableWritesButAllowBrowsingWhenStale(t *testing.T) {
	m := playerSourcesTestModel(t, playerListeningDesk, 160, 44)
	m.ui.controls.state.ready = false
	for index, item := range m.ui.sourceNavItems() {
		m.ui.pane = playerPaneSources
		m.ui.focus.Set(playerPaneSources)
		m.ui.sourceNavIndex = index
		_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		messages := playerLayoutTestMessages(command)
		if item.service == "" {
			if len(messages) != 0 {
				t.Fatalf("stale input %s dispatched a write: %#v", item.label, messages)
			}
		} else {
			assertPlayerSourceNavActivation(t, messages, item, 7, 42)
		}
	}
}

func TestPlayerSourcesStandbyIsMarkedAndAvailableInChooser(t *testing.T) {
	m := playerSourcesTestModel(t, playerListeningDesk, 160, 44)
	m.snapshot.Data.Power = kefw2.SpeakerStatusStandby
	if !strings.Contains(ansi.Strip(m.View().Content), "* Off / Standby") {
		t.Fatal("confirmed standby was not marked in source navigation")
	}
	m.ui.openSource()
	if !strings.Contains(ansi.Strip(m.View().Content), "Off / Standby") {
		t.Fatal("compact input chooser omitted Off / Standby")
	}
}

func TestPlayerVisualHierarchyFollowsPaneAndSelection(t *testing.T) {
	for _, layout := range []string{playerListeningDesk, playerDiscoveryBrowser} {
		t.Run(layout, func(t *testing.T) {
			m := playerSourcesTestModel(t, layout, 160, 44)
			m.ui.discovery.state.page.items = []kefw2.ContentItem{{Title: "First station"}, {Title: "Second station"}}
			m.ui.discovery.selection = 0
			m.View()
			sourceTitle := playerNodeCells(t, m, "title-sources")
			if !playerSameColor(sourceTitle[0].Style.Fg, playerAccentColor) {
				t.Fatal("active pane title is not accented")
			}
			titleRect, _ := m.ui.root.Frame().Rect("title-sources")
			borderLine := strings.Split(m.View().Content, "\n")[titleRect.Min.Y-1]
			if playerRenderedCells(borderLine)[titleRect.Min.X-1].Content != "╔" {
				t.Fatal("active pane lacks the double-border focus cue")
			}
			row := playerNodeCells(t, m, "source-nav-0")
			if !playerSameColor(row[0].Style.Bg, playerSelectionFill) {
				t.Fatal("active selection lacks a filled highlight")
			}
			m.Update(tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt})
			m.View()
			sourceTitle = playerNodeCells(t, m, "title-sources")
			if playerSameColor(sourceTitle[0].Style.Fg, playerAccentColor) {
				t.Fatal("inactive pane retained active styling")
			}
			row = playerNodeCells(t, m, "discovery-row-0")
			other := playerNodeCells(t, m, "discovery-row-1")
			if !playerSameColor(row[0].Style.Bg, playerSelectionFill) ||
				playerSameColor(row[0].Style.Bg, other[0].Style.Bg) {
				t.Fatal("selected discovery item does not stand out")
			}
			m.ui.discovery.selection = 1
			m.View()
			if !playerSameColor(playerNodeCells(t, m, "discovery-row-1")[0].Style.Bg, playerSelectionFill) {
				t.Fatal("selection highlight did not follow the item")
			}
			help := playerNodeCells(t, m, "help-open")
			if !playerSameColor(help[0].Style.Fg, lipgloss.Color("#fbbf24")) ||
				playerSameColor(help[0].Style.Fg, help[4].Style.Fg) {
				t.Fatal("keyboard hints are not distinguished from their descriptions")
			}
			m.ui.discovery.confirmation = &playerDiscoveryConfirmation{}
			m.View()
			cancel := playerNodeCells(t, m, "discovery-confirm-no")
			if !playerSameColor(cancel[0].Style.Bg, playerAccentColor) {
				t.Fatal("modal compositing erased the selected button highlight")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			m.View()
			if !playerSameColor(playerNodeCells(t, m, "discovery-confirm-yes")[0].Style.Bg, playerAccentColor) {
				t.Fatal("confirmation highlight did not follow keyboard navigation")
			}
		})
	}
}

func playerNodeCells(t *testing.T, m *playerModel, id string) []uv.Cell {
	t.Helper()
	rect, ok := m.ui.root.Frame().Rect(id)
	if !ok || rect.Dx() == 0 || rect.Dy() == 0 {
		t.Fatalf("node %s is missing", id)
	}
	line := strings.Split(m.View().Content, "\n")[rect.Min.Y]
	return playerRenderedCells(line)[rect.Min.X:rect.Max.X]
}

func playerSameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func playerRenderedCells(line string) []uv.Cell {
	parser := ansi.NewParser()
	var state byte
	var style uv.Style
	var cells []uv.Cell
	for len(line) > 0 {
		sequence, width, n, next := ansi.DecodeSequence(line, state, parser)
		state, line = next, line[n:]
		if strings.HasPrefix(sequence, "\x1b[") && strings.HasSuffix(sequence, "m") {
			uv.ReadStyle(parser.Params(), &style)
		}
		for x := 0; x < width; x++ {
			cells = append(cells, uv.Cell{Content: sequence, Width: width, Style: style})
		}
	}
	return cells
}
