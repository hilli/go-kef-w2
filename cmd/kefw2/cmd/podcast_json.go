// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	podcastJSONPath     = "path"
	podcastJSONPlay     = "play"
	podcastJSONFavorite = "favorite"
	podcastJSONRemove   = "remove"
)

// These options are local to each command: a parent --json is not inherited.
type podcastJSONOptions struct {
	path, episodePath, action string
	latest                    bool
}

func initPodcastJSON() {
	commands := []*cobra.Command{
		podcastCmd, podcastSearchCmd, podcastFavoritesCmd, podcastPopularCmd,
		podcastTrendingCmd, podcastHistoryCmd, podcastPlayCmd, podcastBrowseCmd, podcastFilterCmd,
	}
	for _, command := range commands {
		command.Flags().String(podcastJSONPath, "", "Exact selected show path (JSON only; excludes show name)")
		command.Flags().String("episode-path", "", "Exact episode path (JSON only; optionally within --path show)")
		command.Flags().String("action", "", "Explicit JSON action: play, queue, favorite, remove")
		command.Flags().Bool("latest", false, "Select the first audio episode in service order (JSON only)")
		oldArgs := command.Args
		command.Args = func(cmd *cobra.Command, args []string) error {
			if jsonOutput(cmd) {
				if cmd == podcastCmd {
					return cobra.NoArgs(cmd, args)
				}
				if cmd == podcastBrowseCmd || cmd == podcastFilterCmd {
					return cobra.MaximumNArgs(1)(cmd, args)
				}
				return nil
			}
			if oldArgs != nil {
				return oldArgs(cmd, args)
			}
			return nil
		}
		setJSONHandler(command, runPodcastJSON)
		run := command.RunE
		command.RunE = func(cmd *cobra.Command, args []string) error {
			if !jsonOutput(cmd) {
				for _, flag := range []string{podcastJSONPath, "episode-path", "action", "latest"} {
					if cmd.Flags().Changed(flag) {
						return fmt.Errorf("--%s requires --json", flag)
					}
				}
			}
			return run(cmd, args)
		}
	}
	episodes := &cobra.Command{
		Use:   TypeEpisodes,
		Short: "List every audio episode of an exact show path without selecting playback",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString(podcastJSONPath)
			if !jsonOutput(cmd) || strings.TrimSpace(path) == "" {
				return fmt.Errorf("podcast episodes requires --path SHOW_PATH --json")
			}
			client := kefw2.NewAirableClient(currentSpeaker)
			rows, err := client.GetPodcastEpisodesAll(path)
			if err != nil {
				return err
			}
			return writePodcastRows(cmd, podcastAudio(rows.Rows))
		},
	}
	episodes.Flags().String(podcastJSONPath, "", "Exact show path returned by podcast search or lists")
	addJSONFlag(episodes)
	podcastCmd.AddCommand(episodes)
}

func readPodcastJSONOptions(cmd *cobra.Command) (podcastJSONOptions, error) {
	o := podcastJSONOptions{}
	o.path, _ = cmd.Flags().GetString(podcastJSONPath)
	o.episodePath, _ = cmd.Flags().GetString("episode-path")
	o.action, _ = cmd.Flags().GetString("action")
	o.latest, _ = cmd.Flags().GetBool("latest")
	for _, name := range []string{podcastJSONPath, "episode-path", "action"} {
		value, _ := cmd.Flags().GetString(name)
		if cmd.Flags().Changed(name) && strings.TrimSpace(value) == "" {
			return o, fmt.Errorf("--%s must not be empty", name)
		}
	}
	for flag, action := range map[string]string{
		ModeQueue: ModeQueue, "save-favorite": podcastJSONFavorite, podcastJSONRemove: podcastJSONRemove,
	} {
		if cmd.Flags().Lookup(flag) == nil {
			continue
		}
		enabled, _ := cmd.Flags().GetBool(flag)
		if enabled {
			if o.action != "" {
				return o, fmt.Errorf("--action, --queue, --save-favorite and --remove are mutually exclusive")
			}
			o.action = action
		}
	}
	switch o.action {
	case "", podcastJSONPlay, ModeQueue, podcastJSONFavorite, podcastJSONRemove:
	default:
		return o, fmt.Errorf("unknown podcast action %q; use play, queue, favorite or remove", o.action)
	}
	if o.latest && o.episodePath != "" {
		return o, fmt.Errorf("--latest and --episode-path are mutually exclusive")
	}
	if (o.action == podcastJSONFavorite || o.action == podcastJSONRemove) && (o.latest || o.episodePath != "") {
		return o, fmt.Errorf("favorite actions select a show, not --latest or --episode-path")
	}
	return o, nil
}

