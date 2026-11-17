// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	upnpPlayAction = "play"
	upnpQueueFlag  = "queue"
)

func init() {
	setJSONHandler(upnpCmd, upnpServersJSON)
	setJSONHandler(upnpBrowseCmd, upnpBrowseJSON)
	setJSONHandler(upnpPlayCmd, upnpPlayJSON)
	setJSONHandler(upnpSearchCmd, upnpSearchJSON)
	setJSONHandler(upnpIndexCmd, upnpIndexJSON)
	setJSONHandler(upnpServerListCmd, upnpServersJSON)
	setJSONHandler(upnpServerDefaultCmd, upnpDefaultJSON)
	setJSONHandler(upnpContainerBrowseCmd, upnpContainerJSON)
	setJSONHandler(upnpContainerIndexCmd, upnpContainerJSON)

	for _, cmd := range []*cobra.Command{
		upnpBrowseCmd, upnpPlayCmd, upnpIndexCmd, upnpServerDefaultCmd,
		upnpContainerBrowseCmd, upnpContainerIndexCmd,
	} {
		cmd.Flags().String("path", "", "Opaque API path from this speaker (JSON only; not a durable ID)")
	}
	for _, cmd := range []*cobra.Command{upnpCmd, upnpServerListCmd, upnpBrowseCmd, upnpSearchCmd} {
		cmd.Flags().Int("offset", 0, "JSON result offset")
		cmd.Flags().Int("limit", 100, "Maximum JSON results (1-1000)")
	}
	upnpPlayCmd.Args = func(cmd *cobra.Command, args []string) error {
		if jsonOutput(cmd) {
			return cobra.MaximumNArgs(1)(cmd, args)
		}
		return cobra.MinimumNArgs(1)(cmd, args)
	}
	for _, cmd := range []*cobra.Command{
		upnpCmd, upnpBrowseCmd, upnpPlayCmd, upnpSearchCmd, upnpIndexCmd,
		upnpServerListCmd, upnpServerDefaultCmd, upnpContainerBrowseCmd, upnpContainerIndexCmd,
	} {
		originalArgs := cmd.Args
		cmd.Args = func(cmd *cobra.Command, args []string) error {
			if !jsonOutput(cmd) {
				for _, flag := range []string{"path", "offset", "limit"} {
					if cmd.Flags().Changed(flag) {
						return fmt.Errorf("--%s requires --json", flag)
					}
				}
			}
			if originalArgs != nil {
				return originalArgs(cmd, args)
			}
			return nil
		}
	}
}

func upnpJSONClient() (*kefw2.AirableClient, error) {
	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, fmt.Errorf("a speaker is required for this UPnP operation")
	}
	return kefw2.NewAirableClient(currentSpeaker, kefw2.WithHTTPTimeout(60*time.Second)), nil
}

func upnpPageArgs(cmd *cobra.Command) (int, int, error) {
	offset, _ := cmd.Flags().GetInt("offset")
	limit, _ := cmd.Flags().GetInt("limit")
	if offset < 0 || limit < 1 || limit > 1000 || offset > math.MaxInt-limit {
		return 0, 0, fmt.Errorf("offset must be non-negative and limit must be between 1 and 1000")
	}
	return offset, limit, nil
}

func upnpPage(items []kefw2.ContentItem, offset, limit int) *kefw2.RowsResponse {
	start := min(offset, len(items))
	end := start + min(limit, len(items)-start)
	return &kefw2.RowsResponse{Rows: items[start:end], RowsCount: len(items)}
}

func upnpServerItems(client *kefw2.AirableClient) ([]kefw2.ContentItem, error) {
	rows, err := client.GetAllRows("ui:/upnp")
	if err != nil {
		return nil, err
	}
	items := make([]kefw2.ContentItem, 0, len(rows.Rows))
	for _, item := range rows.Rows {
		if item.Type != TypeQuery {
			items = append(items, item)
		}
	}
	return items, nil
}

func upnpServersJSON(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	offset, limit, err := upnpPageArgs(cmd)
	if err != nil {
		return err
	}
	client, err := upnpJSONClient()
	if err != nil {
		return err
	}
	items, err := upnpServerItems(client)
	if err != nil {
		return err
	}
	return writeContentPage(cmd, upnpPage(items, offset, limit), offset, limit)
}

func validateUPnPAPIPath(path string) error {
	if !strings.HasPrefix(path, "upnp:/") {
		return fmt.Errorf("expected a speaker-provided upnp:/ API path")
	}
	return nil
}

// Positional paths remain human slash paths; opaque paths are always explicit.
func upnpPathArg(cmd *cobra.Command, args []string) (path string, opaque bool, err error) {
	if len(args) > 1 {
		return "", false, fmt.Errorf("expected at most one display path")
	}
	if cmd.Flags().Changed("path") {
		if len(args) != 0 {
			return "", false, fmt.Errorf("--path cannot be combined with a positional display path")
		}
		path, _ = cmd.Flags().GetString("path")
		return path, true, validateUPnPAPIPath(path)
	}
	if len(args) == 1 {
		path = args[0]
		if strings.Contains(path, ":/") {
			return "", false, fmt.Errorf("use --path for an opaque API path")
		}
	}
	return path, false, nil
}

