// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestPlayerLayoutPersistenceIsolationAndInvalidFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player-layout.yaml")
	desk := playerDefaultArrangement(playerListeningDesk)
	desk.Order[0], desk.Order[1] = desk.Order[1], desk.Order[0]
	desk.Width["sources"] = 30
	if err := playerSaveLayouts(path, playerListeningDesk, desk, false); err != nil {
		t.Fatal(err)
	}
	browser := playerDefaultArrangement(playerDiscoveryBrowser)
	browser.Width["details"] = 50
	if err := playerSaveLayouts(path, playerDiscoveryBrowser, browser, false); err != nil {
		t.Fatal(err)
	}
	file, err := playerReadLayouts(path)
	if err != nil || !reflect.DeepEqual(file.Layouts[playerListeningDesk], desk) ||
		!reflect.DeepEqual(file.Layouts[playerDiscoveryBrowser], browser) {
		t.Fatalf("arrangements not isolated: %+v, %v", file, err)
	}
	for _, invalid := range []string{
		"version: 99\nlayouts: {}\n",
		"version: 1\nlayouts:\n  listening-desk:\n    order: [sources, sources, queue]\n    width: {sources: 20, discover: 40, queue: 30}\n",
		"version: [\n",
	} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if err := playerSaveLayouts(path, playerListeningDesk, desk, false); err == nil {
			t.Fatal("invalid layout was silently overwritten")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != invalid {
			t.Fatalf("invalid data changed: %q, %v", data, err)
		}
	}
	if err := playerSaveLayouts(path, playerListeningDesk, playerDefaultArrangement(playerListeningDesk), true); err != nil {
		t.Fatalf("explicit reset failed: %v", err)
	}
}

func TestPlayerLayoutWriteFailureAndMoveCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocker")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	ui := newPlayerLayoutUI(playerListeningDesk, filepath.Join(path, "kefw2.yaml"))
	before := playerCopyArrangement(ui.arrangement)
	ui.pane = "sources"
	ui.startMode("move")
	ui.adjust(1)
	ui.cancel()
	if !reflect.DeepEqual(ui.arrangement, before) {
		t.Fatal("cancel changed saved arrangement")
	}
	ui.startMode("resize")
	ui.adjust(1)
	ui.arrangement = playerCopyArrangement(ui.preview)
	ui.cancel()
	ui.save(false)
	if ui.error == nil {
		t.Fatal("write failure was not displayed")
	}
}