func runPodcastJSON(cmd *cobra.Command, args []string) error {
	o, err := readPodcastJSONOptions(cmd)
	if err != nil {
		return err
	}
	name, selected, err := validatePodcastJSONSelection(cmd, args, &o)
	if err != nil {
		return err
	}
	client := kefw2.NewAirableClient(currentSpeaker)
	// Exact selections need not perform unrelated searches/category requests.
	if o.path != "" || o.episodePath != "" {
		if err := validatePodcastJSONExactArgs(cmd, args); err != nil {
			return err
		}
		return performPodcastJSONAction(cmd, client, o, nil, "")
	}
	rows, err := fetchPodcastJSONRows(cmd, client, args)
	if err != nil {
		return err
	}
	if !selected {
		return writePodcastRows(cmd, rows.Rows)
	}
	showName, episodeName, hasEpisode := parsePodcastPath(name)
	if hasEpisode && (o.latest || o.action == podcastJSONFavorite || o.action == podcastJSONRemove) {
		return fmt.Errorf("an episode name cannot be combined with --latest or favorite actions")
	}
	show, err := findUniqueContent(filterPodcastContainers(rows.Rows), showName)
	if err != nil {
		return err
	}
	return performPodcastJSONAction(cmd, client, o, show, episodeName)
}

func podcastJSONSelectsName(cmd *cobra.Command) bool {
	switch cmd {
	case podcastPlayCmd, podcastFavoritesCmd, podcastPopularCmd, podcastTrendingCmd, podcastHistoryCmd:
		return true
	}
	return false
}

func validatePodcastJSONSelection(
	cmd *cobra.Command, args []string, o *podcastJSONOptions,
) (string, bool, error) {
	// Search arguments are always a query, never a selected result. Browse and
	// filter arguments navigate; only category/play arguments select a show.
	name := ""
	if podcastJSONSelectsName(cmd) {
		name = strings.Join(args, " ")
		if len(args) > 0 && (o.path != "" || o.episodePath != "") {
			return "", false, fmt.Errorf("--path/--episode-path and a show/episode name are mutually exclusive")
		}
	}
	selected := o.path != "" || o.episodePath != "" || name != ""
	if cmd == podcastCmd && (selected || o.action != "" || o.latest) {
		return "", false, fmt.Errorf("use a podcast subcommand for selection or actions")
	}
	if !selected && (o.action != "" || o.latest || cmd == podcastPlayCmd) {
		return "", false, fmt.Errorf("an explicit --path, --episode-path or show name is required for an action")
	}
	if selected && o.action == "" {
		if podcastJSONSelectsName(cmd) {
			o.action = podcastJSONPlay
		} else {
			return "", false, fmt.Errorf("selecting a path requires an explicit --action")
		}
	}
	return name, selected, nil
}

func validatePodcastJSONExactArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch cmd {
	case podcastSearchCmd:
		return fmt.Errorf("a search query and exact selection paths are mutually exclusive")
	case podcastFilterCmd:
		return fmt.Errorf("a filter display path and --path/--episode-path are mutually exclusive")
	case podcastBrowseCmd:
		if !podcastJSONCategory(args[0]) {
			return fmt.Errorf("unknown podcast category %q", args[0])
		}
	}
	return nil
}

func fetchPodcastJSONRows(
	cmd *cobra.Command, client *kefw2.AirableClient, args []string,
) (*kefw2.RowsResponse, error) {
	switch cmd {
	case podcastCmd:
		return client.GetPodcastMenuAll()
	case podcastSearchCmd, podcastPlayCmd:
		query := strings.Join(args, " ")
		if strings.TrimSpace(query) == "" {
			return nil, fmt.Errorf("podcast search query must not be empty")
		}
		if cmd == podcastPlayCmd {
			query, _, _ = parsePodcastPath(query)
		}
		return client.SearchPodcastsAll(query)
	case podcastFavoritesCmd:
		return client.GetPodcastFavoritesAll()
	case podcastPopularCmd:
		return client.GetPodcastPopularAll()
	case podcastTrendingCmd:
		return client.GetPodcastTrendingAll()
	case podcastHistoryCmd:
		return client.GetPodcastHistoryAll()
	case podcastBrowseCmd:
		category := ModePopular
		if len(args) > 0 {
			category = strings.ToLower(args[0])
		}
		return fetchPodcastJSONCategory(client, category)
	case podcastFilterCmd:
		return podcastJSONFilter(client, args)
	}
	return nil, fmt.Errorf("unsupported podcast JSON command %q", cmd.Name())
}

func fetchPodcastJSONCategory(client *kefw2.AirableClient, category string) (*kefw2.RowsResponse, error) {
	switch category {
	case ModePopular:
		return client.GetPodcastPopularAll()
	case ModeTrending:
		return client.GetPodcastTrendingAll()
	case podcastModeHistory:
		return client.GetPodcastHistoryAll()
	case ModeFavorites, favoriteAlias:
		return client.GetPodcastFavoritesAll()
	}
	return nil, fmt.Errorf("unknown podcast category %q", category)
}

func podcastJSONCategory(category string) bool {
	switch strings.ToLower(category) {
	case ModePopular, ModeTrending, podcastModeHistory, ModeFavorites, favoriteAlias:
		return true
	}
	return false
}

