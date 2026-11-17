// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/hilli/go-kef-w2/kefw2"
)

type queueEntry struct {
	Position int `json:"position"`
	contentSummary
}

const (
	queueMoveBefore = "before"
	queueMoveAfter  = "after"
	queueMoveDown   = "down"
	queueMoveNext   = "next"
	queueRepeatOne  = "one"
	queueRepeatAll  = "all"
)

func init() {
	setJSONHandler(queueCmd, runQueueListJSON)
	setJSONHandler(queueListCmd, runQueueListJSON)
	setJSONHandler(queueClearCmd, func(cmd *cobra.Command, _ []string) error {
		if err := kefw2.NewAirableClient(currentSpeaker).ClearPlaylist(); err != nil {
			return err
		}
		return writeSpeakerJSON(cmd, actionResult{"clear_queue", true})
	})
	setJSONHandler(queueRemoveCmd, runQueueSelectionJSON)
	setJSONHandler(queuePlayCmd, runQueueSelectionJSON)
	setJSONHandler(queueMoveCmd, runQueueMoveJSON)
	setJSONHandler(queueModeCmd, runQueueModeJSON)
	setJSONHandler(queueShuffleCmd, runQueueModeJSON)
	setJSONHandler(queueRepeatCmd, runQueueModeJSON)
	setJSONHandler(queueSaveCmd, runQueueSaveJSON)
	setJSONHandler(queueLoadCmd, runQueueLoadJSON)
	setJSONHandler(queueSavedCmd, runQueueSavedJSON)
	setJSONHandler(queueDeleteSavedCmd, runQueueDeleteJSON)
	for _, cmd := range []*cobra.Command{queueCmd, queueListCmd, queueClearCmd, queueModeCmd, queueSavedCmd} {
		cmd.Args = cobra.NoArgs
	}
}

func queueRowsJSON() (*kefw2.AirableClient, []kefw2.ContentItem, error) {
	client := kefw2.NewAirableClient(currentSpeaker)
	rows, err := client.GetPlayQueue()
	if err != nil {
		return nil, nil, fmt.Errorf("getting play queue: %w", err)
	}
	if len(rows.Rows) < rows.RowsCount {
		return nil, nil, fmt.Errorf("play queue response is incomplete")
	}
	return client, rows.Rows, nil
}

func runQueueListJSON(cmd *cobra.Command, _ []string) error {
	_, rows, err := queueRowsJSON()
	if err != nil {
		return err
	}
	summaries := summarizeContent(rows)
	items := make([]queueEntry, 0, len(rows))
	for i, item := range summaries {
		items = append(items, queueEntry{i + 1, item})
	}
	return writeSpeakerJSON(cmd, struct {
		Items []queueEntry `json:"items"`
	}{items})
}

func queueJSONIndex(rows []kefw2.ContentItem, arg string) (int, error) {
	if strings.TrimSpace(arg) == "" {
		return -1, fmt.Errorf("queue selection must not be empty")
	}
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 1 || n > len(rows) {
			return -1, fmt.Errorf("queue position must be between 1 and %d", len(rows))
		}
		return n - 1, nil
	}
	index := -1
	for i, item := range rows {
		label := item.Title
		if item.MediaData != nil && item.MediaData.MetaData.Artist != "" {
			label += " - " + item.MediaData.MetaData.Artist
		}
		if strings.EqualFold(arg, item.Title) || strings.EqualFold(arg, label) || arg == item.Path {
			if index >= 0 {
				return -1, fmt.Errorf("queue selection %q is ambiguous; use a numeric position", arg)
			}
			index = i
		}
	}
	if index < 0 {
		return -1, fmt.Errorf("queue selection %q not found", arg)
	}
	return index, nil
}

func runQueueSelectionJSON(cmd *cobra.Command, args []string) error {
	client, rows, err := queueRowsJSON()
	if err != nil {
		return err
	}
	index, err := queueJSONIndex(rows, args[0])
	if err != nil {
		return err
	}
	action := "play_queue_item"
	if cmd == queueRemoveCmd {
		action = "remove_queue_item"
		err = client.RemoveFromQueue([]int{index})
	} else {
		err = client.PlayQueueIndex(index, &rows[index])
	}
	if err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Position int `json:"position"`
	}{actionResult{action, true}, index + 1})
}

func queueRelativePosition(from, target, count int, after bool) int {
	if from == target {
		return from
	}
	if after {
		if from < target {
			return target
		}
		return min(target+1, count-1)
	}
	if from > target {
		return target
	}
	return max(target-1, 0)
}

func queueDestinationJSON(cmd *cobra.Command, args []string, rows []kefw2.ContentItem, from int) (int, error) {
	destination := strings.ToLower(args[1])
	relative := destination == queueMoveBefore || destination == queueMoveAfter
	if relative != (len(args) == 3) {
		return -1, fmt.Errorf("only 'before' and 'after' require a third target argument")
	}
	if relative {
		target, err := queueJSONIndex(rows, args[2])
		if err != nil {
			return -1, err
		}
		return queueRelativePosition(from, target, len(rows), destination == queueMoveAfter), nil
	}
	switch destination {
	case "top":
		return 0, nil
	case "bottom":
		return len(rows) - 1, nil
	case "up":
		return max(from-1, 0), nil
	case queueMoveDown:
		return min(from+1, len(rows)-1), nil
	case queueMoveNext:
		pd, err := currentSpeaker.PlayerData(cmd.Context())
		if err != nil {
			return -1, err
		}
		for i, row := range rows {
			if row.Path != "" && row.Path == pd.TrackRoles.Path {
				return queueRelativePosition(from, i, len(rows), true), nil
			}
		}
		if strings.HasPrefix(pd.TrackRoles.Path, "playlists:item/") {
			return -1, fmt.Errorf("current queue item is missing from this queue snapshot; read the queue again")
		}
		return 0, nil
	default:
		return queueJSONIndex(rows, args[1])
	}
}