func TestPlayerLayoutResetPreservesOtherValidArrangement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player-layout.yaml")
	data := `version: 1
layouts:
  listening-desk:
    order: [sources, sources, queue]
    width: {sources: 24, discover: 42, queue: 32}
  discovery-browser:
    order: [discover, sources, details]
    width: {sources: 22, discover: 50, details: 40}
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := playerSaveLayouts(path, playerListeningDesk, playerDefaultArrangement(playerListeningDesk), true); err != nil {
		t.Fatal(err)
	}
	file, err := playerReadLayouts(path)
	want := playerDefaultArrangement(playerDiscoveryBrowser)
	want.Order[0], want.Order[1] = want.Order[1], want.Order[0]
	want.Width[playerPaneDiscover] = 50
	want.Width[playerPaneSources] = 22
	if err != nil || !reflect.DeepEqual(file.Layouts[playerDiscoveryBrowser], want) {
		t.Fatalf("reset lost other valid arrangement: %+v, %v", file, err)
	}
}

func TestPlayerPhysicalInputExternalAudioLabel(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	for _, source := range []kefw2.Source{
		kefw2.SourceTV, kefw2.SourceOptical, kefw2.SourceCoaxial, kefw2.SourceAux, kefw2.SourceUSB,
	} {
		m.snapshot = &statusSnapshot{Data: statusResponse{Source: source}}
		if !strings.Contains(m.View().Content, "INPUT "+string(source)+" / external audio") {
			t.Errorf("missing external audio label for %s", source)
		}
	}
	m.snapshot = &statusSnapshot{Data: statusResponse{Source: kefw2.SourceOptical, Playback: &playbackStatus{}}}
	if !strings.Contains(m.View().Content, "INPUT optical / external audio") {
		t.Error("physical input without track metadata did not show external audio")
	}
	m.snapshot = &statusSnapshot{Data: statusResponse{Source: kefw2.SourceWiFi}}
	if strings.Contains(m.View().Content, "external audio") {
		t.Error("Wi-Fi was mislabeled as external audio")
	}
}

func TestPlayerChooserFilterAndEscape(t *testing.T) {
	model := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{
		configured: []playerSpeaker{
			{Name: "Twin", Model: "LS60", IPAddress: "10.0.0.1"},
			{Name: "Twin", Model: "LSX", IPAddress: "10.0.0.2"},
			{Name: "Study", Model: "Q", IPAddress: "10.0.0.3"},
		}, choose: true,
	}, nil)
	for _, r := range "LSX" {
		model.handleChooserKey(tea.KeyPressMsg{Code: r})
	}

	if got := model.chooserMatches(); !reflect.DeepEqual(got, []int{1}) || model.chooserIndex != 1 {
		t.Fatalf("model filter: matches %v, index %d", got, model.chooserIndex)
	}
	lines, rows := playerChooserLines(model)
	if !reflect.DeepEqual(rows, []int{1}) || !strings.Contains(strings.Join(lines, "\n"), "10.0.0.2") {
		t.Fatalf("filtered chooser did not disambiguate IP: %v", lines)
	}
	model.handleChooserKey(tea.KeyPressMsg{Code: tea.KeyEsc})
	if model.chooserOpen || model.target != nil {
		t.Fatal("cancel changed selected speaker")
	}
	model.openSpeakerChooser()
	for _, r := range "qS" {
		model.handleKey(tea.KeyPressMsg{Code: r})
	}
	if !model.chooserOpen || model.chooserFilter != "qS" {
		t.Fatal("printable shortcuts escaped the chooser")
	}
}

func TestPlayerLayoutsRenderAndKeepCollapsedPanesReachable(t *testing.T) {
	for _, layout := range []string{playerListeningDesk, playerDiscoveryBrowser} {
		t.Run(layout, func(t *testing.T) {
			m := newPlayerModel(context.Background(), layout, playerSelection{}, nil)
			m.ui = newPlayerLayoutUI(layout, filepath.Join(t.TempDir(), "kefw2.yaml"))
			m.addExtension(m.ui.extension())
			for _, size := range []struct {
				w, h int
				want string
			}{
				{140, 40, "DISCOVER"}, {140, 24, "DISCOVER"}, {100, 30, "DISCOVER"},
				{80, 40, "DISCOVER"}, {80, 24, "DISCOVER"}, {59, 40, "Terminal too small"},
				{140, 17, "Terminal too small"}, {140, 1, "Terminal too small"},
			} {
				m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
				view := m.View()
				if !strings.Contains(view.Content, size.want) {
					t.Fatalf("%dx%d missing %q in %q", size.w, size.h, size.want, view.Content)
				}
			}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.Update(tea.KeyPressMsg{Code: 'Q'})
			if !strings.Contains(m.View().Content, "QUEUE") {
				t.Fatal("collapsed queue is unreachable")
			}
			m.Update(tea.KeyPressMsg{Code: 'i'})
			if !strings.Contains(m.View().Content, "DETAILS") {
				t.Fatal("collapsed details is unreachable")
			}
		})
	}
}

func TestPlayerLayoutKeyboardPreviewAndZoom(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	before := playerCopyArrangement(m.ui.arrangement)
	for _, key := range []rune{'p', 'm'} {
		m.Update(tea.KeyPressMsg{Code: key})
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if reflect.DeepEqual(m.ui.preview.Order, before.Order) {
		t.Fatal("move preview did not change")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !reflect.DeepEqual(m.ui.arrangement, before) {
		t.Fatal("escape committed preview")
	}
	m.Update(tea.KeyPressMsg{Code: 'z'})
	if m.ui.zoom != m.ui.pane {
		t.Fatal("zoom did not select the focused pane")
	}
	m.Update(tea.KeyPressMsg{Code: 'z'})
	if m.ui.zoom != "" || !reflect.DeepEqual(m.ui.arrangement, before) {
		t.Fatal("unzoom did not restore the arrangement")
	}
	if _, err := os.Stat(m.ui.path); !os.IsNotExist(err) {
		t.Fatal("canceled layout operation wrote a file")
	}
}

func TestPlayerLayoutResizeDuringDragCancels(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("divider-sources")
	if !ok {
		t.Fatal("missing sources divider")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y + 1, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	move := tea.MouseMotionMsg{X: rect.Min.X + 5, Y: rect.Min.Y + 1, Button: tea.MouseLeft}
	m.View().OnMouse(move)
	m.Update(move)
	if m.ui.drag != "resize" {
		t.Fatalf("divider did not start resize: %q", m.ui.drag)
	}
	before := playerCopyArrangement(m.ui.arrangement)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	view = m.View()
	release := tea.MouseReleaseMsg{X: rect.Min.X + 5, Y: rect.Min.Y + 1, Button: tea.MouseLeft}
	view.OnMouse(release)
	m.Update(release)
	if !reflect.DeepEqual(m.ui.arrangement, before) {
		t.Fatal("resizing terminal committed an in-progress drag")
	}
	if _, err := os.Stat(m.ui.path); !os.IsNotExist(err) {
		t.Fatal("canceled drag wrote the layout")
	}
}

func TestPlayerSearchCapturesShortcutCharacters(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.addExtension(m.ui.extension())
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"}, {Code: 's', Text: "S", Mod: tea.ModShift},
		{Code: tea.KeySpace, Text: " "}, {Code: 'm', Text: "m"},
	} {
		m.Update(key)
	}
	if got := m.ui.search.Value(); got != "qS m" || !m.ui.searchActive || m.chooserOpen {
		t.Fatalf("shortcuts escaped the search field: value %q active %t chooser %t",
			got, m.ui.searchActive, m.chooserOpen)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.ui.searchActive || m.ui.search.Value() != "qS m" {
		t.Fatal("escape did not preserve the unsubmitted search text")
	}
}

func TestPlayerLayoutLostReleaseCancelsInsteadOfCommitting(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("divider-sources")
	if !ok {
		t.Fatal("missing divider")
	}

	before := playerCopyArrangement(m.ui.arrangement)
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y + 1, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	move := tea.MouseMotionMsg{X: rect.Min.X + 8, Y: rect.Min.Y + 1, Button: tea.MouseLeft}
	m.View().OnMouse(move)
	m.Update(move)
	lost := tea.MouseMotionMsg{X: rect.Min.X + 8, Y: rect.Min.Y + 1, Button: tea.MouseNone}
	m.View().OnMouse(lost)
	m.Update(lost)
	if !reflect.DeepEqual(before, m.ui.arrangement) {
		t.Fatal("lost release committed resize")
	}
}

func TestPlayerDiscoveryRadioConfirmationCapturesTargetAndGeneration(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 7
	ui := newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	ui.discovery = newPlayerDiscoveryUI()
	ui.discovery.generation = 7
	request := playerDiscoveryRequestID{generation: 7, sequence: 4}
	ui.discovery.confirmation = &playerDiscoveryConfirmation{
		target: playerSpeaker{Name: "Living", IPAddress: "10.0.0.1"},
		action: "play radio", item: kefw2.ContentItem{Title: "Station"}, clearsQueue: true, request: request,
	}
	m.ui = ui
	m.addExtension(ui.extension())
	view := m.View()
	if !strings.Contains(view.Content, "clears the active queue") || !strings.Contains(view.Content, "10.0.0.1") {
		t.Fatalf("radio confirmation omitted target or warning: %q", view.Content)
	}
	_, command := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("confirmation produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok || scoped.generation != 7 {
		t.Fatalf("confirmation was not scoped: %#v", messages[0])
	}
	confirm, ok := scoped.message.(playerDiscoveryConfirmMsg)
	if !ok || !confirm.accept || confirm.generation != 7 || confirm.request != request {
		t.Fatalf("confirmation did not retain the target request: %#v", scoped.message)
	}
}

func TestPlayerCompactRadioConfirmationMouseRetainsRequest(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 7
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.generation = 7
	request := playerDiscoveryRequestID{generation: 7, sequence: 4}
	m.ui.discovery.confirmation = &playerDiscoveryConfirmation{
		target: playerSpeaker{Name: "Living", IPAddress: "10.0.0.1"},
		action: "play radio", item: kefw2.ContentItem{Title: "Station"}, clearsQueue: true, request: request,
	}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("discovery-confirm-yes")
	if !ok {
		t.Fatal("confirmation button not visible")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	command := m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("confirmation click produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok {
		t.Fatalf("confirmation was not scoped: %#v", messages[0])
	}
	confirmation, ok := scoped.message.(playerDiscoveryConfirmMsg)
	if !ok || !confirmation.accept || confirmation.request != request || confirmation.generation != 7 {
		t.Fatalf("confirmation click lost captured request: %#v", scoped.message)
	}
}

func TestPlayerConfirmationKeyboardNavigation(t *testing.T) {
	for _, layout := range []string{playerListeningDesk, playerDiscoveryBrowser} {
		for _, kind := range []string{"radio", "queue"} {
			for _, test := range []struct {
				name   string
				keys   []tea.KeyPressMsg
				final  tea.KeyPressMsg
				accept bool
			}{
				{name: "default-cancel"},
				{name: "tab-confirm", keys: []tea.KeyPressMsg{{Code: tea.KeyTab}}, accept: true},
				{name: "shift-tab-confirm", keys: []tea.KeyPressMsg{{Code: tea.KeyTab, Mod: tea.ModShift}}, accept: true},
				{name: "tab-wrap-cancel", keys: []tea.KeyPressMsg{{Code: tea.KeyTab}, {Code: tea.KeyTab}}},
				{name: "left-confirm", keys: []tea.KeyPressMsg{{Code: tea.KeyLeft}}, accept: true},
				{name: "right-cancel", keys: []tea.KeyPressMsg{{Code: tea.KeyLeft}, {Code: tea.KeyRight}}},
				{name: "y-confirms", final: tea.KeyPressMsg{Code: 'y', Text: "y"}, accept: true},
				{name: "n-cancels", keys: []tea.KeyPressMsg{{Code: tea.KeyLeft}}, final: tea.KeyPressMsg{Code: 'n', Text: "n"}},
				{name: "escape-cancels", keys: []tea.KeyPressMsg{{Code: tea.KeyLeft}}, final: tea.KeyPressMsg{Code: tea.KeyEsc}},
			} {
				t.Run(layout+"/"+kind+"/"+test.name, func(t *testing.T) {
					m := newPlayerModel(context.Background(), layout, playerSelection{}, nil)
					m.generation = 7
					m.ui = newPlayerLayoutUI(layout, filepath.Join(t.TempDir(), "kefw2.yaml"))
					m.addExtension(m.ui.extension())
					request := playerDiscoveryRequestID{generation: 7, sequence: 4}
					snapshot := &playerQueueSnapshot{}
					if kind == "radio" {
						m.ui.discovery = newPlayerDiscoveryUI()
						m.ui.discovery.generation = 7
						m.ui.discovery.confirmation = &playerDiscoveryConfirmation{
							request: request, item: kefw2.ContentItem{Title: "Station"}, clearsQueue: true,
						}
					} else {
						m.ui.openQueueClear(playerQueueClearMsg{Generation: 7, Snapshot: snapshot})
						m.ui.searchActive = true
						m.ui.search.Focus()
					}
					m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
					if !strings.Contains(m.View().Content, ">[Cancel]") {
						t.Fatal("dialog did not visibly default to Cancel")
					}
					for _, key := range test.keys {
						_, command := m.Update(key)
						if messages := playerLayoutTestMessages(command); len(messages) != 0 {
							t.Fatalf("navigation dispatched an action: %#v", messages)
						}
					}
					label := ">[Cancel]"
					if len(test.keys) > 0 && (test.keys[len(test.keys)-1].Code == tea.KeyLeft ||
						test.name == "tab-confirm" || test.name == "shift-tab-confirm") {
						label = ">[Confirm]"
					}
					if !strings.Contains(m.View().Content, label) {
						t.Fatalf("selected button %q was not visible", label)
					}
					final := test.final
					if final.Code == 0 {
						final = tea.KeyPressMsg{Code: tea.KeyEnter}
					}
					_, command := m.Update(final)
					messages := playerLayoutTestMessages(command)
					assertPlayerConfirmationMessages(t, m, messages, kind, test.accept, request, snapshot)
				})
			}
		}
	}
}

func assertPlayerConfirmationMessages(t *testing.T, m *playerModel, messages []tea.Msg, kind string,
	accept bool, request playerDiscoveryRequestID, snapshot *playerQueueSnapshot,
) {
	t.Helper()
	if kind == "queue" && !accept {
		if len(messages) != 0 || m.ui.queueClear != nil {
			t.Fatal("Cancel dispatched a queue write or left the dialog open")
		}
		return
	}
	if len(messages) != 1 {
		t.Fatalf("activation produced %d messages", len(messages))
	}
	if kind == "radio" {
		scoped, ok := messages[0].(playerScopedMsg)
		if !ok || scoped.generation != 7 {
			t.Fatalf("confirmation lost generation: %#v", messages[0])
		}
		confirm, ok := scoped.message.(playerDiscoveryConfirmMsg)
		if !ok || confirm.request != request || confirm.accept != accept {
			t.Fatalf("wrong confirmation: %#v", scoped.message)
		}
		return
	}
	clearRequest, ok := messages[0].(playerQueueClearMsg)
	if !ok || !clearRequest.Confirmed || clearRequest.Generation != 7 || clearRequest.Snapshot != snapshot {
		t.Fatalf("queue confirmation lost snapshot: %#v", messages[0])
	}
}

func TestPlayerNewDiscoveryConfirmationResetsToCancel(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.addExtension(m.ui.extension())
	m.ui.discovery.confirmation = &playerDiscoveryConfirmation{}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if !strings.Contains(m.View().Content, ">[Confirm]") {
		t.Fatal("Tab did not select Confirm")
	}
	m.ui.discovery.confirmation = &playerDiscoveryConfirmation{}
	if !strings.Contains(m.View().Content, ">[Cancel]") {
		t.Fatal("new confirmation inherited the previous Confirm selection")
	}
}

func playerLayoutTestMessages(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	msg := command()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, next := range batch {
			messages = append(messages, playerLayoutTestMessages(next)...)
		}
		return messages
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func TestPlayerQueueKeyboardUsesRenderedSnapshot(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 9
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.queue = newPlayerQueueUI()
	snapshot := &playerQueueSnapshot{rows: []kefw2.ContentItem{{Title: "one"}, {Title: "two"}}}
	m.ui.queue.current = playerQueueUIState{Generation: 9, Snapshot: snapshot, Ready: true}
	m.ui.pane = "queue"
	m.ui.focus.Set(ModeQueue)
	m.addExtension(m.ui.extension())
	_, command := m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("reorder produced %d messages", len(messages))
	}
	move, ok := messages[0].(playerQueueMoveMsg)
	if !ok || move.Generation != 9 || move.Snapshot != snapshot || move.From != 0 || move.To != 1 {
		t.Fatalf("reorder lost its rendered snapshot: %#v", messages[0])
	}
	m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if m.ui.queueClear == nil || m.ui.queueClear.Confirmed {
		t.Fatal("clear did not request explicit confirmation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.ui.queueClear != nil {
		t.Fatal("escape did not cancel queue clear")
	}
}

func TestPlayerMuteKeyUsesCapturedGeneration(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 12
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.controls.state = playerControlsUIState{ready: true, epoch: 42, confirmed: &statusSnapshot{
		Data: statusResponse{Power: kefw2.SpeakerStatusOn, Source: kefw2.SourceWiFi},
	}}
	m.addExtension(m.ui.extension())
	_, command := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m.ui.controls.state.epoch = 43
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("mute produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok || scoped.generation != 12 {
		t.Fatalf("mute was not scoped: %#v", messages[0])
	}
	mute, ok := scoped.message.(playerMuteRequest)
	if !ok || mute.generation != 12 || mute.epoch != 42 {
		t.Fatalf("mute lost generation: %#v", scoped.message)
	}
}

func TestPlayerSourceChooserVisibleBeforeKeyboardAndMouseSwitch(t *testing.T) {
	for _, test := range []struct {
		method string
		width  int
	}{
		{"keyboard", 80}, {"mouse", 80}, {"keyboard", 140}, {"mouse", 140},
	} {
		t.Run(fmt.Sprintf("%s-%d", test.method, test.width), func(t *testing.T) {
			target := playerSpeaker{Name: "Office", IPAddress: "office"}
			m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{selected: &target}, nil)
			m.generation = 4
			m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
			m.ui.controls = newPlayerControlsUI()
			m.ui.controls.state.ready = true
			m.ui.controls.state.epoch = 42
			m.ui.controls.state.supportedSources = []kefw2.Source{kefw2.SourceWiFi}
			m.addExtension(m.ui.extension())
			height := 40
			if test.width == 80 {
				height = 24
			}
			m.Update(tea.WindowSizeMsg{Width: test.width, Height: height})
			_, command := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
			if len(playerLayoutTestMessages(command)) != 0 {
				t.Fatal("opening chooser dispatched an action")
			}
			view := m.View()
			if !strings.Contains(view.Content, "Choose input for Office (office)") {
				t.Fatalf("source chooser not visible: %q", view.Content)
			}
			if _, ok := m.ui.root.Frame().Rect("source-chooser"); !ok {
				t.Fatal("source chooser overlay not in rendered frame")
			}
			if test.method == "keyboard" {
				_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				rect, ok := m.ui.root.Frame().Rect("source-switch")
				if !ok {
					t.Fatal("source switch button not in rendered frame")
				}
				press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
				view.OnMouse(press)
				m.Update(press)
				command = m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
			}
			m.ui.controls.state.epoch = 43
			messages := playerLayoutTestMessages(command)
			if len(messages) != 1 {
				t.Fatalf("source switch produced %d messages", len(messages))
			}
			scoped, ok := messages[0].(playerScopedMsg)
			if !ok || scoped.generation != 4 {
				t.Fatalf("source switch was not scoped: %#v", messages[0])
			}
			if request, ok := scoped.message.(playerSourceRequest); !ok || request.source != kefw2.SourceWiFi ||
				request.epoch != 42 {
				t.Fatalf("wrong source request: %#v", scoped.message)
			}
		})
	}
}

func TestPlayerModalsHaveOpaqueBoundedFrames(t *testing.T) {
	for _, size := range []struct {
		width, height int
		layout        string
	}{
		{60, 18, playerListeningDesk}, {80, 24, playerListeningDesk}, {160, 44, playerListeningDesk},
		{60, 18, playerDiscoveryBrowser}, {80, 24, playerDiscoveryBrowser}, {160, 44, playerDiscoveryBrowser},
	} {
		for _, kind := range []string{"speaker", "source", "queue-clear", "discovery-confirm", "help", "pane-menu"} {
			t.Run(fmt.Sprintf("%s-%s-%d", kind, size.layout, size.width), func(t *testing.T) {
				target := playerSpeaker{Name: "Living", IPAddress: "10.0.0.1"}
				selection := playerSelection{selected: &target, configured: []playerSpeaker{target, {
					Name: "Office", IPAddress: "10.0.0.2",
				}}}
				m := newPlayerModel(context.Background(), size.layout, selection, nil)
				m.ui = newPlayerLayoutUI(size.layout, filepath.Join(t.TempDir(), "kefw2.yaml"))
				m.ui.controls = newPlayerControlsUI()
				m.ui.controls.state = playerControlsUIState{ready: true, epoch: 1, supportedSources: []kefw2.Source{kefw2.SourceWiFi}}
				m.ui.queue = newPlayerQueueUI()
				m.ui.queue.current.Ready = true
				m.ui.discovery = newPlayerDiscoveryUI()
				m.addExtension(m.ui.extension())
				m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				id := kind
				switch kind {
				case "speaker":
					id = "speaker-chooser"
					m.openSpeakerChooser()
				case "source":
					id = "source-chooser"
					m.ui.openSource()
				case "queue-clear":
					m.ui.openQueueClear(playerQueueClearMsg{})
				case "discovery-confirm":
					m.ui.discovery.confirmation = &playerDiscoveryConfirmation{
						action: "play radio", target: target, item: kefw2.ContentItem{Title: "Station"}, clearsQueue: true,
					}
				case "help":
					m.ui.help = true
				case "pane-menu":
					m.ui.menu = true
				}
				assertPlayerModalFrame(t, m, id, kind)
			})
		}
	}
}

func assertPlayerModalFrame(t *testing.T, m *playerModel, id, kind string) {
	t.Helper()
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect(id)
	if !ok {
		t.Fatalf("modal %q missing from frame", id)
	}
	lines := strings.Split(view.Content, "\n")
	if rect.Min.Y < 0 || rect.Max.Y > len(lines) || rect.Dx() < 3 || rect.Dy() < 3 {
		t.Fatalf("invalid modal bounds %v", rect)
	}
	cell := func(x, y int) string { return ansi.Strip(ansi.Cut(lines[y], x, x+1)) }
	if cell(rect.Min.X, rect.Min.Y) != "╭" || cell(rect.Max.X-1, rect.Min.Y) != "╮" {
		t.Fatalf("%s modal is not visibly bounded: %q", kind, ansi.Strip(lines[rect.Min.Y]))
	}
	for y := rect.Min.Y + 1; y < rect.Max.Y-1; y++ {
		if cell(rect.Min.X, y) != "│" || cell(rect.Max.X-1, y) != "│" {
			t.Fatalf("%s modal content blends with background: %q", kind, ansi.Strip(lines[y]))
		}
		cells := playerRenderedCells(lines[y])
		for x := rect.Min.X + 1; x < rect.Max.X-1; x++ {
			cell := cells[x]
			if cell.Style.Fg == nil || cell.Style.Bg == nil ||
				playerSameColor(cell.Style.Fg, cell.Style.Bg) {
				t.Fatalf("%s modal cell %d,%d lacks opaque contrasting colors: %#v", kind, x, y, cell)
			}
		}
	}
	rowID, want := "", ""
	switch kind {
	case "speaker":
		rowID, want = "speaker-1", strings.TrimSpace(playerChooserRow(m, 1))
	case "source":
		rowID, want = "source-0", "> wifi"
	}
	if rowID != "" {
		row, ok := m.ui.root.Frame().Rect(rowID)
		if !ok {
			t.Fatalf("%s modal row missing", kind)
		}
		inside := ansi.Strip(ansi.Cut(lines[row.Min.Y], rect.Min.X+1, rect.Max.X-1))
		if strings.TrimSpace(inside) != want {
			t.Fatalf("%s modal contains background text: %q, want %q", kind, inside, want)
		}
	}
}

func TestPlayerSpeakerChooserFitsManySpeakersAtCompactHeight(t *testing.T) {
	var speakers []playerSpeaker
	for i := 0; i < 30; i++ {
		speakers = append(speakers, playerSpeaker{Name: fmt.Sprintf("Speaker %02d", i), IPAddress: fmt.Sprint(i)})
	}
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{configured: speakers}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.openSpeakerChooser()
	m.chooserIndex = len(speakers) - 1
	content := m.View().Content
	rect, ok := m.ui.root.Frame().Rect("speaker-chooser")
	selected, selectedOK := m.ui.root.Frame().Rect("speaker-29")
	if !ok || !selectedOK || rect.Min.Y < 0 || rect.Max.Y > m.height || selected.Max.Y >= rect.Max.Y {
		t.Fatalf("selected speaker or modal frame clipped: modal %v selected %v", rect, selected)
	}
	if !strings.Contains(content, "Speaker 29") {
		t.Fatal("selected speaker was not visible")
	}
}

func TestPlayerSourceChooserClosesWhenReadinessEpochChanges(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.controls.state = playerControlsUIState{ready: true, epoch: 42, supportedSources: []kefw2.Source{kefw2.SourceWiFi}}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m.ui.controls.state.epoch = 43
	if strings.Contains(m.View().Content, "Choose input for") {
		t.Fatal("stale source chooser remained visible after readiness changed")
	}
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.ui.sourceOpen || len(playerLayoutTestMessages(command)) != 0 {
		t.Fatal("stale source chooser accepted a source request")
	}
}

func TestPlayerConfirmedControlReadbackDrivesStatus(t *testing.T) {
	h := newPlayerControlsUITestHarness(t)
	m := h.model
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = h.ui
	m.addExtension(m.ui.extension())
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	before := playerControlsUITestSnapshot(16)
	before.Data.Source = kefw2.SourceBluetooth
	h.status(before)
	h.maximum(40, nil)
	generation := m.generation
	h.send(playerSourceRequest{generation: generation, source: kefw2.SourceWiFi})
	call := h.call(t, "source")
	pending := m.View().Content
	if !strings.Contains(pending, "INPUT bluetooth | VOLUME 16 | MUTE false") ||
		!strings.Contains(pending, "Pending:") {
		t.Fatalf("pending source did not preserve confirmed status: %q", pending)
	}
	after := playerControlsUITestSnapshot(16)
	after.Data.Playback.Track = &trackStatus{Title: "New track", Artist: "New artist"}
	call.reply <- playerControlsUITestReply{snapshot: after}
	h.until(t, func() bool {
		state := h.ui.State()
		return !state.pending && state.confirmed != nil && state.confirmed.Data.Source == kefw2.SourceWiFi
	})
	h.send(playerMuteRequest{generation: generation})
	h.call(t, "mute").reply <- playerControlsUITestReply{muted: true}
	h.until(t, func() bool { return !h.ui.State().pending && h.ui.State().muted })
	h.send(playerVolumeTargetRequest{generation: generation, target: 17, release: true})
	h.call(t, "volume").reply <- playerControlsUITestReply{confirmed: 17}
	h.until(t, func() bool { return h.ui.State().volume == 17 && !h.ui.State().volumePending })
	content := m.View().Content
	for _, want := range []string{"INPUT wifi | VOLUME 17 | MUTE true", "Track: New track"} {
		if !strings.Contains(content, want) {
			t.Fatalf("confirmed control result %q not rendered without another event: %q", want, content)
		}
	}
	if m.snapshot.Data.Source != kefw2.SourceBluetooth || m.snapshot.Data.Volume != 16 || m.snapshot.Data.Muted {
		t.Fatal("test unexpectedly received a new core status event")
	}
	h.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	h.ui.suspend()
	h.ui.reap()
	if !strings.Contains(m.View().Content, "INPUT wifi | VOLUME 17 | MUTE true") {
		t.Fatal("latest confirmed status disappeared after the controls session stopped")
	}
	m.generation++
	if strings.Contains(m.View().Content, "INPUT wifi | VOLUME 17") {
		t.Fatal("display leaked the previous generation's control snapshot")
	}
}

func TestPlayerCompactHeaderKeepsConnectionWithUnicodeName(t *testing.T) {
	target := playerSpeaker{Name: strings.Repeat("長い名前🎵", 16), IPAddress: "192.0.2.123", Configured: true}
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{selected: &target}, nil)
	m.connection = playerReady
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	line := strings.Split(m.View().Content, "\n")[0]
	if !strings.Contains(line, " | Ready") || !strings.Contains(line, "[Queue]") {
		t.Fatalf("compact header lost connection state or actions: %q", line)
	}
}

func TestPlayerLongUnicodeSpeakerIdentityRemainsVisible(t *testing.T) {
	for _, test := range []struct {
		width, height int
		first, second string
	}{
		{60, 18, "192.0.2.11", "192.0.2.22"},
		{80, 24, "192.0.2.11", "192.0.2.22"},
		{60, 18, "2001:db8:abcd:1234:5678:9abc:def0:1111", "2001:db8:abcd:1234:5678:9abc:def0:2222"},
	} {
		t.Run(fmt.Sprintf("%dx%d-%s", test.width, test.height, test.first), func(t *testing.T) {
			assertPlayerLongUnicodeSpeakerIdentity(t, test.width, test.height, test.first, test.second)
		})
	}
}

func assertPlayerLongUnicodeSpeakerIdentity(t *testing.T, width, height int, firstIP, secondIP string) {
	t.Helper()
	name := strings.Repeat("同じ名前🎧", 20)
	first := playerSpeaker{Name: name, IPAddress: firstIP, Configured: true}
	second := playerSpeaker{Name: name, IPAddress: secondIP, Configured: true}
	m := newPlayerModel(context.Background(), playerListeningDesk,
		playerSelection{selected: &first, configured: []playerSpeaker{first, second}}, nil)
	m.connection = playerReady
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.controls.state = playerControlsUIState{ready: true, epoch: 1}
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current = playerQueueUIState{Ready: true, Target: second}
	m.ui.discovery = newPlayerDiscoveryUI()
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	header := strings.Split(m.View().Content, "\n")
	if !strings.Contains(strings.Join(header[:2], "\n"), first.IPAddress) || !strings.Contains(header[0], "Ready") {
		t.Fatalf("header lost target IP or connection state: %q", header[:2])
	}
	m.openSpeakerChooser()
	view := m.View()
	for index, want := range []string{first.IPAddress, second.IPAddress} {
		rect, ok := m.ui.root.Frame().Rect(fmt.Sprintf("speaker-%d", index))
		if !ok {
			t.Fatalf("speaker %d row missing", index)
		}
		line := strings.Split(view.Content, "\n")[rect.Min.Y]
		if !strings.Contains(ansi.Strip(ansi.Cut(line, rect.Min.X, rect.Max.X)), want) {
			t.Fatalf("chooser did not show speaker %d IP: %q", index, line)
		}
	}
	m.chooserOpen = false
	for _, test := range []struct {
		kind, id, ip string
	}{
		{"source", "source-chooser", first.IPAddress},
		{"queue", "queue-clear", second.IPAddress},
		{"radio", "discovery-confirm", first.IPAddress},
	} {
		t.Run(test.kind, func(t *testing.T) {
			switch test.kind {
			case "source":
				m.ui.openSource()
			case "queue":
				m.ui.openQueueClear(playerQueueClearMsg{})
			case "radio":
				m.ui.discovery.confirmation = &playerDiscoveryConfirmation{
					target: first, action: "play radio", item: kefw2.ContentItem{Title: "Station"}, clearsQueue: true,
				}
			}
			view := m.View()
			rect, ok := m.ui.root.Frame().Rect(test.id)
			if !ok {
				t.Fatalf("modal %s missing", test.id)
			}
			lines := strings.Split(view.Content, "\n")
			var content []string
			for y := rect.Min.Y; y < rect.Max.Y; y++ {
				content = append(content, ansi.Strip(ansi.Cut(lines[y], rect.Min.X, rect.Max.X)))
			}
			if !strings.Contains(strings.Join(content, "\n"), test.ip) {
				t.Fatalf("%s confirmation lost target IP: %q", test.kind, content)
			}
			m.ui.sourceOpen, m.ui.queueClear, m.ui.discovery.confirmation = false, nil, nil
		})
	}
}

func TestPlayerCompactDockShowsConfirmedPowerTrackAndProgress(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 8
	m.snapshot = &statusSnapshot{Data: statusResponse{Source: kefw2.SourceOptical}}
	confirmed := playerControlsUITestSnapshot(28)
	confirmed.Data.Power = kefw2.SpeakerStatusOn
	confirmed.Data.Playback.Track = &trackStatus{Title: "Current track", Artist: "Artist", Album: "Album"}
	confirmed.Player.MediaRoles.Title = "Transport"
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.controls.active = &playerControlsUISession{generation: m.generation}
	m.ui.controls.state = playerControlsUIState{ready: true, confirmed: &confirmed, volume: 28, volumeKnown: true}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	content := m.View().Content
	for _, want := range []string{"POWER " + string(kefw2.SpeakerStatusOn), "Track: Current track", "Progress: 0:01/1:40"} {
		if !strings.Contains(content, want) {
			t.Fatalf("compact dock missing %q in %q", want, content)
		}
	}
}

func TestPlayerEventDisconnectWarningIsVisibleAndSanitized(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.addExtension(m.ui.extension())
	position, duration := 10, 100
	m.snapshot = &statusSnapshot{Data: statusResponse{Source: kefw2.SourceWiFi, Playback: &playbackStatus{
		State: "playing", PositionMS: &position, DurationMS: &duration,
		Track: &trackStatus{Title: "Track", Artist: "Artist", Album: "Album"},
	}}}
	m.eventErr = errors.New("connection lost\x1b]8;;https://example.invalid/untrusted\x1b\\link\x1b]8;;\x1b\\")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	content := m.View().Content
	if !strings.Contains(content, "Events disconnected; polling: connection lostlink") ||
		strings.Contains(content, "example.invalid/untrusted") {
		t.Fatalf("event disconnect warning missing or unsafe: %q", content)
	}
	if strings.Index(content, "Events disconnected; polling:") > strings.Index(content, "Playback:") {
		t.Fatal("event disconnect warning appeared after optional playback metadata")
	}
}

func TestPlayerVolumeIsUnknownUntilRead(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if content := m.View().Content; !strings.Contains(content, "VOL --") || strings.Contains(content, "VOL 0") {
		t.Fatalf("unknown volume was presented as a confirmed zero: %q", content)
	}
	m.ui.controls.state.volumeKnown = true
	if content := m.View().Content; !strings.Contains(content, "VOL 0") {
		t.Fatalf("confirmed zero volume was not displayed: %q", content)
	}
}

func TestPlayerVolumeSliderUsesConfirmedValueWhenMaximumIsUnknown(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.controls.state = playerControlsUIState{volume: 30, volumeKnown: true}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	content := m.View().Content
	if !strings.Contains(content, "VOL 30") || !strings.Contains(content, strings.Repeat("=", 50)) {
		t.Fatalf("slider did not reflect the confirmed volume with an unknown maximum: %q", content)
	}
}

func TestPlayerSanitizesSpeakerAndMediaText(t *testing.T) {
	const unsafe = "Name\x1b]8;;https://example.invalid/untrusted\x1b\\link\x1b]8;;\x1b\\\nnext"
	target := playerSpeaker{Name: unsafe, IPAddress: "127.0.0.1"}
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{selected: &target}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.state.page.items = []kefw2.ContentItem{{Title: unsafe}}
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current.Snapshot = &playerQueueSnapshot{rows: []kefw2.ContentItem{{Title: unsafe}}}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	content := m.View().Content
	if strings.Contains(content, "example.invalid/untrusted") || strings.Contains(content, "\x1b]8;") {
		t.Fatalf("speaker or media text retained an untrusted terminal hyperlink: %q", content)
	}
	if !strings.Contains(content, "Namelink next") {
		t.Fatalf("sanitized speaker and media text is missing: %q", content)
	}
}

func TestPlayerVolumeSliderUsesVisibleTrackAndFlushesRelease(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 12
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.controls = newPlayerControlsUI()
	maximum := 70
	m.ui.controls.state = playerControlsUIState{ready: true, epoch: 42, volume: 20, volumeKnown: true, maximum: &maximum}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("volume")
	if !ok || rect.Dx() != 50 {
		t.Fatalf("unexpected slider track: %v, %t", rect, ok)
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	initial := playerLayoutTestMessages(view.OnMouse(press))
	m.Update(press)
	m.ui.controls.state.epoch = 43
	release := tea.MouseReleaseMsg{X: rect.Max.X - 1, Y: rect.Min.Y, Button: tea.MouseLeft}
	final := playerLayoutTestMessages(m.View().OnMouse(release))
	m.Update(release)
	if len(initial) != 1 || len(final) != 1 {
		t.Fatalf("slider sent unexpected targets: initial=%#v final=%#v", initial, final)
	}
	first := initial[0].(playerScopedMsg).message.(playerVolumeSliderRequest)
	last := final[0].(playerScopedMsg).message.(playerVolumeSliderRequest)
	if first.x != 0 || first.width != 50 || first.release || last.x != 49 || last.width != 50 || !last.release ||
		first.epoch != 42 || last.epoch != 42 {
		t.Fatalf("slider targets: first=%+v last=%+v", first, last)
	}
}

func TestPlayerSplitterResizesAdjacentPanes(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	beforeDiscover, _ := m.ui.root.Frame().Rect("title-discover")
	beforeQueue, _ := m.ui.root.Frame().Rect("title-queue")
	divider, ok := m.ui.root.Frame().Rect("divider-discover")
	if !ok {
		t.Fatal("missing divider")
	}
	press := tea.MouseClickMsg{X: divider.Min.X, Y: divider.Min.Y + 1, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	motion := tea.MouseMotionMsg{X: divider.Min.X + 5, Y: divider.Min.Y + 1, Button: tea.MouseLeft}
	m.View().OnMouse(motion)
	m.Update(motion)
	release := tea.MouseReleaseMsg{X: divider.Min.X + 5, Y: divider.Min.Y + 1, Button: tea.MouseLeft}
	m.View().OnMouse(release)
	m.Update(release)
	m.View()
	afterDiscover, _ := m.ui.root.Frame().Rect("title-discover")
	afterQueue, _ := m.ui.root.Frame().Rect("title-queue")
	if afterDiscover.Dx() != beforeDiscover.Dx()+5 || afterQueue.Dx() != beforeQueue.Dx()-5 {
		t.Fatalf("splitter did not resize neighbors: before %v/%v after %v/%v",
			beforeDiscover, beforeQueue, afterDiscover, afterQueue)
	}
}

func TestPlayerFilteredChooserMouseSelectsExactSpeaker(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{
		choose: true, configured: []playerSpeaker{
			{Name: "Twin", Model: "LS60", IPAddress: "10.0.0.1"},
			{Name: "Twin", Model: "LSX", IPAddress: "10.0.0.2"},
		},
	}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	for _, r := range "10.0.0.2" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("speaker-1")
	if !ok {
		t.Fatal("filtered speaker row missing")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	release := tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	messages := playerLayoutTestMessages(m.View().OnMouse(release))
	if len(messages) != 1 {
		t.Fatalf("chooser click produced %d messages", len(messages))
	}
	choice, ok := messages[0].(playerChooseSpeakerMsg)
	if !ok || choice.index != 1 {
		t.Fatalf("chooser selected the wrong duplicate: %#v", messages[0])
	}
}

func TestPlayerWidePaneMouseActionsRemainVisible(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.queue = newPlayerQueueUI()
	m.ui.discovery = newPlayerDiscoveryUI()
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.View()
	for _, id := range []string{"queue-repeat", "queue-shuffle", "discovery-action-[Favs]", "discovery-action-[>]"} {
		rect, ok := m.ui.root.Frame().Rect(id)
		if !ok || rect.Dx() == 0 || rect.Dy() == 0 {
			t.Errorf("mouse action %s is clipped: %v, %t", id, rect, ok)
		}
	}
}

func TestPlayerResetChangesOnlyActiveArrangement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "player-layout.yaml")
	desk, browser := playerDefaultArrangement(playerListeningDesk), playerDefaultArrangement(playerDiscoveryBrowser)
	desk.Width[playerPaneSources] = 30
	browser.Width[playerPaneDetails] = 50
	if err := playerSaveLayouts(path, playerListeningDesk, desk, false); err != nil {
		t.Fatal(err)
	}
	if err := playerSaveLayouts(path, playerDiscoveryBrowser, browser, false); err != nil {
		t.Fatal(err)
	}
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(dir, "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	file, err := playerReadLayouts(path)
	if err != nil || !reflect.DeepEqual(file.Layouts[playerListeningDesk], playerDefaultArrangement(playerListeningDesk)) ||
		!reflect.DeepEqual(file.Layouts[playerDiscoveryBrowser], browser) {
		t.Fatalf("reset affected the wrong layout: %+v %v", file, err)
	}
}

func TestPlayerTitleDragDocksAndPersists(t *testing.T) {
	dir := t.TempDir()
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(dir, "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	source, sourceOK := m.ui.root.Frame().Rect("title-sources")
	target, targetOK := m.ui.root.Frame().Rect("title-queue")
	if !sourceOK || !targetOK {
		t.Fatal("pane titles unavailable")
	}
	press := tea.MouseClickMsg{X: source.Min.X + 1, Y: source.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	motion := tea.MouseMotionMsg{X: target.Min.X + 1, Y: target.Min.Y, Button: tea.MouseLeft}
	m.View().OnMouse(motion)
	m.Update(motion)
	if m.ui.preview.Order[2] != playerPaneSources {
		t.Fatalf("dock preview did not move pane: %v", m.ui.preview.Order)
	}
	release := tea.MouseReleaseMsg{X: target.Min.X + 1, Y: target.Min.Y, Button: tea.MouseLeft}
	m.View().OnMouse(release)
	m.Update(release)
	file, err := playerReadLayouts(filepath.Join(dir, "player-layout.yaml"))
	if err != nil || file.Layouts[playerListeningDesk].Order[2] != playerPaneSources {
		t.Fatalf("dock was not saved: %+v, %v", file, err)
	}
}

func TestPlayerUncertaintyNoticeSurvivesSessionReset(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(playerNoticeMsg{target: playerSpeaker{Name: "Office", IPAddress: "10.0.0.2"}, action: "play"})
	m.extensions[0].reset(m)
	content := m.View().Content
	if !strings.Contains(content, "UNCERTAIN") || !strings.Contains(content, "Office") ||
		!strings.Contains(content, "10.0.0.2") {
		t.Fatalf("notice lost its old target after reset: %q", content)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(m.ui.notices) != 0 {
		t.Fatal("escape did not dismiss uncertainty notice")
	}
}

func TestPlayerSpeakerChooserRestoresPriorFocus(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{
		configured: []playerSpeaker{{Name: "Living", IPAddress: "10.0.0.1"}},
	}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.ui.focus.Set(playerPaneDiscover)
	m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	view := m.View()
	row, ok := m.ui.root.Frame().Rect("speaker-0")
	if !ok {
		t.Fatal("chooser row missing")
	}
	press := tea.MouseClickMsg{X: row.Min.X, Y: row.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	if !m.ui.focus.Is("speaker-0") {
		t.Fatal("chooser did not acquire modal focus")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.chooserOpen || !m.ui.focus.Is(playerPaneDiscover) {
		t.Fatal("chooser did not restore previous focus")
	}
}

func TestPlayerSearchTabsToServiceSelector(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.generation = 4
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.generation = 4
	m.addExtension(m.ui.extension())
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.ui.searchActive || !m.ui.focus.Is("service-radio") {
		t.Fatal("Tab did not leave search and focus service selector")
	}
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("service activation produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok || scoped.generation != 4 {
		t.Fatalf("service activation was not scoped: %#v", messages[0])
	}
	service, ok := scoped.message.(playerDiscoveryServiceMsg)
	if !ok || service.service != playerDiscoveryRadio || service.generation != 4 {
		t.Fatalf("wrong service activation: %#v", scoped.message)
	}
}

func TestPlayerQueueDragUsesDisplayedSnapshot(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 9
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.queue = newPlayerQueueUI()
	snapshot := &playerQueueSnapshot{rows: []kefw2.ContentItem{{Title: "one"}, {Title: "two"}, {Title: "three"}}}
	m.ui.queue.current = playerQueueUIState{Generation: 9, Snapshot: snapshot, Ready: true}
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	grip, gripOK := m.ui.root.Frame().Rect("queue-grip-0")
	destination, rowOK := m.ui.root.Frame().Rect("queue-row-2")
	if !gripOK || !rowOK {
		t.Fatal("queue drag targets not visible")
	}
	press := tea.MouseClickMsg{X: grip.Min.X, Y: grip.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	motion := tea.MouseMotionMsg{X: destination.Min.X, Y: destination.Min.Y, Button: tea.MouseLeft}
	m.View().OnMouse(motion)
	m.Update(motion)
	release := tea.MouseReleaseMsg{X: destination.Min.X, Y: destination.Min.Y, Button: tea.MouseLeft}
	messages := playerLayoutTestMessages(m.View().OnMouse(release))
	if len(messages) != 1 {
		t.Fatalf("queue drag produced %d messages", len(messages))
	}
	move, ok := messages[0].(playerQueueMoveMsg)
	if !ok || move.Generation != 9 || move.Snapshot != snapshot || move.From != 0 || move.To != 1 {
		t.Fatalf("queue drag lost displayed snapshot: %#v", messages[0])
	}
}

func TestPlayerDiscoveryBrowserSearchAbovePanes(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.View()
	search, searchOK := m.ui.root.Frame().Rect("search")
	pane, paneOK := m.ui.root.Frame().Rect("title-discover")
	if !searchOK || !paneOK || search.Max.Y > pane.Min.Y {
		t.Fatalf("search is not above the pane tree: search %v pane %v", search, pane)
	}
}

func TestPlayerDiscoveryFailureVisibleAtCompactAndWideSizes(t *testing.T) {
	for _, width := range []int{80, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
			m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
			m.ui.discovery = newPlayerDiscoveryUI()
			m.ui.discovery.action = &playerDiscoveryActionStatus{
				action: "play podcast", target: playerSpeaker{Name: strings.Repeat("Long name", 12)},
				err: fmt.Errorf("resolving episode details: %w", errors.New("HTTP 404: episode not found")),
			}
			m.addExtension(m.ui.extension())
			height := 44
			if width == 80 {
				height = 24
			}
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			content := m.View().Content
			if !strings.Contains(content, "FAILED: play podcast") || !strings.Contains(content, "HTTP 404") {
				t.Fatalf("failure or cause was clipped at width %d: %q", width, content)
			}
			m.ui.discovery.action.uncertain = true
			if !strings.Contains(m.View().Content, "UNCERTAIN: play podcast") {
				t.Fatal("possibly dispatched failure did not show uncertainty")
			}
		})
	}
}

func TestPlayerDiscoveryOfflineDisablesWritesButAllowsFavoritesRead(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.selection = 0
	m.ui.discovery.state.page.items = []kefw2.ContentItem{{Title: "Station", Type: kefw2.ContentTypeAudio}}
	m.ui.pane, m.ui.zoom = playerPaneDiscover, playerPaneDiscover
	m.ui.focus.Set(playerPaneDiscover)
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := m.View()
	if !strings.Contains(view.Content, "Writes disabled: awaiting fresh speaker status") {
		t.Fatalf("offline discovery did not explain disabled writes: %q", view.Content)
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: 'f', Text: "f"}, {Code: 'F', Text: "F"}} {
		_, command := m.Update(key)
		if len(playerLayoutTestMessages(command)) != 0 {
			t.Fatalf("offline discovery accepted %q", key.String())
		}
	}
	for _, id := range []string{"discovery-action-[+Fav]", "discovery-action-[Open/Play]"} {
		rect, ok := m.ui.root.Frame().Rect(id)
		if !ok {
			t.Fatalf("action %s not visible", id)
		}
		press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
		view.OnMouse(press)
		m.Update(press)
		command := m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
		if len(playerLayoutTestMessages(command)) != 0 {
			t.Fatalf("offline mouse action %s sent a request", id)
		}
	}
	_, command := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		focus, _ := m.ui.focus.Current()
		t.Fatalf("favorites read produced %d messages (focus=%q pane=%q)", len(messages), focus, m.ui.pane)
	}
	if scoped, ok := messages[0].(playerScopedMsg); !ok {
		t.Fatalf("favorites read was not scoped: %#v", messages[0])
	} else if _, ok := scoped.message.(playerDiscoveryFavoritesMsg); !ok {
		t.Fatalf("wrong favorites read: %#v", scoped.message)
	}
}

func TestPlayerQueueOfflineDisablesWritesAndKeepsReload(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current = playerQueueUIState{Snapshot: &playerQueueSnapshot{
		rows: []kefw2.ContentItem{{Title: "Track"}},
	}, ModeKnown: true}
	m.ui.pane, m.ui.zoom = ModeQueue, ModeQueue
	m.ui.focus.Set(ModeQueue)
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := m.View()
	if !strings.Contains(view.Content, "Writes disabled: awaiting fresh speaker status") {
		t.Fatalf("offline queue did not explain disabled writes: %q", view.Content)
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: 'h', Text: "h"}, {Code: 'c', Text: "c"}} {
		_, command := m.Update(key)
		if len(playerLayoutTestMessages(command)) != 0 || m.ui.queueClear != nil {
			t.Fatalf("offline queue accepted %q", key.String())
		}
	}
	rect, ok := m.ui.root.Frame().Rect("queue-shuffle")
	if !ok {
		t.Fatal("shuffle action not visible")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	command := m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
	if len(playerLayoutTestMessages(command)) != 0 {
		t.Fatal("offline mouse shuffle sent a request")
	}
	_, command = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("queue reload produced %d messages", len(messages))
	}
	if _, ok := messages[0].(playerQueueLoadMsg); !ok {
		t.Fatalf("wrong queue reload request: %#v", messages[0])
	}
}

func TestPlayerQueueModeRequestsCaptureReadinessEpoch(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.queue = newPlayerQueueUI()
	m.ui.queue.current = playerQueueUIState{Generation: 3, Ready: true, ReadyEpoch: 42, ModeKnown: true}
	m.ui.pane, m.ui.zoom = ModeQueue, ModeQueue
	m.ui.focus.Set(ModeQueue)
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_, keyCommand := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	view := m.View()
	rect, ok := m.ui.root.Frame().Rect("queue-repeat")
	if !ok {
		t.Fatal("repeat action not visible")
	}
	press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	mouseCommand := m.View().OnMouse(tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
	m.ui.queue.current.ReadyEpoch = 43
	keyMessages, mouseMessages := playerLayoutTestMessages(keyCommand), playerLayoutTestMessages(mouseCommand)
	if len(keyMessages) != 1 || len(mouseMessages) != 1 {
		t.Fatalf("mode actions produced %d keyboard and %d mouse messages", len(keyMessages), len(mouseMessages))
	}
	shuffle, ok := keyMessages[0].(playerQueueShuffleMsg)
	if !ok || shuffle.ReadyEpoch != 42 {
		t.Fatalf("shuffle did not capture readiness epoch: %#v", keyMessages[0])
	}
	repeat, ok := mouseMessages[0].(playerQueueRepeatMsg)
	if !ok || repeat.ReadyEpoch != 42 {
		t.Fatalf("repeat did not capture readiness epoch: %#v", mouseMessages[0])
	}
}

func TestPlayerDiscoveryUPnPCapabilitiesAreVisible(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.service = playerDiscoveryUPnP
	m.ui.zoom = playerPaneDiscover
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	content := ansi.Strip(m.View().Content)
	for _, want := range []string{"no UPnP search, favorites or direct play", "play from Queue"} {
		if !strings.Contains(content, want) {
			t.Fatalf("UPnP capability %q not visible in %q", want, content)
		}
	}
}

func TestPlayerDiscoveryUPnPDisablesUnsupportedActions(t *testing.T) {
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.generation = 3
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.service, m.ui.discovery.generation = playerDiscoveryUPnP, 3
	m.ui.discovery.hasTarget = true
	m.ui.discovery.session, m.ui.discovery.writeContext = context.Background(), context.Background()
	m.ui.discovery.writePermit = &playerDiscoveryUIWritePermit{active: true}
	m.ui.discovery.selection = 0
	m.ui.discovery.state = playerDiscoveryState{
		request: playerDiscoveryRequestID{generation: 3, sequence: 2},
		page: playerDiscoveryPage{items: []kefw2.ContentItem{{
			Title: "Track", Type: kefw2.ContentTypeAudio, Path: "upnp:/track/1",
		}}},
	}
	m.ui.pane = playerPaneDiscover
	m.ui.focus.Set(playerPaneDiscover)
	m.ui.zoom = playerPaneDiscover
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyEnter}, {Code: 'f', Text: "f"}, {Code: 'F', Text: "F"},
		{Code: 'v', Text: "v"}, {Code: '/', Text: "/"},
	} {
		_, command := m.Update(key)
		if len(playerLayoutTestMessages(command)) != 0 || m.ui.searchActive {
			t.Fatalf("unsupported UPnP action %q sent a request or opened search", key.String())
		}
	}
	view := m.View()
	for _, id := range []string{"discovery-action-[Open/Play]", "discovery-action-[+Fav]", "discovery-action-[Favs]"} {
		rect, ok := m.ui.root.Frame().Rect(id)
		if !ok {
			t.Fatalf("missing action %s", id)
		}
		command := view.OnMouse(tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft})
		if len(playerLayoutTestMessages(command)) != 0 {
			t.Fatalf("disabled action %s sent a request", id)
		}
	}
	_, command := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("enqueue produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok {
		t.Fatalf("enqueue was not scoped: %#v", messages[0])
	}
	if _, ok := scoped.message.(playerDiscoveryEnqueueMsg); !ok {
		t.Fatalf("wrong enqueue request: %#v", scoped.message)
	}
}

func TestPlayerDiscoveryDoesNotOfferRadioOrPodcastEnqueue(t *testing.T) {
	for _, service := range []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast} {
		t.Run(string(service), func(t *testing.T) {
			m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
			m.generation = 3
			m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(t.TempDir(), "kefw2.yaml"))
			m.ui.discovery = newPlayerDiscoveryUI()
			m.ui.discovery.service, m.ui.discovery.generation = service, 3
			m.ui.discovery.selection = 0
			m.ui.discovery.state = playerDiscoveryState{
				request: playerDiscoveryRequestID{generation: 3, sequence: 2},
				page:    playerDiscoveryPage{items: []kefw2.ContentItem{{Title: "Result", Type: kefw2.ContentTypeAudio}}},
			}
			m.ui.pane = playerPaneDiscover
			m.ui.focus.Set(playerPaneDiscover)
			m.ui.zoom = playerPaneDiscover
			m.addExtension(m.ui.extension())
			m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			view := m.View()
			rect, ok := m.ui.root.Frame().Rect("discovery-action-[Queue -]")
			if !ok {
				t.Fatal("disabled queue action was not shown")
			}
			press := tea.MouseClickMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
			release := tea.MouseReleaseMsg{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}
			click := view.OnMouse(press)
			m.Update(press)
			clickRelease := m.View().OnMouse(release)
			m.Update(release)
			_, key := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
			if len(playerLayoutTestMessages(click)) != 0 || len(playerLayoutTestMessages(clickRelease)) != 0 ||
				len(playerLayoutTestMessages(key)) != 0 {
				t.Fatal("unsupported enqueue action sent a request")
			}
		})
	}
}

func TestPlayerPlayableRadioContainerEnterRequestsPlay(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 3
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.generation = 3
	m.ui.discovery.hasTarget = true
	m.ui.discovery.session, m.ui.discovery.writeContext = context.Background(), context.Background()
	m.ui.discovery.writePermit = &playerDiscoveryUIWritePermit{active: true}
	m.ui.discovery.selection = 0
	request := playerDiscoveryRequestID{generation: 3, sequence: 2}
	m.ui.discovery.state = playerDiscoveryState{
		request: request, hasRequest: true, hasPage: true, status: playerDiscoveryReady,
		page: playerDiscoveryPage{items: []kefw2.ContentItem{{
			Title: "Radio", Type: kefw2.ContentTypeContainer, ContainerPlayable: true, AudioType: "audioBroadcast",
		}}},
	}
	m.addExtension(m.ui.extension())
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("activation produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok {
		t.Fatalf("activation was not scoped: %#v", messages[0])
	}
	play, ok := scoped.message.(playerDiscoveryPlayMsg)
	if !ok || play.generation != 3 || play.request != request || play.index != 0 {
		t.Fatalf("playable station was browsed instead of played: %#v", scoped.message)
	}
}

func TestPlayerDiscoveryRetryTargetsFailedRequest(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.generation = 3
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.ui.discovery = newPlayerDiscoveryUI()
	m.ui.discovery.generation, m.ui.discovery.hasTarget = 3, true
	m.ui.discovery.session = context.Background()
	request := playerDiscoveryRequestID{generation: 3, sequence: 2}
	m.ui.discovery.state = playerDiscoveryState{request: request, hasRequest: true, status: playerDiscoveryError}
	m.ui.discovery.lastRead = &playerDiscoveryUIRetry{request: request}
	m.addExtension(m.ui.extension())
	_, command := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	messages := playerLayoutTestMessages(command)
	if len(messages) != 1 {
		t.Fatalf("retry produced %d messages", len(messages))
	}
	scoped, ok := messages[0].(playerScopedMsg)
	if !ok {
		t.Fatalf("retry was not scoped: %#v", messages[0])
	}
	retry, ok := scoped.message.(playerDiscoveryRetryMsg)
	if !ok || retry.generation != 3 || retry.request != request {
		t.Fatalf("retry did not retain failed request: %#v", scoped.message)
	}
}

func TestPlayerDiscoveryQueueDockPersists(t *testing.T) {
	dir := t.TempDir()
	m := newPlayerModel(context.Background(), playerDiscoveryBrowser, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(dir, "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.ui.pane != ModeQueue {
		t.Fatal("queue did not become the docked pane")
	}
	reloaded := newPlayerLayoutUI(playerDiscoveryBrowser, filepath.Join(dir, "kefw2.yaml"))
	if reloaded.error != nil || reloaded.arrangement.Order[2] != ModeQueue {
		t.Fatalf("queue dock was not restored: %+v, %v", reloaded.arrangement, reloaded.error)
	}
	if reloaded.arrangement.Width[ModeQueue] != 40 {
		t.Fatal("queue dock did not preserve inspector width")
	}
}

func TestPlayerLayoutOperationsNeverDispatchSpeakerActions(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	mutations := 0
	m.addExtension(playerExtension{update: func(_ *playerModel, msg tea.Msg) (tea.Cmd, bool) {
		switch msg.(type) {
		case playerMuteRequest, playerTransportRequest, playerSourceRequest, playerVolumeSliderRequest,
			playerQueuePlayMsg, playerQueueMoveMsg, playerQueueRemoveMsg, playerQueueClearMsg,
			playerDiscoveryPlayMsg, playerDiscoveryFavoriteMsg, playerDiscoveryEnqueueMsg:
			mutations++
		}
		return nil, false
	}})
	keys := []tea.KeyPressMsg{
		{Code: 'p', Text: "p"}, {Code: 'm', Text: "m"}, {Code: tea.KeyLeft}, {Code: tea.KeyEnter},
		{Code: 'z', Text: "z"}, {Code: 'z', Text: "z"},
		{Code: 'p', Text: "p"}, {Code: 'r', Text: "r"}, {Code: tea.KeyRight}, {Code: tea.KeyEsc},
	}
	for _, key := range keys {
		_, command := m.Update(key)
		for _, msg := range playerLayoutTestMessages(command) {
			m.Update(msg)
		}
	}
	if mutations != 0 {
		t.Fatalf("layout operations dispatched %d speaker mutations", mutations)
	}
}

func TestPlayerOpeningChooserCancelsActiveSplitterDrag(t *testing.T) {
	m := newPlayerModel(context.Background(), playerListeningDesk, playerSelection{
		configured: []playerSpeaker{{Name: "Living", IPAddress: "10.0.0.1"}},
	}, nil)
	m.ui = newPlayerLayoutUI(playerListeningDesk, filepath.Join(t.TempDir(), "kefw2.yaml"))
	m.addExtension(m.ui.extension())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := m.View()
	divider, ok := m.ui.root.Frame().Rect("divider-sources")
	if !ok {
		t.Fatal("missing divider")
	}
	before := playerCopyArrangement(m.ui.arrangement)
	press := tea.MouseClickMsg{X: divider.Min.X, Y: divider.Min.Y + 1, Button: tea.MouseLeft}
	view.OnMouse(press)
	m.Update(press)
	motion := tea.MouseMotionMsg{X: divider.Min.X + 5, Y: divider.Min.Y + 1, Button: tea.MouseLeft}
	m.View().OnMouse(motion)
	m.Update(motion)
	m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	release := tea.MouseReleaseMsg{X: divider.Min.X + 5, Y: divider.Min.Y + 1, Button: tea.MouseLeft}
	m.View().OnMouse(release)
	m.Update(release)
	if !m.chooserOpen || !reflect.DeepEqual(before, m.ui.arrangement) {
		t.Fatal("opening chooser committed an in-progress resize")
	}
}