func writePodcastRows(cmd *cobra.Command, items []kefw2.ContentItem) error {
	// All pages have been fetched, so these offsets describe the complete list.
	return writeContentPage(cmd, &kefw2.RowsResponse{Rows: items, RowsCount: len(items)}, 0, len(items))
}

func podcastAudio(items []kefw2.ContentItem) []kefw2.ContentItem {
	audio := make([]kefw2.ContentItem, 0)
	for _, item := range items {
		if item.Type == kefw2.ContentTypeAudio {
			audio = append(audio, item)
		}
	}
	return audio
}

func podcastJSONFilter(client *kefw2.AirableClient, args []string) (*kefw2.RowsResponse, error) {
	// The filter endpoint itself is paginated, unlike GetPodcastFilter's first page.
	rows, err := client.GetPodcastFilterAll()
	if len(args) == 0 {
		return rows, err
	}
	for _, segment := range strings.Split(args[0], "/") {
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(segment) == "" {
			return nil, fmt.Errorf("filter path contains an empty segment")
		}
		item, findErr := findUniqueContent(rows.Rows, segment)
		if findErr != nil {
			return nil, findErr
		}
		rows, err = client.GetAllRows(item.Path)
	}
	return rows, err
}

func performPodcastJSONAction(
	cmd *cobra.Command, client *kefw2.AirableClient, o podcastJSONOptions,
	show *kefw2.ContentItem, episodeName string,
) error {
	var err error
	if o.path != "" {
		show, err = client.GetPodcastDetailsExact(o.path)
		if err != nil {
			return err
		}
		if show.Type != kefw2.ContentTypeContainer {
			return fmt.Errorf("--path must select a podcast show container; use --episode-path for audio")
		}
	}
	if show != nil && (show.Type != kefw2.ContentTypeContainer || strings.TrimSpace(show.Path) == "") {
		return fmt.Errorf("selection is not a podcast show with an exact path")
	}
	item := show
	if o.action == podcastJSONFavorite || o.action == podcastJSONRemove {
		if show == nil {
			return fmt.Errorf("a show is required for favorite actions")
		}
		if o.action == podcastJSONFavorite {
			err = client.AddPodcastFavorite(show)
		} else {
			err = client.RemovePodcastFavorite(show)
		}
	} else {
		item, err = selectPodcastJSONEpisode(client, o, show, episodeName)
		if err != nil {
			return err
		}
		if item == nil || item.Type != kefw2.ContentTypeAudio || strings.TrimSpace(item.Path) == "" {
			return fmt.Errorf("selection is not an audio episode with an exact path")
		}
		if o.action == ModeQueue {
			// Append, never clear/replace the queue or explicitly start playback.
			err = client.AddToQueue([]kefw2.ContentItem{*item}, false)
		} else {
			err = client.PlayPodcastEpisodeExact(item)
		}
	}
	if err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Item   contentSummary `json:"item"`
		Latest bool           `json:"latest"`
	}{actionResult{podcastJSONActionName(o.action), true}, summarizeContent([]kefw2.ContentItem{*item})[0], o.latest})
}

func podcastJSONActionName(action string) string {
	switch action {
	case podcastJSONFavorite:
		return "add_podcast_favorite"
	case podcastJSONRemove:
		return "remove_podcast_favorite"
	default:
		return action + "_podcast"
	}
}

func selectPodcastJSONEpisode(
	client *kefw2.AirableClient, o podcastJSONOptions, show *kefw2.ContentItem, episodeName string,
) (*kefw2.ContentItem, error) {
	if show == nil {
		if o.episodePath != "" {
			return client.GetPodcastDetailsExact(o.episodePath)
		}
		return nil, fmt.Errorf("an episode selection is required")
	}
	if !o.latest && o.episodePath == "" && episodeName == "" {
		return nil, fmt.Errorf("show playback requires --latest, --episode-path or a show/episode name; list with 'podcast episodes --path SHOW_PATH --json'")
	}
	episodes, err := client.GetPodcastEpisodesAll(show.Path)
	if err != nil {
		return nil, err
	}
	audio := podcastAudio(episodes.Rows)
	if o.latest {
		if len(audio) == 0 {
			return nil, fmt.Errorf("no audio episodes found for %q", show.Title)
		}
		return &audio[0], nil // Explicit --latest: service order, not a name fallback.
	}
	if o.episodePath != "" {
		return podcastExactEpisode(audio, o.episodePath)
	}
	return findUniqueContent(audio, episodeName)
}

func podcastExactEpisode(items []kefw2.ContentItem, path string) (*kefw2.ContentItem, error) {
	var match *kefw2.ContentItem
	for i := range items {
		if items[i].Path == path {
			if match != nil {
				return nil, fmt.Errorf("duplicate episode path %q", path)
			}
			match = &items[i]
		}
	}
	if match == nil {
		return nil, fmt.Errorf("episode path %q not found in selected show", path)
	}
	return match, nil
}
