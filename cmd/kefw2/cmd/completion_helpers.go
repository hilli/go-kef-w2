/*
Copyright © 2023-2026 Jens Hilligsoe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

// ============================================
// Hierarchical Path Completion
// ============================================
//
// This pattern allows shell-style tab completion through a tree of options,
// similar to navigating a filesystem but for API-driven data.
//
// Example usage:
//   kefw2 radio browse <TAB>                    -> shows top-level categories
//   kefw2 radio browse "by Genre/"<TAB>         -> shows genres
//   kefw2 radio browse "by Genre/Jazz"<ENTER>   -> shows fuzzy picker for Jazz stations
//
// Path escaping:
//   Names containing "/" are escaped as "%2F"
//   Example: "AC/DC" becomes "AC%2FDC" in paths

// EscapePathSegment escapes special characters for use in hierarchical paths
// "/" -> "%2F" (path separator)
// ":" -> "%3A" (zsh word separator)
func EscapePathSegment(s string) string {
	s = strings.ReplaceAll(s, "/", "%2F")
	s = strings.ReplaceAll(s, ":", "%3A")
	return s
}

// UnescapePathSegment unescapes special characters back to original
func UnescapePathSegment(s string) string {
	s = strings.ReplaceAll(s, "%2F", "/")
	s = strings.ReplaceAll(s, "%3A", ":")
	return s
}

// ParseHierarchicalPath splits a path like "a/b/c" into segments,
// handling %2F escapes for literal slashes in names.
func ParseHierarchicalPath(path string) []string {
	if path == "" {
		return nil
	}

	// Use a placeholder for escaped slashes, split, then unescape
	const placeholder = "\x00"
	escaped := strings.ReplaceAll(path, "%2F", placeholder)
	segments := strings.Split(escaped, "/")

	// Unescape and clean up
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.ReplaceAll(seg, placeholder, "/")
		if seg != "" { // Skip empty segments
			result = append(result, seg)
		}
	}
	return result
}

// BuildHierarchicalPath joins segments with "/", escaping slashes in names
func BuildHierarchicalPath(segments []string) string {
	escaped := make([]string, len(segments))
	for i, seg := range segments {
		escaped[i] = EscapePathSegment(seg)
	}
	return strings.Join(escaped, "/")
}

// FuzzyMatch checks if all characters in pattern appear in str in order.
// Both strings are compared case-insensitively.
// Example: FuzzyMatch("WDVX Radio", "wdx") returns true
func FuzzyMatch(str, pattern string) bool {
	str = strings.ToLower(str)
	pattern = strings.ToLower(pattern)
	pi := 0
	for _, c := range str {
		if pi < len(pattern) && c == rune(pattern[pi]) {
			pi++
		}
	}
	return pi == len(pattern)
}

// ============================================
// Radio Browse Completion
// ============================================

// HierarchicalRadioCompletion provides tab completion for radio browse paths
func HierarchicalRadioCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Parse the path to determine parent directory and partial segment
	// Examples:
	//   ""           -> parent="", partial=""      (show all top-level)
	//   "Fav"        -> parent="", partial="Fav"   (filter top-level by "Fav")
	//   "by Genre/"  -> parent="by Genre", partial="" (show all in "by Genre")
	//   "by Genre/Ja" -> parent="by Genre", partial="Ja" (filter "by Genre" by "Ja")

	parentPath := ""
	partial := ""

	if toComplete != "" {
		if strings.HasSuffix(toComplete, "/") {
			// User typed "something/" - browse into that directory
			parentPath = strings.TrimSuffix(toComplete, "/")
			partial = ""
		} else {
			// User typed partial text - find the last "/" to split parent from partial
			lastSlash := strings.LastIndex(toComplete, "/")
			if lastSlash >= 0 {
				parentPath = toComplete[:lastSlash]
				partial = toComplete[lastSlash+1:]
			} else {
				// No slash - this is a partial at top level
				parentPath = ""
				partial = toComplete
			}
		}
	}

	// Unescape the partial segment (in case user typed %2F)
	partial = UnescapePathSegment(partial)

	// Get items from parent path (check cache first)
	var items []CachedItem
	if browseCache != nil {
		if cached, ok := browseCache.Get(parentPath, "radio"); ok {
			items = cached
		}
	}

	// If not in cache, fetch from API
	if items == nil {
		client := kefw2.NewAirableClient(currentSpeaker)
		resp, err := client.BrowseRadioByDisplayPath(parentPath)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		items = convertRowsToCachedItems(resp.Rows, parentPath)

		// Cache the result
		if browseCache != nil {
			browseCache.Set(parentPath, "radio", items)
		}
	}

	// Filter items by partial match if there's a partial segment
	if partial != "" {
		items = FilterItemsByPrefix(items, partial)
	}

	return buildRadioCompletions(items, parentPath), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// convertRowsToCachedItems converts API rows to cached items for completion
// parentDisplayPath is the display path of the parent (for building full display paths)
func convertRowsToCachedItems(rows []kefw2.ContentItem, parentDisplayPath string) []CachedItem {
	items := make([]CachedItem, 0, len(rows))
	for _, row := range rows {
		itemType := "playable"
		if row.Type == "container" && !row.ContainerPlayable {
			itemType = "container"
		}

		// Build the display path for this item
		displayPath := EscapePathSegment(row.Title)
		if parentDisplayPath != "" {
			displayPath = parentDisplayPath + "/" + displayPath
		}

		items = append(items, CachedItem{
			Title:       row.Title,
			Path:        row.Path,
			DisplayPath: displayPath,
			Type:        itemType,
			Description: row.LongDescription,
		})
	}
	return items
}

// buildRadioCompletions builds completion strings from cached items
// Returns full paths (required for shell to replace the argument correctly)
// Uses Cobra's "completion\tdescription" format for cleaner display
func buildRadioCompletions(items []CachedItem, currentPath string) []string {
	completions := make([]string, 0, len(items))

	prefix := ""
	if currentPath != "" {
		prefix = currentPath + "/"
	}

	for _, item := range items {
		// Escape "/" in names
		escapedTitle := EscapePathSegment(item.Title)
		fullPath := prefix + escapedTitle

		// Add "/" suffix for containers to hint "more inside"
		if item.Type == "container" {
			fullPath += "/"
		}

		// Use tab-separated format: "completion\tdescription"
		// The description shows just the name, making the display cleaner
		completion := fullPath + "\t" + item.Title
		if item.Type == "container" {
			completion = fullPath + "\t" + item.Title + "/"
		}

		completions = append(completions, completion)
	}

	return completions
}

// ============================================
// Utility Functions for Completion
// ============================================

// FilterItemsByType returns only items matching the given type
func FilterItemsByType(items []CachedItem, itemType string) []CachedItem {
	filtered := make([]CachedItem, 0)
	for _, item := range items {
		if item.Type == itemType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// FilterItemsByPrefix returns items whose title starts with the given prefix
func FilterItemsByPrefix(items []CachedItem, prefix string) []CachedItem {
	if prefix == "" {
		return items
	}

	prefix = strings.ToLower(prefix)
	filtered := make([]CachedItem, 0)
	for _, item := range items {
		if strings.HasPrefix(strings.ToLower(item.Title), prefix) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// ============================================
// Podcast Browse Completion
// ============================================

// PodcastBrowseCategories is the list of available browse categories
var PodcastBrowseCategories = []string{
	"popular\tPopular podcasts",
	"trending\tTrending podcasts",
	"history\tRecently played podcasts",
	"favorites\tYour favorite podcasts",
}

// PodcastBrowseCompletion provides tab completion for podcast browse categories
func PodcastBrowseCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Filter categories by what user has typed
	if toComplete == "" {
		return PodcastBrowseCategories, cobra.ShellCompDirectiveNoFileComp
	}

	filtered := make([]string, 0)
	lowerComplete := strings.ToLower(toComplete)
	for _, cat := range PodcastBrowseCategories {
		// Categories are in "name\tdescription" format
		parts := strings.SplitN(cat, "\t", 2)
		if strings.HasPrefix(parts[0], lowerComplete) {
			filtered = append(filtered, cat)
		}
	}

	return filtered, cobra.ShellCompDirectiveNoFileComp
}

// ============================================
// Radio Station Completion Functions
// ============================================

// RadioLocalCompletion provides tab completion for local radio stations
func RadioLocalCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.GetRadioLocalAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// RadioFavoritesCompletion provides tab completion for favorite radio stations
func RadioFavoritesCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.GetRadioFavoritesAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// RadioTrendingCompletion provides tab completion for trending radio stations
func RadioTrendingCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.GetRadioTrendingAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// RadioHQCompletion provides tab completion for high quality radio stations
func RadioHQCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.GetRadioHQAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// RadioNewCompletion provides tab completion for new radio stations
func RadioNewCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.GetRadioNewAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// RadioPopularCompletion provides tab completion for popular radio stations
func RadioPopularCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Don't complete if we already have an argument
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.GetRadioPopularAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// DynamicRadioSearchCompletion provides dynamic completion for radio search by querying the API
func DynamicRadioSearchCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Need at least 2 characters to search
	if len(toComplete) < 2 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)
	resp, err := client.SearchRadio(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildStationCompletions(resp.Rows, ""), cobra.ShellCompDirectiveNoFileComp
}

// buildStationCompletions builds completion strings from radio station rows
func buildStationCompletions(rows []kefw2.ContentItem, filter string) []string {
	completions := make([]string, 0, len(rows))
	lowerFilter := strings.ToLower(filter)

	for _, row := range rows {
		// Only include playable stations
		if !isPlayableStation(row) {
			continue
		}

		// Filter by what user has typed
		if filter != "" && !strings.HasPrefix(strings.ToLower(row.Title), lowerFilter) {
			continue
		}

		// Use tab-separated format: "completion\tdescription"
		description := row.LongDescription
		if description == "" {
			description = row.AudioType
		}
		if description != "" {
			completions = append(completions, row.Title+"\t"+description)
		} else {
			completions = append(completions, row.Title)
		}

		// Limit suggestions for shell UX
		if len(completions) >= 100 {
			break
		}
	}

	return completions
}

// isPlayableStation checks if a content item is a playable radio station
func isPlayableStation(item kefw2.ContentItem) bool {
	return (item.ContainerPlayable && item.AudioType == "audioBroadcast") || item.Type == "audio"
}

// ============================================
// Podcast Completion Functions
// ============================================

// PodcastPopularCompletion provides tab completion for popular podcasts
// Supports hierarchical completion: "ShowName/" triggers episode listing
func PodcastPopularCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	return buildPodcastCompletionsWithEpisodes(client, func() (*kefw2.RowsResponse, error) {
		return client.GetPodcastPopularAll()
	}, toComplete)
}

// PodcastTrendingCompletion provides tab completion for trending podcasts
func PodcastTrendingCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	return buildPodcastCompletionsWithEpisodes(client, func() (*kefw2.RowsResponse, error) {
		return client.GetPodcastTrendingAll()
	}, toComplete)
}

// PodcastHistoryCompletion provides tab completion for podcast history
func PodcastHistoryCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	return buildPodcastCompletionsWithEpisodes(client, func() (*kefw2.RowsResponse, error) {
		return client.GetPodcastHistoryAll()
	}, toComplete)
}

// PodcastFavoritesCompletion provides tab completion for favorite podcasts
func PodcastFavoritesCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	return buildPodcastCompletionsWithEpisodes(client, func() (*kefw2.RowsResponse, error) {
		return client.GetPodcastFavoritesAll()
	}, toComplete)
}

// DynamicPodcastSearchCompletion provides dynamic completion for podcast search
func DynamicPodcastSearchCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Need at least 2 characters to search
	if len(toComplete) < 2 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)
	resp, err := client.SearchPodcasts(toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildPodcastCompletions(resp.Rows, ""), cobra.ShellCompDirectiveNoFileComp
}

// PodcastPlayCompletion provides completion for podcast play command.
// Shows favorites when empty, searches when 2+ characters typed, shows episodes after "ShowName/".
func PodcastPlayCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())

	// If path contains "/", show episodes for that podcast
	if strings.Contains(toComplete, "/") {
		lastSlash := strings.LastIndex(toComplete, "/")
		showName := UnescapePathSegment(toComplete[:lastSlash])
		episodePartial := ""
		if lastSlash < len(toComplete)-1 {
			episodePartial = toComplete[lastSlash+1:]
		}

		// Try to find the podcast in favorites first
		resp, err := client.GetPodcastFavoritesAll()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var podcast *kefw2.ContentItem
		for i := range resp.Rows {
			if resp.Rows[i].Title == showName {
				podcast = &resp.Rows[i]
				break
			}
		}

		// If not in favorites, search for it
		if podcast == nil {
			searchResp, err := client.SearchPodcasts(showName)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			for i := range searchResp.Rows {
				if searchResp.Rows[i].Title == showName {
					podcast = &searchResp.Rows[i]
					break
				}
			}
		}

		if podcast == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Get episodes for this podcast
		episodes, err := client.GetPodcastEpisodesAll(podcast.Path)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return buildEpisodeCompletions(episodes.Rows, showName, episodePartial), cobra.ShellCompDirectiveNoFileComp
	}

	// If user has typed 2+ characters (no "/"), search for podcasts
	if len(toComplete) >= 2 {
		resp, err := client.SearchPodcasts(toComplete)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return buildPodcastCompletions(resp.Rows, ""), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}

	// Otherwise show favorites as suggestions
	resp, err := client.GetPodcastFavoritesAll()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildPodcastCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// PodcastFilterCompletion provides hierarchical completion for podcast filter paths
func PodcastFilterCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Parse the path to determine parent directory and partial segment
	parentPath := ""
	partial := ""

	if toComplete != "" {
		if strings.HasSuffix(toComplete, "/") {
			parentPath = strings.TrimSuffix(toComplete, "/")
			partial = ""
		} else {
			lastSlash := strings.LastIndex(toComplete, "/")
			if lastSlash >= 0 {
				parentPath = toComplete[:lastSlash]
				partial = toComplete[lastSlash+1:]
			} else {
				parentPath = ""
				partial = toComplete
			}
		}
	}

	partial = UnescapePathSegment(partial)

	client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
	resp, err := client.BrowsePodcastByDisplayPath(parentPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Convert to cached items for filtering
	items := convertRowsToCachedItems(resp.Rows, parentPath)

	// Filter by partial match
	if partial != "" {
		items = FilterItemsByPrefix(items, partial)
	}

	// Build completions using the existing radio completion builder
	// (works the same way - full paths with "/" for containers)
	return buildRadioCompletions(items, parentPath), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// buildPodcastCompletionsWithEpisodes handles two-level completion:
// 1. At top level, shows podcast names with "/" suffix
// 2. After "ShowName/", shows episodes of that show
func buildPodcastCompletionsWithEpisodes(client *kefw2.AirableClient, fetchPodcasts func() (*kefw2.RowsResponse, error), toComplete string) ([]string, cobra.ShellCompDirective) {
	// Check if we're completing an episode (path contains "/")
	if strings.Contains(toComplete, "/") {
		// User typed "ShowName/" or "ShowName/partial" - show episodes
		lastSlash := strings.LastIndex(toComplete, "/")
		showName := UnescapePathSegment(toComplete[:lastSlash])
		episodePartial := ""
		if lastSlash < len(toComplete)-1 {
			episodePartial = toComplete[lastSlash+1:]
		}

		// First get the podcasts to find the matching show
		resp, err := fetchPodcasts()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Find the podcast by name
		var podcast *kefw2.ContentItem
		for i := range resp.Rows {
			if resp.Rows[i].Title == showName {
				podcast = &resp.Rows[i]
				break
			}
		}

		if podcast == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Get episodes for this podcast
		episodes, err := client.GetPodcastEpisodesAll(podcast.Path)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Build episode completions
		return buildEpisodeCompletions(episodes.Rows, showName, episodePartial), cobra.ShellCompDirectiveNoFileComp
	}

	// Top level - show podcasts
	resp, err := fetchPodcasts()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildPodcastCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// buildPodcastCompletions builds completion strings from podcast container rows
func buildPodcastCompletions(rows []kefw2.ContentItem, filter string) []string {
	completions := make([]string, 0, len(rows))
	lowerFilter := strings.ToLower(filter)

	for _, row := range rows {
		// Only include podcast containers
		if row.Type != "container" {
			continue
		}

		// Filter by what user has typed
		if filter != "" && !strings.HasPrefix(strings.ToLower(row.Title), lowerFilter) {
			continue
		}

		// Escape "/" in title and add "/" to indicate episodes available
		escapedTitle := EscapePathSegment(row.Title)
		fullPath := escapedTitle + "/"

		// Use tab-separated format: "completion\tdescription"
		description := row.LongDescription
		if description == "" {
			description = "Podcast"
		}
		completions = append(completions, fullPath+"\t"+row.Title)

		// Limit suggestions for shell UX
		if len(completions) >= 100 {
			break
		}
	}

	return completions
}

// buildEpisodeCompletions builds completion strings for podcast episodes
func buildEpisodeCompletions(rows []kefw2.ContentItem, showName string, filter string) []string {
	completions := make([]string, 0, len(rows))
	// Unescape the filter since it may contain %3A for ":"
	unescapedFilter := UnescapePathSegment(filter)
	lowerFilter := strings.ToLower(unescapedFilter)
	showPrefix := EscapePathSegment(showName) + "/"

	for _, row := range rows {
		// Filter by what user has typed (compare against unescaped title)
		if unescapedFilter != "" && !strings.HasPrefix(strings.ToLower(row.Title), lowerFilter) {
			continue
		}

		// Build full path: "ShowName/EpisodeTitle"
		escapedTitle := EscapePathSegment(row.Title)
		fullPath := showPrefix + escapedTitle

		// Use tab-separated format
		completions = append(completions, fullPath+"\t"+row.Title)

		// Limit suggestions
		if len(completions) >= 100 {
			break
		}
	}

	return completions
}

// ============================================
// UPnP Browse Completion
// ============================================

// HierarchicalUPnPCompletion provides tab completion for UPnP browse/play paths.
// Uses default server from config. If no default server, shows server list at top level.
func HierarchicalUPnPCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	serverPath := viper.GetString("upnp.default_server_path")

	// Parse path: "Music/Alb" -> parent="Music", partial="Alb"
	parentPath := ""
	partial := ""

	if toComplete != "" {
		if strings.HasSuffix(toComplete, "/") {
			parentPath = strings.TrimSuffix(toComplete, "/")
			partial = ""
		} else {
			lastSlash := strings.LastIndex(toComplete, "/")
			if lastSlash >= 0 {
				parentPath = toComplete[:lastSlash]
				partial = toComplete[lastSlash+1:]
			} else {
				parentPath = ""
				partial = toComplete
			}
		}
	}

	partial = UnescapePathSegment(partial)

	// Check cache first
	cacheKey := "upnp:" + serverPath + ":" + parentPath
	var items []CachedItem
	if browseCache != nil {
		if cached, ok := browseCache.Get(parentPath, cacheKey); ok {
			items = cached
		}
	}

	if items == nil {
		client := kefw2.NewAirableClient(currentSpeaker)
		resp, err := client.BrowseUPnPByDisplayPath(parentPath, serverPath)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		items = convertUPnPRowsToCachedItems(resp.Rows, parentPath)

		if browseCache != nil {
			browseCache.Set(parentPath, cacheKey, items)
		}
	}

	if partial != "" {
		items = FilterItemsByPrefix(items, partial)
	}

	return buildUPnPCompletions(items, parentPath), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// convertUPnPRowsToCachedItems converts UPnP API rows to cached items.
// Filters out "query" type entries (like "Search").
func convertUPnPRowsToCachedItems(rows []kefw2.ContentItem, parentDisplayPath string) []CachedItem {
	items := make([]CachedItem, 0, len(rows))
	for _, row := range rows {
		if row.Type == "query" {
			continue // Skip search entries
		}

		itemType := "playable"
		if row.Type == "container" {
			itemType = "container"
		}

		displayPath := EscapePathSegment(row.Title)
		if parentDisplayPath != "" {
			displayPath = parentDisplayPath + "/" + displayPath
		}

		description := row.LongDescription
		if row.MediaData != nil && row.MediaData.MetaData.Artist != "" {
			description = row.MediaData.MetaData.Artist
		}

		items = append(items, CachedItem{
			Title:       row.Title,
			Path:        row.Path,
			DisplayPath: displayPath,
			Type:        itemType,
			Description: description,
		})
	}
	return items
}

// buildUPnPCompletions builds completion strings for UPnP items.
func buildUPnPCompletions(items []CachedItem, currentPath string) []string {
	completions := make([]string, 0, len(items))

	prefix := ""
	if currentPath != "" {
		prefix = currentPath + "/"
	}

	for _, item := range items {
		escapedTitle := EscapePathSegment(item.Title)
		fullPath := prefix + escapedTitle

		if item.Type == "container" {
			fullPath += "/"
		}

		completion := fullPath + "\t" + item.Title
		if item.Type == "container" {
			completion = fullPath + "\t" + item.Title + "/"
		}

		completions = append(completions, completion)
	}

	return completions
}

// ============================================
// Queue Item Completion
// ============================================

// QueueItemCompletion provides tab completion for queue items.
// Format: "Title - Artist" with "(2)", "(3)" for duplicates.
func QueueItemCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)
	resp, err := client.GetPlayQueue()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if len(resp.Rows) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return buildQueueCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveKeepOrder | cobra.ShellCompDirectiveNoFileComp
}

// buildQueueCompletions builds completion strings for queue items.
// Uses "Title - Artist" format with "(2)", "(3)" for duplicates.
func buildQueueCompletions(rows []kefw2.ContentItem, filter string) []string {
	completions := make([]string, 0, len(rows))
	lowerFilter := strings.ToLower(filter)
	labelCounts := make(map[string]int)

	for i, row := range rows {
		// Build label: "Title - Artist"
		label := row.Title
		artist := ""
		if row.MediaData != nil && row.MediaData.MetaData.Artist != "" {
			artist = row.MediaData.MetaData.Artist
			label = row.Title + " - " + artist
		}

		// Handle duplicates
		baseLabel := label
		labelCounts[baseLabel]++
		if labelCounts[baseLabel] > 1 {
			label = fmt.Sprintf("%s (%d)", baseLabel, labelCounts[baseLabel])
		}

		// Filter by what user has typed
		if filter != "" && !strings.HasPrefix(strings.ToLower(label), lowerFilter) {
			continue
		}

		// Use tab-separated format: "completion\tdescription"
		description := fmt.Sprintf("#%d", i+1)
		if artist != "" {
			description = fmt.Sprintf("#%d - %s", i+1, artist)
		}
		completions = append(completions, label+"\t"+description)

		// Limit suggestions
		if len(completions) >= 100 {
			break
		}
	}

	return completions
}

// QueueMoveCompletion provides tab completion for the queue move command.
// First arg: track to move (by title or position)
// Second arg: destination keywords or track title
// Third arg: target track (only for before/after)
func QueueMoveCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if currentSpeaker == nil || currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)
	resp, err := client.GetPlayQueue()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if len(resp.Rows) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	switch len(args) {
	case 0:
		// First argument: track to move
		return buildQueueCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveKeepOrder | cobra.ShellCompDirectiveNoFileComp

	case 1:
		// Second argument: destination keywords + track names
		keywords := []string{
			"top\tMove to first position",
			"bottom\tMove to last position",
			"up\tMove one position up",
			"down\tMove one position down",
			"next\tMove to play after current",
			"before\tMove before a track",
			"after\tMove after a track",
		}

		// Filter keywords by what user has typed
		lowerComplete := strings.ToLower(toComplete)
		filteredKeywords := make([]string, 0)
		for _, kw := range keywords {
			if strings.HasPrefix(strings.ToLower(kw), lowerComplete) {
				filteredKeywords = append(filteredKeywords, kw)
			}
		}

		// Also add track completions for direct position-by-name
		trackCompletions := buildQueueCompletions(resp.Rows, toComplete)

		return append(filteredKeywords, trackCompletions...), cobra.ShellCompDirectiveKeepOrder | cobra.ShellCompDirectiveNoFileComp

	case 2:
		// Third argument: only for before/after
		destLower := strings.ToLower(args[1])
		if destLower == "before" || destLower == "after" {
			return buildQueueCompletions(resp.Rows, toComplete), cobra.ShellCompDirectiveKeepOrder | cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp

	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}
