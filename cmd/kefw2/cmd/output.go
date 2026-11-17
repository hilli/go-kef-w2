// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

type speakerIdentity struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
	Model     string `json:"model"`
}

type commandResponse struct {
	Speaker *speakerIdentity `json:"speaker,omitempty"`
	Data    any              `json:"data"`
}

func identifySpeaker(s *kefw2.KEFSpeaker) speakerIdentity {
	return speakerIdentity{Name: s.Name, IPAddress: s.IPAddress, Model: s.Model}
}

func jsonOutput(cmd *cobra.Command) bool {
	enabled, _ := cmd.Flags().GetBool("json")
	return enabled
}

func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Output JSON without interactive prompts or decoration")
}

func writeJSON(cmd *cobra.Command, data any) error {
	return encodeResponse(cmd, commandResponse{Data: data})
}

func writeSpeakerJSON(cmd *cobra.Command, data any) error {
	speaker := identifySpeaker(currentSpeaker)
	return encodeResponse(cmd, commandResponse{Speaker: &speaker, Data: data})
}

func encodeResponse(cmd *cobra.Command, response commandResponse) error {
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(response); err != nil {
		return fmt.Errorf("writing JSON output: %w", err)
	}
	return nil
}

type actionResult struct {
	Action   string `json:"action"`
	Accepted bool   `json:"accepted"`
}

func setJSONHandler(cmd *cobra.Command, handler func(*cobra.Command, []string) error) {
	run, runE := cmd.Run, cmd.RunE
	addJSONFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if jsonOutput(cmd) {
			return handler(cmd, args)
		}
		if runE != nil {
			return runE(cmd, args)
		}
		run(cmd, args)
		return nil
	}
}

func requireJSONFlags(cmd *cobra.Command, flags ...string) {
	validate := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if !jsonOutput(cmd) {
			for _, flag := range flags {
				if cmd.Flags().Changed(flag) {
					return fmt.Errorf("--%s requires --json", flag)
				}
			}
		}
		if validate != nil {
			return validate(cmd, args)
		}
		return nil
	}
}

type contentSummary struct {
	Title       string `json:"title"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
}

func summarizeContent(items []kefw2.ContentItem) []contentSummary {
	result := make([]contentSummary, 0, len(items))
	for _, item := range items {
		entry := contentSummary{
			Title: item.Title, Path: item.Path, Type: item.Type, Description: item.LongDescription,
		}
		if item.MediaData != nil {
			entry.Artist = item.MediaData.MetaData.Artist
			entry.Album = item.MediaData.MetaData.Album
		}
		result = append(result, entry)
	}
	return result
}

type contentPage struct {
	Items      []contentSummary `json:"items"`
	Offset     int              `json:"offset"`
	Limit      int              `json:"limit"`
	TotalRows  int              `json:"total_rows"`
	NextOffset int              `json:"next_offset"`
	HasMore    bool             `json:"has_more"`
}

func writeContentPage(cmd *cobra.Command, rows *kefw2.RowsResponse, offset, limit int) error {
	next := offset + len(rows.Rows)
	return writeSpeakerJSON(cmd, contentPage{
		Items: summarizeContent(rows.Rows), Offset: offset, Limit: limit,
		TotalRows: rows.RowsCount, NextOffset: next, HasMore: next < rows.RowsCount,
	})
}

func findUniqueContent(items []kefw2.ContentItem, query string) (*kefw2.ContentItem, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("content name must not be empty")
	}
	exact := make([]kefw2.ContentItem, 0)
	partial := make([]kefw2.ContentItem, 0)
	for _, item := range items {
		if strings.EqualFold(item.Title, query) {
			exact = append(exact, item)
		} else if strings.Contains(strings.ToLower(item.Title), strings.ToLower(query)) {
			partial = append(partial, item)
		}
	}
	matches := partial
	if len(exact) > 0 {
		matches = exact
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("content %q not found", query)
	}
	return nil, fmt.Errorf("content %q is ambiguous; use an exact path or numeric index", query)
}
