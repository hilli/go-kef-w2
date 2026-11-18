// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

func TestContentPickerV2InputAndSelection(t *testing.T) {
	var played string
	model := NewContentPickerModel(ContentPickerConfig{
		Items: []kefw2.ContentItem{
			{Title: "Alpha", Type: TypeAudio},
			{Title: "Beta", Type: TypeAudio},
		},
		Callbacks: ContentPickerCallbacks{
			Play: func(item *kefw2.ContentItem) error {
				played = item.Title
				return nil
			},
		},
	})
	if model.View().AltScreen {
		t.Fatal("inline picker unexpectedly entered the alternate screen")
	}
	next, _ := model.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	model = next.(ContentPickerModel)
	if model.filterInput.Value() != "b" || len(model.filtered) != 1 || model.filtered[0].Title != "Beta" {
		t.Fatalf("v2 key press did not filter the picker: input=%q items=%v", model.filterInput.Value(), model.filtered)
	}
	next, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = next.(ContentPickerModel)
	if played != "Beta" || !model.played || command == nil {
		t.Fatalf("filtered selection was not played: %q, played=%v", played, model.played)
	}
}

func TestBrowserV2FocusAndAlternateScreen(t *testing.T) {
	radio := initialRadioModel(nil)
	nextRadio, command := radio.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	radio = nextRadio.(radioModel)
	if command == nil || radio.searchInput.Value() != "q" || radio.quitting || !radio.View().AltScreen {
		t.Fatal("radio search did not preserve focused input and alternate screen")
	}
	podcast := initialPodcastModel(nil)
	nextPodcast, command := podcast.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	podcast = nextPodcast.(podcastModel)
	if command == nil || podcast.searchInput.Value() != "q" || podcast.quitting || !podcast.View().AltScreen {
		t.Fatal("podcast search did not preserve focused input and alternate screen")
	}
	upnp := initialUpnpModel(nil)
	_, command = upnp.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if command == nil || !upnp.View().AltScreen {
		t.Fatal("UPnP add-all key or alternate screen was lost")
	}
}