func upnpDefaultServerPath() (string, error) {
	path := viper.GetString("upnp.default_server_path")
	if path == "" {
		return "", fmt.Errorf("no default UPnP server configured; run 'kefw2 config upnp server default <name>'")
	}
	return path, validateUPnPAPIPath(path)
}

// Resolve every title uniquely, including the final track. Never select the first match.
func resolveUPnPJSONPath(client *kefw2.AirableClient, display string, containersOnly bool) (string, error) {
	path, err := upnpDefaultServerPath()
	if err != nil {
		return "", err
	}
	parts := strings.Split(display, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(part, "%2F", "/")
		rows, err := client.BrowseContainerAll(path)
		if err != nil {
			return "", err
		}
		items := make([]kefw2.ContentItem, 0, len(rows.Rows))
		for _, item := range rows.Rows {
			if item.Type == TypeContainer || (!containersOnly && i == len(parts)-1 && item.Type == TypeAudio) {
				items = append(items, item)
			}
		}
		item, err := findUniqueContent(items, part)
		if err != nil {
			return "", err
		}
		if err := validateUPnPAPIPath(item.Path); err != nil {
			return "", err
		}
		path = item.Path
	}
	return path, nil
}

func upnpBrowseJSON(cmd *cobra.Command, args []string) error {
	queue, _ := cmd.Flags().GetBool(upnpQueueFlag)
	if queue {
		return fmt.Errorf("JSON browse is read-only; use 'upnp play --path <path> --queue --json' for explicit selection")
	}
	path, opaque, err := upnpPathArg(cmd, args)
	if err != nil {
		return err
	}
	offset, limit, err := upnpPageArgs(cmd)
	if err != nil {
		return err
	}
	client, err := upnpJSONClient()
	if err != nil {
		return err
	}
	if !opaque {
		if len(args) == 0 {
			path = viper.GetString("upnp.browse_container")
			if strings.HasPrefix(path, "upnp:/") {
				opaque = true
			}
			if path == "" && viper.GetString("upnp.default_server_path") == "" {
				return upnpServersJSON(cmd, nil)
			}
		}
		if !opaque {
			path, err = resolveUPnPJSONPath(client, path, true)
			if err != nil {
				return err
			}
		}
	}
	if err := validateUPnPAPIPath(path); err != nil {
		return err
	}
	rows, err := client.BrowseContainerPaged(path, offset, offset+limit-1)
	if err != nil {
		return err
	}
	return writeContentPage(cmd, rows, offset, limit)
}

func upnpPlayJSON(cmd *cobra.Command, args []string) error {
	path, opaque, err := upnpPathArg(cmd, args)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("provide a display path or --path for explicit playback")
	}
	client, err := upnpJSONClient()
	if err != nil {
		return err
	}
	if !opaque {
		path, err = resolveUPnPJSONPath(client, path, false)
		if err != nil {
			return err
		}
	}
	tracks, err := client.GetContainerTracksRecursive(path)
	if err != nil {
		return fmt.Errorf("collecting tracks: %w", err)
	}
	if len(tracks) == 0 {
		return fmt.Errorf("no audio tracks found at %q", path)
	}
	action := upnpPlayAction
	queue, _ := cmd.Flags().GetBool(upnpQueueFlag)
	if queue {
		action = upnpQueueFlag
		err = client.AddToQueue(tracks, true)
	} else {
		err = client.PlayUPnPTracks(tracks)
	}
	if err != nil {
		return err
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		Path       string `json:"path"`
		TrackCount int    `json:"track_count"`
	}{actionResult{Action: action, Accepted: true}, path, len(tracks)})
}

func loadUPnPJSONIndex() (*kefw2.TrackIndex, error) {
	index, err := kefw2.LoadTrackIndex()
	if err != nil {
		return nil, fmt.Errorf("loading UPnP index: %w", err)
	}
	if index == nil {
		return nil, fmt.Errorf("UPnP index not built; run 'kefw2 upnp index --rebuild' to create it")
	}
	if index.Tracks == nil {
		index.Tracks = make([]kefw2.IndexedTrack, 0)
	}
	return index, nil
}

func upnpSearchJSON(cmd *cobra.Command, args []string) error {
	queue, _ := cmd.Flags().GetBool(upnpQueueFlag)
	if queue {
		return fmt.Errorf("JSON search is read-only; use 'upnp play --path <path> --queue --json' for explicit selection")
	}
	offset, limit, err := upnpPageArgs(cmd)
	if err != nil {
		return err
	}
	index, err := loadUPnPJSONIndex()
	if err != nil {
		return err
	}
	tracks := index.Tracks
	query := strings.Join(args, " ")
	if query != "" {
		tracks = kefw2.SearchTracks(index, query, len(index.Tracks))
	}
	items := make([]kefw2.ContentItem, 0, len(tracks))
	for _, track := range tracks {
		items = append(items, kefw2.IndexedTrackToContentItem(&track))
	}
	page := upnpPage(items, offset, limit)
	return writeJSON(cmd, struct {
		contentPage
		ServerPath string    `json:"server_path"`
		ServerName string    `json:"server_name"`
		IndexedAt  time.Time `json:"indexed_at"`
	}{
		contentPage{Items: summarizeContent(page.Rows), Offset: offset, Limit: limit,
			TotalRows: page.RowsCount, NextOffset: offset + len(page.Rows), HasMore: offset+len(page.Rows) < page.RowsCount},
		index.ServerPath, index.ServerName, index.IndexedAt,
	})
}