func runQueueMoveJSON(cmd *cobra.Command, args []string) error {
	client, rows, err := queueRowsJSON()
	if err != nil {
		return err
	}
	from, err := queueJSONIndex(rows, args[0])
	if err != nil {
		return err
	}
	to, err := queueDestinationJSON(cmd, args, rows, from)
	if err != nil {
		return err
	}
	if from != to {
		if err := client.MoveQueueItem(from, to); err != nil {
			return err
		}
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		From int `json:"from"`
		To   int `json:"to"`
	}{actionResult{"move_queue_item", from != to}, from + 1, to + 1})
}

func runQueueModeJSON(cmd *cobra.Command, args []string) error {
	client := kefw2.NewAirableClient(currentSpeaker)
	if len(args) > 0 {
		value := strings.ToLower(args[0])
		var err error
		if cmd == queueShuffleCmd {
			var enabled bool
			enabled, err = parseMuteArg(value)
			if err == nil {
				err = client.SetShuffle(enabled)
			}
		} else {
			switch value {
			case "none":
				value = PlayModeOff
			case "track", "single":
				value = queueRepeatOne
			case ModeQueue, "playlist":
				value = queueRepeatAll
			}
			if value != PlayModeOff && value != queueRepeatOne && value != queueRepeatAll {
				return fmt.Errorf("repeat must be off, one, or all")
			}
			err = client.SetRepeat(value)
		}
		if err != nil {
			return err
		}
		return writeSpeakerJSON(cmd, struct {
			actionResult
			Value string `json:"value"`
		}{actionResult{"set_" + cmd.Name(), true}, value})
	}
	mode, err := client.GetPlayMode()
	if err != nil {
		return err
	}
	repeat := PlayModeOff
	switch mode {
	case kefw2.PlayModeRepeatOne, kefw2.PlayModeShuffleRepeatOne:
		repeat = queueRepeatOne
	case kefw2.PlayModeRepeatAll, kefw2.PlayModeShuffleRepeatAll:
		repeat = queueRepeatAll
	}
	return writeSpeakerJSON(cmd, struct {
		Mode    string `json:"mode"`
		Shuffle bool   `json:"shuffle"`
		Repeat  string `json:"repeat"`
	}{mode, strings.HasPrefix(mode, "shuffle"), repeat})
}

func savedQueueJSONPath(name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("saved queue name must be nonempty and contain no path separators or colons")
	}
	return getSavedQueuePath(name)
}

func runQueueSaveJSON(cmd *cobra.Command, args []string) error {
	path, err := savedQueueJSONPath(args[0])
	if err != nil {
		return err
	}
	_, rows, err := queueRowsJSON()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("play queue is empty; nothing to save")
	}
	tracks := make([]SavedTrack, 0, len(rows))
	for _, row := range rows {
		track := toSavedTrack(row)
		if track.Resource.URI == "" {
			return fmt.Errorf("track %q has no restorable stream resource", row.Title)
		}
		tracks = append(tracks, track)
	}
	data, err := yaml.Marshal(SavedQueue{Name: args[0], Tracks: tracks})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Name       string `json:"name"`
		TrackCount int    `json:"track_count"`
	}{actionResult{"save_queue", true}, args[0], len(tracks)})
}

func runQueueLoadJSON(cmd *cobra.Command, args []string) error {
	path, err := savedQueueJSONPath(args[0])
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path) //nolint:gosec // Validated queue name beneath the user's config directory.
	if err != nil {
		return fmt.Errorf("reading saved queue: %w", err)
	}
	var saved SavedQueue
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&saved); err != nil {
		return fmt.Errorf("parsing saved queue: %w", err)
	}
	if len(saved.Tracks) == 0 {
		return fmt.Errorf("saved queue is empty")
	}
	tracks := make([]kefw2.ContentItem, 0, len(saved.Tracks))
	for _, track := range saved.Tracks {
		if track.Resource.URI == "" {
			return fmt.Errorf("saved track %q has no resource URI", track.Title)
		}
		tracks = append(tracks, track.toContentItem())
	}
	client := kefw2.NewAirableClient(currentSpeaker)
	if err := client.ClearPlaylist(); err != nil {
		return err
	}
	if err := client.AddToQueue(tracks, true); err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Name       string `json:"name"`
		TrackCount int    `json:"track_count"`
	}{actionResult{"load_queue", true}, args[0], len(tracks)})
}

func runQueueSavedJSON(cmd *cobra.Command, _ []string) error {
	names, err := listSavedQueues()
	if err != nil {
		return err
	}
	items := make([]string, 0, len(names))
	items = append(items, names...)
	return writeJSON(cmd, struct {
		Items []string `json:"items"`
	}{items})
}

func runQueueDeleteJSON(cmd *cobra.Command, args []string) error {
	path, err := savedQueueJSONPath(args[0])
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting saved queue: %w", err)
	}
	return writeJSON(cmd, struct {
		actionResult
		Name string `json:"name"`
	}{actionResult{"delete_saved_queue", true}, args[0]})
}
