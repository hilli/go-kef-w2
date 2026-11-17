// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

func init() {
	setJSONHandler(radioCmd, runRadioMenuJSON)
	setJSONHandler(radioBrowseCmd, runRadioBrowseJSON)
	radioCmd.Args = cobra.NoArgs
	radioBrowseCmd.Args = cobra.MaximumNArgs(1)
	radioBrowseCmd.Flags().String("path", "", "Browse or save an exact API path in JSON mode")
	requireJSONFlags(radioBrowseCmd, "path")
	for _, cmd := range []*cobra.Command{
		radioFavoritesCmd, radioPopularCmd, radioLocalCmd, radioTrendingCmd, radioHQCmd, radioNewCmd,
	} {
		setJSONHandler(cmd, runRadioCategoryJSON)
		cmd.Flags().String("path", "", "Select an exact station path from this category in JSON mode")
		requireJSONFlags(cmd, "path")
	}
}

func radioCategoryRows(client *kefw2.AirableClient, cmd *cobra.Command) (*kefw2.RowsResponse, error) {
	switch cmd {
	case radioFavoritesCmd:
		return client.GetRadioFavoritesAll()
	case radioPopularCmd:
		return client.GetRadioPopularAll()
	case radioLocalCmd:
		return client.GetRadioLocalAll()
	case radioTrendingCmd:
		return client.GetRadioTrendingAll()
	case radioHQCmd:
		return client.GetRadioHQAll()
	case radioNewCmd:
		return client.GetRadioNewAll()
	default:
		return nil, fmt.Errorf("unknown radio category %q", cmd.Name())
	}
}

func runRadioMenuJSON(cmd *cobra.Command, _ []string) error {
	client := kefw2.NewAirableClient(currentSpeaker)
	rows, err := client.GetRadioMenu()
	if err != nil {
		return err
	}
	if rows.RowsCount > len(rows.Rows) {
		return fmt.Errorf("radio menu is incomplete; browse its exact API path")
	}
	return writeContentPage(cmd, rows, 0, len(rows.Rows))
}

func runRadioCategoryJSON(cmd *cobra.Command, args []string) error {
	path, _ := cmd.Flags().GetString("path")
	if cmd.Flags().Changed("path") && (strings.TrimSpace(path) == "" || len(args) > 0) {
		return fmt.Errorf("--path requires a nonempty path and cannot be combined with a station name")
	}
	save, _ := cmd.Flags().GetBool("save-favorite")
	remove, _ := cmd.Flags().GetBool("remove")
	if (save || remove) && len(args) == 0 && path == "" {
		return fmt.Errorf("favorite changes require an explicit station name or --path")
	}
	client := kefw2.NewAirableClient(currentSpeaker)
	rows, err := radioCategoryRows(client, cmd)
	if err != nil {
		return err
	}
	stations := filterPlayableStations(rows.Rows)
	if len(args) == 0 && path == "" {
		return writeSpeakerJSON(cmd, struct {
			Items []contentSummary `json:"items"`
		}{summarizeContent(stations)})
	}
	var selected *kefw2.ContentItem
	if path != "" {
		for i := range stations {
			if stations[i].Path == path {
				if selected != nil {
					return fmt.Errorf("station path is ambiguous in this category")
				}
				selected = &stations[i]
			}
		}
		if selected == nil {
			return fmt.Errorf("station path not found in this category")
		}
	} else {
		selected, err = findUniqueContent(stations, strings.Join(args, " "))
		if err != nil {
			return err
		}
	}
	action := "play_radio"
	switch {
	case save:
		action = "add_radio_favorite"
		err = client.AddRadioFavorite(selected)
	case remove:
		action = "remove_radio_favorite"
		err = client.RemoveRadioFavorite(selected)
	default:
		err = client.PlayRadioByPath(selected.Path)
	}
	if err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Path string `json:"path"`
	}{actionResult{action, true}, selected.Path})
}

func runRadioBrowseJSON(cmd *cobra.Command, args []string) error {
	path, _ := cmd.Flags().GetString("path")
	if cmd.Flags().Changed("path") && (strings.TrimSpace(path) == "" || len(args) > 0) {
		return fmt.Errorf("--path requires a nonempty API path and cannot be combined with a display path")
	}
	save, _ := cmd.Flags().GetBool("save-favorite")
	client := kefw2.NewAirableClient(currentSpeaker)
	if save {
		if path == "" {
			return fmt.Errorf("saving a favorite requires an explicit station --path in JSON mode")
		}
		station, err := client.GetRadioStationByPath(path)
		if err != nil {
			return err
		}
		if err := client.AddRadioFavorite(station); err != nil {
			return err
		}
		return writeSpeakerJSON(cmd, struct {
			actionResult
			Path string `json:"path"`
		}{actionResult{"add_radio_favorite", true}, path})
	}
	var rows *kefw2.RowsResponse
	var err error
	switch {
	case path != "":
		rows, err = client.GetAllRows(path)
	case len(args) > 0:
		rows, err = client.BrowseRadioByDisplayPath(args[0])
	default:
		return runRadioMenuJSON(cmd, nil)
	}
	if err != nil {
		return err
	}
	if rows.RowsCount > len(rows.Rows) {
		return fmt.Errorf("radio browse response is incomplete; use an exact --path")
	}
	return writeContentPage(cmd, rows, 0, len(rows.Rows))
}
