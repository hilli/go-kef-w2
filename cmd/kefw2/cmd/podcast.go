/*
Copyright © 2023-2026 Jens Hilligsøe

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

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// Styles for the podcast TUI
var (
	podcastTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("207")).
				MarginBottom(1)

	podcastSearchStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("207")).
				Padding(0, 1)

	podcastStatusStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				MarginTop(1)

	podcastSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("207"))

	podcastPlayingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("82"))
)

// podcastItem represents a podcast or episode in the list
type podcastItem struct {
	item kefw2.ContentItem
}

func (i podcastItem) Title() string       { return i.item.Title }
func (i podcastItem) Description() string { return i.item.LongDescription }
func (i podcastItem) FilterValue() string { return i.item.Title }

// podcastModel is the Bubbletea model for the podcast browser
type podcastModel struct {
	client         *kefw2.AirableClient
	searchInput    textinput.Model
	list           list.Model
	items          []kefw2.ContentItem
	loading        bool
	playing        string // Currently playing episode name
	err            error
	width          int
	height         int
	mode           string // "search", "favorites", "popular", "episodes", etc.
	currentPodcast *kefw2.ContentItem
	quitting       bool
}

// Messages for async operations
type podcastSearchResultMsg struct {
	items []kefw2.ContentItem
	err   error
}

type podcastPlayResultMsg struct {
	episodeName string
	err         error
}

func initialPodcastModel(client *kefw2.AirableClient) podcastModel {
	ti := textinput.New()
	ti.Placeholder = "Search podcasts..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	// Create delegate for list items
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = podcastSelectedStyle
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Podcasts"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)

	return podcastModel{
		client:      client,
		searchInput: ti,
		list:        l,
		mode:        "search",
	}
}

func (m podcastModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m podcastModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if !m.searchInput.Focused() || msg.String() == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
		case "enter":
			if m.searchInput.Focused() && m.searchInput.Value() != "" {
				m.loading = true
				query := m.searchInput.Value()
				return m, m.searchPodcasts(query)
			} else if !m.searchInput.Focused() {
				if item, ok := m.list.SelectedItem().(podcastItem); ok {
					if item.item.Type == "container" {
						// It's a podcast, load episodes
						m.currentPodcast = &item.item
						m.mode = "episodes"
						m.loading = true
						return m, m.loadEpisodes(item.item.Path)
					} else if item.item.Type == "audio" {
						// It's an episode, play it
						return m, m.playEpisode(&item.item)
					}
				}
			}
		case "tab":
			if m.searchInput.Focused() {
				m.searchInput.Blur()
			} else {
				m.searchInput.Focus()
			}
		case "esc":
			if m.mode == "episodes" {
				// Go back to search/browse mode
				m.mode = "search"
				m.currentPodcast = nil
				m.list.SetItems([]list.Item{})
			} else if m.searchInput.Focused() {
				m.searchInput.Blur()
			} else {
				m.searchInput.Focus()
			}
		case "backspace":
			if !m.searchInput.Focused() && m.mode == "episodes" {
				m.mode = "search"
				m.currentPodcast = nil
				m.list.SetItems([]list.Item{})
			}
		case "f":
			if !m.searchInput.Focused() {
				m.mode = "favorites"
				m.loading = true
				return m, m.loadFavorites()
			}
		case "p":
			if !m.searchInput.Focused() {
				m.mode = "popular"
				m.loading = true
				return m, m.loadPopular()
			}
		case "t":
			if !m.searchInput.Focused() {
				m.mode = "trending"
				m.loading = true
				return m, m.loadTrending()
			}
		case "h":
			if !m.searchInput.Focused() {
				m.mode = "history"
				m.loading = true
				return m, m.loadHistory()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 6
		footerHeight := 3
		listHeight := m.height - headerHeight - footerHeight
		if listHeight < 5 {
			listHeight = 5
		}
		m.list.SetSize(m.width-4, listHeight)

	case podcastSearchResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.items = msg.items
			m.err = nil
			items := make([]list.Item, len(msg.items))
			for i, s := range msg.items {
				items[i] = podcastItem{item: s}
			}
			m.list.SetItems(items)
			m.searchInput.Blur()
		}

	case podcastPlayResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.playing = msg.episodeName
			m.err = nil
		}
	}

	// Update child components
	if m.searchInput.Focused() {
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m podcastModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	title := "Podcasts"
	if m.currentPodcast != nil {
		title = fmt.Sprintf("Podcasts > %s", m.currentPodcast.Title)
	}
	b.WriteString(podcastTitleStyle.Render(title))
	b.WriteString("\n")

	// Search input (hide in episodes mode)
	if m.mode != "episodes" {
		searchBox := podcastSearchStyle.Render(m.searchInput.View())
		b.WriteString(searchBox)
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n")
	}

	// Loading or list
	if m.loading {
		b.WriteString("Loading...")
		b.WriteString("\n")
	} else {
		b.WriteString(m.list.View())
	}

	// Status bar
	var status string
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("Error: %v", m.err))
	} else if m.playing != "" {
		status = podcastPlayingStyle.Render(fmt.Sprintf("Now playing: %s", m.playing))
	} else if m.mode == "episodes" {
		status = "Enter: play | Esc/Backspace: back | q: quit"
	} else {
		status = fmt.Sprintf("Mode: %s | Tab: toggle focus | f: favorites | p: popular | t: trending | h: history | Enter: select | q: quit", m.mode)
	}
	b.WriteString("\n")
	b.WriteString(podcastStatusStyle.Render(status))

	return b.String()
}

// Command functions for async operations
func (m podcastModel) searchPodcasts(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.SearchPodcasts(query)
		if err != nil {
			return podcastSearchResultMsg{err: err}
		}
		return podcastSearchResultMsg{items: resp.Rows}
	}
}

func (m podcastModel) playEpisode(episode *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.PlayPodcastEpisode(episode)
		if err != nil {
			return podcastPlayResultMsg{err: err}
		}
		return podcastPlayResultMsg{episodeName: episode.Title}
	}
}

func (m podcastModel) loadEpisodes(podcastPath string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetPodcastEpisodesAll(podcastPath)
		if err != nil {
			return podcastSearchResultMsg{err: err}
		}
		// Filter for audio episodes
		var episodes []kefw2.ContentItem
		for _, row := range resp.Rows {
			if row.Type == "audio" {
				episodes = append(episodes, row)
			}
		}
		return podcastSearchResultMsg{items: episodes}
	}
}

func (m podcastModel) loadFavorites() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetPodcastFavorites()
		if err != nil {
			return podcastSearchResultMsg{err: err}
		}
		return podcastSearchResultMsg{items: resp.Rows}
	}
}

func (m podcastModel) loadPopular() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetPodcastPopular()
		if err != nil {
			return podcastSearchResultMsg{err: err}
		}
		return podcastSearchResultMsg{items: resp.Rows}
	}
}

func (m podcastModel) loadTrending() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetPodcastTrending()
		if err != nil {
			return podcastSearchResultMsg{err: err}
		}
		return podcastSearchResultMsg{items: resp.Rows}
	}
}

func (m podcastModel) loadHistory() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetPodcastHistory()
		if err != nil {
			return podcastSearchResultMsg{err: err}
		}
		return podcastSearchResultMsg{items: resp.Rows}
	}
}

// podcastCmd represents the podcast command
var podcastCmd = &cobra.Command{
	Use:     "podcast",
	Aliases: []string{"pod"},
	Short:   "Browse and play podcasts",
	Long: `Browse and play podcasts on your KEF speaker.

Interactive mode: Opens a TUI for searching and browsing podcasts.
Use Tab to switch between search and results, Enter to select a podcast
or play an episode.

Keyboard shortcuts:
  Tab       - Toggle between search input and podcast list
  Enter     - Search (when in input), select podcast, or play episode
  Esc       - Go back from episodes view / toggle focus
  Backspace - Go back from episodes view
  f         - Load favorites
  p         - Load popular podcasts
  t         - Load trending podcasts
  h         - Load podcast history
  q         - Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		model := initialPodcastModel(client)

		p := tea.NewProgram(model, tea.WithAltScreen())
		_, err := p.Run()
		exitOnError(err, "Error running podcast browser")
	},
}

// podcastSearchCmd handles direct search from command line
var podcastSearchCmd = &cobra.Command{
	Use:               "search [query]",
	Short:             "Search for podcasts",
	Long:              `Search for podcasts by name or keyword. Shows an interactive picker to select and browse.`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: DynamicPodcastSearchCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		saveFav, _ := cmd.Flags().GetBool("save-favorite")
		addToQueue, _ := cmd.Flags().GetBool("queue")
		query := strings.Join(args, " ")
		client := kefw2.NewAirableClient(currentSpeaker)

		headerPrinter.Printf("Searching for: %s\n", query)

		resp, err := client.SearchPodcasts(query)
		exitOnError(err, "Search failed")

		podcasts := filterPodcastContainers(resp.Rows)

		if len(podcasts) == 0 {
			contentPrinter.Println("No podcasts found.")
			return
		}

		// Show interactive picker using unified content picker
		action := ActionPlay
		title := fmt.Sprintf("Search results for '%s'", query)
		if saveFav {
			action = ActionSaveFavorite
			title = title + " (save mode)"
		} else if addToQueue {
			action = ActionAddToQueue
			title = title + " (queue mode)"
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServicePodcast,
			Items:       podcasts,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultPodcastCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// podcastFavoritesCmd lists favorite podcasts
var podcastFavoritesCmd = &cobra.Command{
	Use:               "favorites [show[/episode]]",
	Aliases:           []string{"fav"},
	Short:             "Browse and play favorite podcasts",
	ValidArgsFunction: PodcastFavoritesCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		removeFav, _ := cmd.Flags().GetBool("remove")
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetPodcastFavoritesAll()
		exitOnError(err, "Failed to get favorites")

		podcasts := filterPodcastContainers(resp.Rows)

		if len(podcasts) == 0 {
			contentPrinter.Println("No favorites found.")
			return
		}

		// If a podcast name was provided, find and play/remove it directly
		if len(args) > 0 {
			showName, episodeName, hasEpisode := parsePodcastPath(strings.Join(args, " "))
			if podcast, found := findItemByName(podcasts, showName); found {
				if removeFav {
					headerPrinter.Printf("Removing: %s\n", podcast.Title)
					err := client.RemovePodcastFavorite(podcast)
					exitOnError(err, "Failed to remove favorite")
					taskConpletedPrinter.Printf("Removed from favorites: %s\n", podcast.Title)
				} else if hasEpisode {
					// Play specific episode
					episodes, err := client.GetPodcastEpisodesAll(podcast.Path)
					exitOnError(err, "Failed to get episodes")
					if episode, found := findEpisodeByName(episodes.Rows, episodeName); found {
						if addToQueue {
							headerPrinter.Printf("Adding to queue: %s\n", episode.Title)
							err := client.AddToQueue([]kefw2.ContentItem{*episode}, false)
							exitOnError(err, "Failed to add to queue")
							taskConpletedPrinter.Printf("Added to queue: %s\n", episode.Title)
						} else {
							headerPrinter.Printf("Playing: %s\n", episode.Title)
							err := client.PlayPodcastEpisode(episode)
							exitOnError(err, "Failed to play")
							taskConpletedPrinter.Printf("Now playing: %s\n", episode.Title)
						}
					} else {
						exitWithError("Episode '%s' not found in '%s'.", episodeName, podcast.Title)
					}
				} else {
					// Play latest episode
					if addToQueue {
						headerPrinter.Printf("Adding latest episode of '%s' to queue\n", podcast.Title)
						episode, err := client.GetLatestEpisode(podcast)
						exitOnError(err, "Failed to get latest episode")
						err = client.AddToQueue([]kefw2.ContentItem{*episode}, false)
						exitOnError(err, "Failed to add to queue")
						taskConpletedPrinter.Printf("Added to queue: %s\n", episode.Title)
					} else {
						headerPrinter.Printf("Playing latest episode of: %s\n", podcast.Title)
						err := playPodcastLatestEpisode(client, podcast)
						exitOnError(err, "Failed to play")
						taskConpletedPrinter.Printf("Now playing latest episode of: %s\n", podcast.Title)
					}
				}
				return
			}
			exitWithError("Podcast '%s' not found in favorites.", showName)
		}

		// Show interactive picker using unified content picker
		action := ActionPlay
		title := "Favorite Podcasts"
		if removeFav {
			action = ActionRemoveFavorite
			title = title + " (remove mode)"
		} else if addToQueue {
			action = ActionAddToQueue
			title = title + " (queue mode)"
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServicePodcast,
			Items:       podcasts,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultPodcastCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// podcastPopularCmd lists popular podcasts
var podcastPopularCmd = MakePodcastCategoryCommand(PodcastCategoryConfig{
	Use:               "popular [show[/episode]]",
	Short:             "Browse and play popular podcasts",
	ValidArgsFunction: PodcastPopularCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetPodcastPopularAll() },
	EmptyMessage:      "No popular podcasts found.",
	NotFoundMessage:   "popular podcasts",
	Title:             "Popular Podcasts",
	Callbacks:         DefaultPodcastCallbacks,
})

// podcastTrendingCmd lists trending podcasts
var podcastTrendingCmd = MakePodcastCategoryCommand(PodcastCategoryConfig{
	Use:               "trending [show[/episode]]",
	Short:             "Browse and play trending podcasts",
	ValidArgsFunction: PodcastTrendingCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetPodcastTrendingAll() },
	EmptyMessage:      "No trending podcasts found.",
	NotFoundMessage:   "trending podcasts",
	Title:             "Trending Podcasts",
	Callbacks:         DefaultPodcastCallbacks,
})

// podcastHistoryCmd lists recently played podcasts
var podcastHistoryCmd = MakePodcastCategoryCommand(PodcastCategoryConfig{
	Use:               "history [show[/episode]]",
	Short:             "Browse and play recently played podcasts",
	ValidArgsFunction: PodcastHistoryCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetPodcastHistoryAll() },
	EmptyMessage:      "No podcasts in history.",
	NotFoundMessage:   "history",
	Title:             "Podcast History",
	Callbacks:         DefaultPodcastCallbacks,
})

// podcastPlayCmd plays a podcast by searching and playing the first episode
var podcastPlayCmd = &cobra.Command{
	Use:   "play [podcast name]",
	Short: "Search for a podcast and play its latest episode",
	Long: `Search for a podcast and play its latest episode.

Flags:
  -q, --queue    Add to queue instead of playing immediately
                 (starts playback only if queue was empty)

Examples:
  kefw2 podcast play "Serial"
  kefw2 podcast play -q "Serial"    # Add to queue`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: PodcastPlayCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		query := strings.Join(args, " ")
		client := kefw2.NewAirableClient(currentSpeaker)
		addToQueue, _ := cmd.Flags().GetBool("queue")

		headerPrinter.Printf("Searching for: %s\n", query)

		resp, err := client.SearchPodcasts(query)
		exitOnError(err, "Search failed")

		// Filter for podcast containers
		var podcasts []kefw2.ContentItem
		for _, p := range resp.Rows {
			if p.Type == "container" {
				podcasts = append(podcasts, p)
			}
		}

		if len(podcasts) == 0 {
			exitWithError("No podcasts found.")
		}

		// If only one podcast, get its episodes and play the first
		if len(podcasts) == 1 {
			podcast := podcasts[0]
			headerPrinter.Printf("Found podcast: %s\n", podcast.Title)

			episodes, err := client.GetPodcastEpisodesAll(podcast.Path)
			exitOnError(err, "Failed to get episodes")

			for _, ep := range episodes.Rows {
				if ep.Type == "audio" {
					if addToQueue {
						headerPrinter.Printf("Adding to queue: %s\n", ep.Title)
						err := client.AddToQueue([]kefw2.ContentItem{ep}, true)
						exitOnError(err, "Failed to add to queue")
						taskConpletedPrinter.Printf("Added to queue: %s\n", ep.Title)
					} else {
						headerPrinter.Printf("Playing: %s\n", ep.Title)
						err := client.PlayPodcastEpisode(&ep)
						exitOnError(err, "Failed to play")
						taskConpletedPrinter.Printf("Now playing: %s\n", ep.Title)
					}
					return
				}
			}
			exitWithError("No episodes found for this podcast.")
		}

		// Multiple podcasts - show interactive picker
		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
		}
		title := fmt.Sprintf("Search results for '%s'", query)
		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServicePodcast,
			Items:       podcasts,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultPodcastCallbacks(client),
			SearchQuery: query,
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// filterPodcastContainers filters items to only include podcast containers
func filterPodcastContainers(rows []kefw2.ContentItem) []kefw2.ContentItem {
	var podcasts []kefw2.ContentItem
	for _, row := range rows {
		if row.Type == "container" {
			podcasts = append(podcasts, row)
		}
	}
	return podcasts
}

// parsePodcastPath parses a "show/episode" path into show name and optional episode name.
// Returns (showName, episodeName, hasEpisode)
func parsePodcastPath(path string) (string, string, bool) {
	// Find the first "/" that isn't escaped (%2F)
	// We need to handle the case where show names might contain "/"
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 1 {
		return path, "", false
	}
	return parts[0], parts[1], true
}

// playPodcastLatestEpisode plays the latest episode of a podcast
func playPodcastLatestEpisode(client *kefw2.AirableClient, podcast *kefw2.ContentItem) error {
	episode, err := client.GetLatestEpisode(podcast)
	if err != nil {
		return err
	}
	return client.PlayPodcastEpisode(episode)
}

// Flags for podcast commands
var podcastSaveFavoriteFlag bool

// podcastFilterCmd browses podcasts by genre/language
var podcastFilterCmd = &cobra.Command{
	Use:   "filter [path]",
	Short: "Browse podcasts by genre or language",
	Long: `Browse podcasts by genre or language with tab completion.

Use TAB to navigate through categories:
  kefw2 podcast filter <TAB>              - Show top-level filters (genres, languages)
  kefw2 podcast filter "Genres/"<TAB>     - Show available genres
  kefw2 podcast filter "Genres/Comedy"    - Show Comedy podcasts in picker`,
	ValidArgsFunction: PodcastFilterCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		saveFav, _ := cmd.Flags().GetBool("save-favorite")
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		// Get the filter menu first
		filterResp, err := client.GetPodcastFilter()
		exitOnError(err, "Failed to get filter menu")

		// Determine action based on flags
		action := ActionPlay
		actionSuffix := ""
		if saveFav {
			action = ActionSaveFavorite
			actionSuffix = " (save mode)"
		} else if addToQueue {
			action = ActionAddToQueue
			actionSuffix = " (queue mode)"
		}

		// If no path provided, show the filter menu
		if len(args) == 0 {
			result, err := RunContentPicker(ContentPickerConfig{
				ServiceType: ServicePodcast,
				Items:       filterResp.Rows,
				Title:       "Podcast Filters" + actionSuffix,
				CurrentPath: "",
				Action:      action,
				Callbacks:   DefaultPodcastCallbacks(client),
			})
			exitOnError(err, "Error")
			if result.Queued && result.Selected != nil {
				taskConpletedPrinter.Printf("Added to queue: %s\n", result.Selected.Title)
			} else if result.Played && result.Selected != nil {
				taskConpletedPrinter.Printf("Now playing: %s\n", result.Selected.Title)
			}
			return
		}

		// Browse to the specified path
		browsePath := args[0]
		resp, err := client.BrowsePodcastByDisplayPath(browsePath)
		exitOnError(err, fmt.Sprintf("Failed to browse '%s'", browsePath))

		if len(resp.Rows) == 0 {
			contentPrinter.Printf("No items found at '%s'.\n", browsePath)
			return
		}

		title := fmt.Sprintf("Filter: %s", browsePath) + actionSuffix

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServicePodcast,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: browsePath,
			Action:      action,
			Callbacks:   DefaultPodcastCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// podcastBrowseCmd browses podcast categories
var podcastBrowseCmd = &cobra.Command{
	Use:   "browse [category]",
	Short: "Browse podcast categories with interactive picker",
	Long: `Browse podcast categories and shows with an interactive picker.

Categories available:
  - popular    : Popular podcasts
  - trending   : Trending podcasts
  - history    : Recently played podcasts
  - favorites  : Your favorite podcasts

When you select a podcast, you can navigate into it to see episodes.
Use the fuzzy filter to quickly find what you're looking for.`,
	ValidArgsFunction: PodcastBrowseCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		category := "popular"
		if len(args) > 0 {
			category = strings.ToLower(args[0])
		}

		var resp *kefw2.RowsResponse
		var err error
		var title string

		switch category {
		case "popular":
			resp, err = client.GetPodcastPopular()
			title = "Popular Podcasts"
		case "trending":
			resp, err = client.GetPodcastTrending()
			title = "Trending Podcasts"
		case "history":
			resp, err = client.GetPodcastHistory()
			title = "Podcast History"
		case "favorites", "fav":
			resp, err = client.GetPodcastFavorites()
			title = "Favorite Podcasts"
		default:
			exitWithError("Unknown category: %s. Available categories: popular, trending, history, favorites", category)
		}

		exitOnError(err, fmt.Sprintf("Failed to load %s", category))

		if len(resp.Rows) == 0 {
			contentPrinter.Printf("No podcasts found in %s.\n", category)
			return
		}

		action := ActionPlay
		if podcastSaveFavoriteFlag {
			action = ActionSaveFavorite
			title = title + " (save mode)"
		} else if addToQueue {
			action = ActionAddToQueue
			title = title + " (queue mode)"
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServicePodcast,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultPodcastCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

func init() {
	rootCmd.AddCommand(podcastCmd)
	podcastCmd.AddCommand(podcastSearchCmd)
	podcastCmd.AddCommand(podcastFavoritesCmd)
	podcastCmd.AddCommand(podcastPopularCmd)
	podcastCmd.AddCommand(podcastTrendingCmd)
	podcastCmd.AddCommand(podcastHistoryCmd)
	podcastCmd.AddCommand(podcastPlayCmd)
	podcastCmd.AddCommand(podcastFilterCmd)
	podcastCmd.AddCommand(podcastBrowseCmd)

	// Flags for podcastBrowseCmd
	podcastBrowseCmd.Flags().BoolVarP(&podcastSaveFavoriteFlag, "save-favorite", "f", false, "Save selected podcast as favorite instead of playing")

	// Note: podcastPopularCmd, podcastTrendingCmd, podcastHistoryCmd get their
	// --save-favorite and --queue flags automatically from MakePodcastCategoryCommand

	// Flags for other podcast commands - save favorite
	podcastSearchCmd.Flags().BoolP("save-favorite", "f", false, "Save selected podcast as favorite instead of playing")
	podcastFilterCmd.Flags().BoolP("save-favorite", "f", false, "Save selected podcast as favorite instead of playing")

	// Flags for podcastFavoritesCmd - remove favorite
	podcastFavoritesCmd.Flags().BoolP("remove", "r", false, "Remove selected podcast from favorites instead of playing")

	// Add -q flag for queue mode
	podcastPlayCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	podcastSearchCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	podcastFavoritesCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	podcastFilterCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	podcastBrowseCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
}
