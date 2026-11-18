// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestContentPickerSelectionDispatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action ContentAction
	}{
		{"play", ActionPlay},
		{"save", ActionSaveFavorite},
		{"remove", ActionRemoveFavorite},
		{"queue", ActionAddToQueue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action := tc.action
			called := 0
			callback := func(item *kefw2.ContentItem) error {
				called++
				if item.Path != "track" {
					t.Fatalf("selected unexpected item: %+v", item)
				}
				return nil
			}
			model := NewContentPickerModel(ContentPickerConfig{
				Items:  []kefw2.ContentItem{{Title: "Track", Path: "track", Type: TypeAudio}},
				Action: action,
				Callbacks: ContentPickerCallbacks{
					Play: callback, SaveFavorite: callback, RemoveFavorite: callback, AddToQueue: callback,
				},
			})
			updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			result := updated.(ContentPickerModel).Result()
			if command == nil || called != 1 || result.Error != nil || result.Selected == nil ||
				result.Played != (action == ActionPlay) || result.Saved != (action == ActionSaveFavorite) ||
				result.Removed != (action == ActionRemoveFavorite) || result.Queued != (action == ActionAddToQueue) {
				t.Fatalf("action %d returned %+v, calls=%d", action, result, called)
			}
		})
	}

	failure := errors.New("queue unavailable")
	model := NewContentPickerModel(ContentPickerConfig{
		Items:  []kefw2.ContentItem{{Title: "Track", Type: TypeAudio}},
		Action: ActionAddToQueue,
		Callbacks: ContentPickerCallbacks{AddToQueue: func(*kefw2.ContentItem) error {
			return failure
		}},
	})
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result := updated.(ContentPickerModel).Result()
	if !errors.Is(result.Error, failure) || result.Queued {
		t.Fatalf("failed action returned %+v", result)
	}
}

func TestContentPickerQueueShortcuts(t *testing.T) {
	var added, removed, cleared int
	model := NewContentPickerModel(ContentPickerConfig{
		Items: []kefw2.ContentItem{
			{Title: "Track", Path: "track", Type: TypeAudio},
			{Title: "Folder", Path: "folder", Type: TypeContainer},
		},
		Callbacks: ContentPickerCallbacks{
			AddToQueue: func(*kefw2.ContentItem) error { added++; return nil },
			AddContainerToQueue: func(*kefw2.ContentItem) (int, error) {
				added += 2
				return 2, nil
			},
			DeleteFromQueue: func(*kefw2.ContentItem) error {
				removed++
				if removed == 1 {
					return errors.New("offline")
				}
				return nil
			},
			ClearQueue: func() error { cleared++; return nil },
		},
	})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	model = updated.(ContentPickerModel)
	if added != 1 || !strings.Contains(model.statusMsg, "Track") {
		t.Fatalf("track was not queued: %+v", model)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model = updated.(ContentPickerModel)
	if removed != 1 || len(model.filtered) != 2 || !strings.Contains(model.statusMsg, "offline") {
		t.Fatalf("failed deletion changed the queue: %+v", model.filtered)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	model = updated.(ContentPickerModel)
	if removed != 2 || len(model.filtered) != 1 || model.filtered[0].Path != "folder" {
		t.Fatalf("selected track was not removed: %+v", model.filtered)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	model = updated.(ContentPickerModel)
	if added != 3 || !strings.Contains(model.statusMsg, "2 tracks") {
		t.Fatalf("container was not queued: %q", model.statusMsg)
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	model = updated.(ContentPickerModel)
	if cleared != 1 || len(model.filtered) != 0 || model.cursor != 0 {
		t.Fatalf("queue was not cleared: %+v", model)
	}
}

func TestBrowserSearchAcceptsQuitKey(t *testing.T) {
	key := tea.KeyPressMsg{Code: 'q', Text: "q"}
	radio := initialRadioModel(nil)
	radioUpdated, _ := radio.Update(key)
	radio = radioUpdated.(radioModel)
	if radio.quitting || radio.searchInput.Value() != "q" {
		t.Fatal("radio search treated q as quit")
	}
	podcast := initialPodcastModel(nil)
	podcastUpdated, _ := podcast.Update(key)
	podcast = podcastUpdated.(podcastModel)
	if podcast.quitting || podcast.searchInput.Value() != "q" {
		t.Fatal("podcast search treated q as quit")
	}
}

func TestRadioKeyboardSearchAndShortcut(t *testing.T) {
	model := initialRadioModel(nil)
	model.searchInput.SetValue("station")
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(radioModel)
	if command == nil || !model.loading || !model.searching {
		t.Fatal("search did not start")
	}
	model.searchInput.Blur()
	updated, command = model.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	model = updated.(radioModel)
	if command == nil || model.mode != ModeFavorites || !model.loading {
		t.Fatal("favorites shortcut did not start")
	}
}

func TestPodcastKeyboardNavigation(t *testing.T) {
	model := initialPodcastModel(nil)
	model.searchInput.Blur()
	model.list.SetItems([]list.Item{podcastItem{item: kefw2.ContentItem{
		Title: "Show", Type: TypeContainer, Path: "show",
	}}})
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(podcastModel)
	if command == nil || model.mode != TypeEpisodes || model.currentPodcast == nil || !model.loading {
		t.Fatal("selecting a podcast did not enter episodes")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(podcastModel)
	if model.mode != modeSearch || model.currentPodcast != nil || len(model.list.Items()) != 0 {
		t.Fatal("escape did not return to podcast search")
	}
}

func TestUPnPKeyboardNavigation(t *testing.T) {
	model := initialUpnpModel(nil)
	model.list.SetItems([]list.Item{upnpItem{item: kefw2.ContentItem{
		Title: "Library", Type: TypeContainer, Path: "library",
	}}})
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(upnpModel)
	if command == nil || !model.loading || len(model.breadcrumbs) != 2 || model.breadcrumbs[1].path != "library" {
		t.Fatal("selecting a folder did not navigate")
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	model = updated.(upnpModel)
	if command == nil || len(model.breadcrumbs) != 1 {
		t.Fatal("backspace did not navigate back")
	}
}

func TestUPnPFilterAcceptsQuitKey(t *testing.T) {
	model := initialUpnpModel(nil)
	model.list.SetFilterState(list.Filtering)
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	model = updated.(upnpModel)
	if model.quitting || model.list.FilterValue() != "q" {
		t.Fatal("UPnP filter treated q as quit")
	}
}