func upnpIndexJSON(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	rebuild, _ := cmd.Flags().GetBool("rebuild")
	container, _ := cmd.Flags().GetString("container")
	if cmd.Flags().Changed("path") && cmd.Flags().Changed("container") {
		return fmt.Errorf("--path cannot be combined with --container")
	}
	if cmd.Flags().Changed("container") && strings.Contains(container, ":/") {
		return fmt.Errorf("use --path for an opaque API path; --container accepts a display path")
	}
	if !rebuild {
		if cmd.Flags().Changed("path") || cmd.Flags().Changed("container") {
			return fmt.Errorf("--container and --path require --rebuild")
		}
		index, err := loadUPnPJSONIndex()
		if err != nil {
			return err
		}
		return writeJSON(cmd, index)
	}
	client, err := upnpJSONClient()
	if err != nil {
		return err
	}
	serverPath, err := upnpDefaultServerPath()
	if err != nil {
		return err
	}
	var startPath string
	if cmd.Flags().Changed("path") {
		startPath, _, err = upnpPathArg(cmd, nil)
	} else {
		if !cmd.Flags().Changed("container") {
			container = viper.GetString("upnp.index_container")
		}
		if strings.HasPrefix(container, "upnp:/") {
			startPath = container
		} else {
			startPath, err = resolveUPnPJSONPath(client, container, true)
		}
	}
	if err != nil {
		return err
	}
	if err := validateUPnPAPIPath(startPath); err != nil {
		return err
	}
	index, err := kefw2.BuildTrackIndex(client, startPath, viper.GetString("upnp.default_server"), "", nil)
	if err != nil {
		return fmt.Errorf("UPnP index incomplete; previous index was not replaced: %w", err)
	}
	index.ServerPath = serverPath
	index.ContainerName = container
	if err := kefw2.SaveTrackIndex(index); err != nil {
		return fmt.Errorf("saving UPnP index: %w", err)
	}
	return writeSpeakerJSON(cmd, index)
}

func upnpDefaultJSON(cmd *cobra.Command, args []string) error {
	path, opaque, err := upnpPathArg(cmd, args)
	if err != nil {
		return err
	}
	if len(args) != 0 || opaque {
		client, err := upnpJSONClient()
		if err != nil {
			return err
		}
		items, err := upnpServerItems(client)
		if err != nil {
			return err
		}
		var server *kefw2.ContentItem
		if opaque {
			for i := range items {
				if items[i].Path == path {
					if server != nil {
						return fmt.Errorf("server path %q is ambiguous", path)
					}
					server = &items[i]
				}
			}
			if server == nil {
				return fmt.Errorf("server path %q not found on this speaker", path)
			}
		} else {
			server, err = findUniqueContent(items, path)
			if err != nil {
				return err
			}
		}
		if err := validateUPnPAPIPath(server.Path); err != nil {
			return err
		}
		viper.Set("upnp.default_server", server.Title)
		viper.Set("upnp.default_server_path", server.Path)
		if err := viper.WriteConfig(); err != nil {
			return fmt.Errorf("saving UPnP default: %w", err)
		}
	}
	result := struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}{viper.GetString("upnp.default_server"), viper.GetString("upnp.default_server_path")}
	if len(args) != 0 || opaque {
		return writeSpeakerJSON(cmd, result)
	}
	return writeJSON(cmd, result)
}

func upnpContainerJSON(cmd *cobra.Command, args []string) error {
	path, opaque, err := upnpPathArg(cmd, args)
	if err != nil {
		return err
	}
	key := "upnp.browse_container"
	if cmd == upnpContainerIndexCmd {
		key = "upnp.index_container"
	}
	if len(args) != 0 || opaque {
		if path != "" {
			if _, err := upnpDefaultServerPath(); err != nil {
				return err
			}
			client, err := upnpJSONClient()
			if err != nil {
				return err
			}
			resolved := path
			if !opaque {
				resolved, err = resolveUPnPJSONPath(client, path, true)
				if err != nil {
					return err
				}
			}
			rows, err := client.BrowseContainerAll(resolved)
			if err != nil {
				return err
			}
			if rows.Roles != nil && rows.Roles.Type == TypeAudio {
				return fmt.Errorf("expected a container, got an audio track")
			}
		}
		viper.Set(key, path)
		if err := viper.WriteConfig(); err != nil {
			return fmt.Errorf("saving UPnP container: %w", err)
		}
	}
	result := struct {
		Path string `json:"path"`
	}{viper.GetString(key)}
	if (len(args) != 0 || opaque) && path != "" {
		return writeSpeakerJSON(cmd, result)
	}
	return writeJSON(cmd, result)
}
