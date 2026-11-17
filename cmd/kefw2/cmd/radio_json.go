// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

type radioStation struct {
	Title       string `json:"title"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

func init() {
	addJSONFlag(radioSearchCmd)
	addJSONFlag(radioPlayCmd)
	radioSearchCmd.Flags().Int("offset", 0, "Zero-based service row offset (JSON search only)")
	radioSearchCmd.Flags().Int("limit", 24, "Maximum service rows to request, 1..100 (JSON search only)")
	radioSearchCmd.RunE = runRadioSearch
	radioPlayCmd.Flags().String("path", "", "Play an exact station path returned by JSON search")
	radioPlayCmd.Args = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("path") {
			path, _ := cmd.Flags().GetString("path")
			if strings.TrimSpace(path) == "" || len(args) != 0 {
				return fmt.Errorf("--path requires a nonempty path and cannot be combined with a station name")
			}
			return nil
		}
		return cobra.MinimumNArgs(1)(cmd, args)
	}
	radioPlayCmd.RunE = runRadioPlay
}

func runRadioSearch(cmd *cobra.Command, args []string) error {
	if !jsonOutput(cmd) {
		if cmd.Flags().Changed("offset") || cmd.Flags().Changed("limit") {
			return fmt.Errorf("--offset and --limit require --json")
		}
		radioSearchCmd.Run(cmd, args)
		return nil
	}
	query := strings.Join(args, " ")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("radio search query must not be empty")
	}
	offset, _ := cmd.Flags().GetInt("offset")
	limit, _ := cmd.Flags().GetInt("limit")
	client := kefw2.NewAirableClient(currentSpeaker)
	resp, err := client.SearchRadioPage(query, offset, limit)
	if err != nil {
		return fmt.Errorf("searching radio: %w", err)
	}
	items := make([]radioStation, 0, len(resp.Rows))
	for _, station := range filterPlayableStations(resp.Rows) {
		items = append(items, radioStation{station.Title, station.Path, station.LongDescription})
	}
	nextOffset := offset + len(resp.Rows)
	return writeSpeakerJSON(cmd, struct {
		Items      []radioStation `json:"items"`
		Offset     int            `json:"offset"`
		Limit      int            `json:"limit"`
		TotalRows  int            `json:"total_rows"`
		NextOffset int            `json:"next_offset"`
		HasMore    bool           `json:"has_more"`
	}{items, offset, limit, resp.RowsCount, nextOffset, nextOffset < resp.RowsCount})
}

func runRadioPlay(cmd *cobra.Command, args []string) error {
	path, _ := cmd.Flags().GetString("path")
	if !jsonOutput(cmd) && path == "" {
		radioPlayCmd.Run(cmd, args)
		return nil
	}
	client := kefw2.NewAirableClient(currentSpeaker)
	if path == "" {
		query := strings.Join(args, " ")
		if strings.TrimSpace(query) == "" {
			return fmt.Errorf("radio station name must not be empty")
		}
		resp, err := client.SearchRadio(query)
		if err != nil {
			return fmt.Errorf("searching radio: %w", err)
		}
		if len(resp.Rows) < resp.RowsCount {
			return fmt.Errorf("search results are incomplete; use 'radio search --json' and play an exact --path")
		}
		station, err := selectRadioStation(filterPlayableStations(resp.Rows), query)
		if err != nil {
			return err
		}
		path = station.Path
	}
	if err := client.PlayRadioByPath(path); err != nil {
		return fmt.Errorf("playing radio path %q: %w", path, err)
	}
	if jsonOutput(cmd) {
		return writeSpeakerJSON(cmd, struct {
			actionResult
			Path string `json:"path"`
		}{actionResult{"play_radio", true}, path})
	}
	taskConpletedPrinter.Printf("Playback requested: %s\n", path)
	return nil
}

func selectRadioStation(items []kefw2.ContentItem, query string) (*kefw2.ContentItem, error) {
	var exact *kefw2.ContentItem
	for i := range items {
		if strings.EqualFold(items[i].Title, query) {
			if exact != nil {
				return nil, fmt.Errorf("ambiguous station %q; search with --json and choose an exact --path", query)
			}
			exact = &items[i]
		}
	}
	if exact != nil {
		return exact, nil
	}
	if len(items) == 1 {
		return &items[0], nil
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no playable stations found for %q", query)
	}
	return nil, fmt.Errorf("ambiguous station %q; search with --json and choose an exact --path", query)
}
