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

// ServiceDeezer is the service type constant for Deezer in the cmd package.
const ServiceDeezerType ServiceType = "deezer"

// Styles for the Deezer TUI.
var (
	deezerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("135")).
				MarginBottom(1)

	deezerSearchStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("135")).
				Padding(0, 1)

	deezerStatusStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				MarginTop(1)

	deezerSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("135"))

	deezerPlayingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("82"))
)

// deezerItem represents a Deezer content item in the list.
type deezerItem struct {
	item kefw2.ContentItem
}

func (i deezerItem) Title() string { return i.item.Title }
func (i deezerItem) Description() string {
	if i.item.MediaData != nil && i.item.MediaData.MetaData.Artist != "" {
		desc := i.item.MediaData.MetaData.Artist
		if i.item.MediaData.MetaData.Album != "" {
			desc += " — " + i.item.MediaData.MetaData.Album
		}
		return desc
	}
	if i.item.LongDescription != "" {
		return i.item.LongDescription
	}
	if i.item.Type == TypeContainer {
		return "📁"
	}
	return ""
}
func (i deezerItem) FilterValue() string { return i.item.Title }

// deezerModel is the Bubbletea model for the Deezer browser.
type deezerModel struct {
	client      *kefw2.AirableClient
	searchInput textinput.Model
	list        list.Model
	items       []kefw2.ContentItem
	loading     bool
	playing     string
	err         error
	width       int
	height      int
	mode        string
	quitting    bool
}

// Messages for async operations.
type deezerSearchResultMsg struct {
	items []kefw2.ContentItem
	err   error
}

type deezerPlayResultMsg struct {
	trackName string
	err       error
}

func initialDeezerModel(client *kefw2.AirableClient) deezerModel {
	ti := textinput.New()
	ti.Placeholder = "Search Deezer..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = deezerSelectedStyle
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Deezer"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)

	return deezerModel{
		client:      client,
		searchInput: ti,
		list:        l,
		mode:        "search",
	}
}

func (m deezerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m deezerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case KeyCtrlC, "q":
			if !m.searchInput.Focused() || msg.String() == KeyCtrlC {
				m.quitting = true
				return m, tea.Quit
			}
		case KeyEnter:
			if m.searchInput.Focused() && m.searchInput.Value() != "" {
				m.loading = true
				query := m.searchInput.Value()
				return m, m.searchDeezer(query)
			} else if !m.searchInput.Focused() {
				if item, ok := m.list.SelectedItem().(deezerItem); ok {
					switch {
					case item.item.Type == TypeAudio:
						return m, m.playTrack(&item.item)
					case item.item.ContainerPlayable:
						return m, m.playTrack(&item.item)
					case item.item.Type == TypeContainer:
						m.loading = true
						return m, m.navigateInto(&item.item)
					}
				}
			}
		case "tab":
			if m.searchInput.Focused() {
				m.searchInput.Blur()
			} else {
				m.searchInput.Focus()
			}
		case KeyEsc:
			if m.searchInput.Focused() {
				m.searchInput.Blur()
			} else {
				m.searchInput.Focus()
			}
		case "c":
			if !m.searchInput.Focused() {
				m.mode = "charts"
				m.loading = true
				return m, m.loadCharts()
			}
		case "m":
			if !m.searchInput.Focused() {
				m.mode = "mood"
				m.loading = true
				return m, m.loadMoods()
			}
		case "g":
			if !m.searchInput.Focused() {
				m.mode = "genres"
				m.loading = true
				return m, m.loadGenres()
			}
		case "l":
			if !m.searchInput.Focused() {
				m.mode = "library"
				m.loading = true
				return m, m.loadLibrary()
			}
		case "x":
			if !m.searchInput.Focused() {
				m.mode = "mixes"
				m.loading = true
				return m, m.loadMixes()
			}
		case "r":
			if !m.searchInput.Focused() {
				m.mode = "recommendations"
				m.loading = true
				return m, m.loadRecommendations()
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

	case deezerSearchResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.items = msg.items
			m.err = nil
			items := make([]list.Item, len(msg.items))
			for i, s := range msg.items {
				items[i] = deezerItem{item: s}
			}
			m.list.SetItems(items)
			m.searchInput.Blur()
		}

	case deezerPlayResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.playing = msg.trackName
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

func (m deezerModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	title := deezerTitleStyle.Render("Deezer")
	b.WriteString(title)
	b.WriteString("\n")

	searchBox := deezerSearchStyle.Render(m.searchInput.View())
	b.WriteString(searchBox)
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString("Loading...")
		b.WriteString("\n")
	} else {
		b.WriteString(m.list.View())
	}

	var status string
	switch {
	case m.err != nil:
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("Error: %v", m.err))
	case m.playing != "":
		status = deezerPlayingStyle.Render(fmt.Sprintf("Now playing: %s", m.playing))
	default:
		status = fmt.Sprintf("Mode: %s | Tab: focus | c: charts | m: mood | g: genres | l: library | x: mixes | r: recs | q: quit", m.mode)
	}
	b.WriteString("\n")
	b.WriteString(deezerStatusStyle.Render(status))

	return b.String()
}

// Command functions for async operations.
func (m deezerModel) searchDeezer(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.SearchDeezer(query)
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

func (m deezerModel) playTrack(track *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.ResolveAndPlayDeezerItem(track)
		if err != nil {
			return deezerPlayResultMsg{err: err}
		}
		return deezerPlayResultMsg{trackName: track.Title}
	}
}

func (m deezerModel) navigateInto(item *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRows(item.Path, 0, 50)
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

func (m deezerModel) loadCharts() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetDeezerCharts()
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

func (m deezerModel) loadMoods() tea.Cmd {
	return func() tea.Msg {
		moods, err := m.client.GetDeezerMoods()
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: moods}
	}
}

func (m deezerModel) loadGenres() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetDeezerGenres()
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

func (m deezerModel) loadLibrary() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetDeezerLibrary()
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

func (m deezerModel) loadMixes() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetDeezerMixes()
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

func (m deezerModel) loadRecommendations() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetDeezerRecommendations()
		if err != nil {
			return deezerSearchResultMsg{err: err}
		}
		return deezerSearchResultMsg{items: resp.Rows}
	}
}

// DefaultDeezerCallbacks returns the content picker callbacks for Deezer.
func DefaultDeezerCallbacks(client *kefw2.AirableClient) ContentPickerCallbacks {
	return ContentPickerCallbacks{
		Navigate: func(item *kefw2.ContentItem, currentPath string) ([]kefw2.ContentItem, string, error) {
			resp, err := client.GetRows(item.Path, 0, 50)
			if err != nil {
				return nil, "", err
			}
			newPath := item.Title
			if currentPath != "" {
				newPath = currentPath + "/" + item.Title
			}
			return resp.Rows, newPath, nil
		},
		Play: func(item *kefw2.ContentItem) error {
			return client.ResolveAndPlayDeezerItem(item)
		},
		SaveFavorite: func(item *kefw2.ContentItem) error {
			return client.AddDeezerFavorite(item)
		},
		RemoveFavorite: func(item *kefw2.ContentItem) error {
			return client.RemoveDeezerFavorite(item)
		},
		AddToQueue: func(item *kefw2.ContentItem) error {
			// For containers (albums, playlists), fetch tracks and add them all
			if item.Type == "container" {
				resp, err := client.GetRows(item.Path, 0, 100)
				if err != nil {
					return fmt.Errorf("failed to browse container: %w", err)
				}
				var tracks []kefw2.ContentItem
				for i := range resp.Rows {
					if resp.Rows[i].Type == TypeAudio {
						tracks = append(tracks, resp.Rows[i])
					}
				}
				if len(tracks) == 0 {
					return fmt.Errorf("no tracks found in: %s", item.Title)
				}
				return client.AddToQueue(tracks, false)
			}
			return client.AddToQueue([]kefw2.ContentItem{*item}, false)
		},
		IsPlayable: func(item *kefw2.ContentItem) bool {
			return item.Type == TypeAudio || item.ContainerPlayable
		},
	}
}

// deezerCmd represents the deezer command.
var deezerCmd = &cobra.Command{
	Use:     "deezer",
	Aliases: []string{"dz"},
	Short:   "Browse and play Deezer music",
	Long: `Browse and play Deezer music on your KEF speaker.

Interactive mode: Opens a TUI for searching and browsing Deezer.
Use Tab to switch between search and results, Enter to play tracks
or navigate into containers (albums, artists, playlists).

Keyboard shortcuts:
  Tab    - Toggle between search input and content list
  Enter  - Search (when in input), play track, or browse container
  c      - Load charts
  m      - Load mood streams (Flow, etc.)
  g      - Load genres
  l      - Load library
  x      - Load mixes
  r      - Load recommendations
  q      - Quit

Requires Deezer account linked via the KEF app.`,
	Run: func(_ *cobra.Command, _ []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		model := initialDeezerModel(client)

		p := tea.NewProgram(model, tea.WithAltScreen())
		_, err := p.Run()
		exitOnError(err, "Error running Deezer browser")
	},
}

// deezerSearchCmd handles direct search from command line.
var deezerSearchCmd = &cobra.Command{
	Use:               "search [query]",
	Short:             "Search Deezer for artists, tracks, or albums",
	Long:              `Search Deezer by name or keyword. Shows an interactive picker to select and play.`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: DynamicDeezerSearchCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		saveFav, _ := cmd.Flags().GetBool("save-favorite")
		addToQueue, _ := cmd.Flags().GetBool("queue")
		query := strings.Join(args, " ")
		client := kefw2.NewAirableClient(currentSpeaker)

		headerPrinter.Printf("Searching for: %s\n", query)

		resp, err := client.SearchDeezer(query)
		exitOnError(err, "Search failed")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No results found.")
			return
		}

		action := ActionPlay
		title := fmt.Sprintf("Search results for '%s'", query)
		if saveFav {
			action = ActionSaveFavorite
			title += SuffixSaveMode
		} else if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
			SearchQuery: query,
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// deezerChartsCmd browses Deezer charts.
var deezerChartsCmd = &cobra.Command{
	Use:   "charts [tracks|albums]",
	Short: "Browse Deezer charts",
	Long: `Browse Deezer charts. Optionally specify a subcategory:
  tracks  - Show chart tracks
  albums  - Show chart albums

Without a subcategory, shows the charts menu.`,
	ValidArgsFunction: DeezerChartsCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		var resp *kefw2.RowsResponse
		var err error
		title := "Deezer Charts"

		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "tracks":
				resp, err = client.GetDeezerChartsTracks()
				title = "Deezer Charts — Tracks"
			case "albums":
				resp, err = client.GetDeezerChartsAlbums()
				title = "Deezer Charts — Albums"
			default:
				exitWithError("Unknown chart type: %s. Available: tracks, albums", args[0])
			}
		} else {
			resp, err = client.GetDeezerCharts()
		}
		exitOnError(err, "Failed to load charts")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No chart items found.")
			return
		}

		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// deezerMoodCmd lists and plays Deezer mood streams.
var deezerMoodCmd = &cobra.Command{
	Use:   "mood [name]",
	Short: "Play Deezer mood streams (Flow, Happy, Workout, etc.)",
	Long: `Play Deezer mood streams. Available moods include:
  Flow, Happy, Workout, Party, Chill, Sad, Love, Focus

Without a name, shows an interactive picker of available moods.
With a name, plays that mood directly.`,
	ValidArgsFunction: DeezerMoodCompletion,
	Run: func(_ *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		if len(args) > 0 {
			moodName := strings.Join(args, " ")
			headerPrinter.Printf("Playing mood: %s\n", moodName)
			err := client.PlayDeezerMood(moodName)
			exitOnError(err, "Failed to play mood")
			taskConpletedPrinter.Printf("Now playing: %s\n", moodName)
			return
		}

		// No arg — show picker
		moods, err := client.GetDeezerMoods()
		exitOnError(err, "Failed to get moods")

		if len(moods) == 0 {
			contentPrinter.Println("No moods available.")
			return
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       moods,
			Title:       "Deezer Moods",
			CurrentPath: "",
			Action:      ActionPlay,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, ActionPlay) {
			exitWithError("Operation failed")
		}
	},
}

// deezerGenresCmd browses Deezer genres.
var deezerGenresCmd = &cobra.Command{
	Use:   "genres [genre]",
	Short: "Browse Deezer by genre",
	Long: `Browse Deezer by genre. Without arguments, shows available genres.
With a genre name, browses into that genre.`,
	ValidArgsFunction: DeezerGenresCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		var resp *kefw2.RowsResponse
		var err error
		title := "Deezer Genres"

		if len(args) > 0 {
			browsePath := strings.Join(args, " ")
			resp, err = client.BrowseDeezerByDisplayPath("Genres/" + browsePath)
			title = fmt.Sprintf("Deezer — %s", browsePath)
		} else {
			resp, err = client.GetDeezerGenres()
		}
		exitOnError(err, "Failed to load genres")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No genres found.")
			return
		}

		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// deezerLibraryCmd browses the user's Deezer library.
var deezerLibraryCmd = &cobra.Command{
	Use:   "library [tracks|albums|playlists|history]",
	Short: "Browse your Deezer library",
	Long: `Browse your Deezer library. Optionally specify a section:
  tracks     - Your saved tracks
  albums     - Your saved albums
  playlists  - Your playlists
  history    - Your listening history

Without a section, shows the library menu.`,
	ValidArgsFunction: DeezerLibraryCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		var resp *kefw2.RowsResponse
		var err error
		title := "Deezer Library"

		if len(args) > 0 {
			switch strings.ToLower(args[0]) {
			case "tracks":
				resp, err = client.GetDeezerLibraryTracks()
				title = "Deezer Library — Tracks"
			case "albums":
				resp, err = client.GetDeezerLibraryAlbums()
				title = "Deezer Library — Albums"
			case "playlists":
				resp, err = client.GetDeezerLibraryPlaylists()
				title = "Deezer Library — Playlists"
			case "history":
				resp, err = client.GetDeezerLibraryHistory()
				title = "Deezer Library — History"
			default:
				exitWithError("Unknown library section: %s. Available: tracks, albums, playlists, history", args[0])
			}
		} else {
			resp, err = client.GetDeezerLibrary()
		}
		exitOnError(err, "Failed to load library")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No items found.")
			return
		}

		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// deezerMixesCmd browses Deezer mixes/programs.
var deezerMixesCmd = &cobra.Command{
	Use:   "mixes",
	Short: "Browse Deezer mixes",
	Run: func(cmd *cobra.Command, _ []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetDeezerMixes()
		exitOnError(err, "Failed to load mixes")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No mixes found.")
			return
		}

		action := ActionPlay
		title := "Deezer Mixes"
		if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// deezerRecommendationsCmd browses Deezer recommendations.
var deezerRecommendationsCmd = &cobra.Command{
	Use:     "recommendations",
	Aliases: []string{"recs", "rec"},
	Short:   "Browse Deezer recommendations",
	Run: func(cmd *cobra.Command, _ []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetDeezerRecommendations()
		exitOnError(err, "Failed to load recommendations")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No recommendations found.")
			return
		}

		action := ActionPlay
		title := "Deezer Recommendations"
		if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

// deezerBrowseCmd browses Deezer with hierarchical path completion.
var deezerBrowseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Browse Deezer with hierarchical navigation",
	Long: `Browse Deezer with hierarchical tab completion.

Use TAB to navigate through categories:
  kefw2 deezer browse <TAB>                  - Show top-level menu
  kefw2 deezer browse "Charts/"<TAB>         - Show chart categories
  kefw2 deezer browse "Genres/Rock"          - Show Rock genre content

Path escaping:
  Names containing "/" are escaped as "%2F"`,
	ValidArgsFunction: HierarchicalDeezerCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		addToQueue, _ := cmd.Flags().GetBool("queue")
		client := kefw2.NewAirableClient(currentSpeaker)

		browsePath := ""
		if len(args) > 0 {
			browsePath = args[0]
		}

		resp, err := client.BrowseDeezerByDisplayPath(browsePath)
		exitOnError(err, "Browse failed")

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No items found at this path.")
			return
		}

		title := "Deezer"
		if browsePath != "" {
			title = fmt.Sprintf("Browse: %s (%d items)", browsePath, len(resp.Rows))
		}

		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title += SuffixQueueMode
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceDeezerType,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: browsePath,
			Action:      action,
			Callbacks:   DefaultDeezerCallbacks(client),
		})
		exitOnError(err, "Error")

		if HandlePickerResult(result, action) {
			exitWithError("Operation failed")
		}
	},
}

func init() {
	rootCmd.AddCommand(deezerCmd)
	deezerCmd.AddCommand(deezerSearchCmd)
	deezerCmd.AddCommand(deezerChartsCmd)
	deezerCmd.AddCommand(deezerMoodCmd)
	deezerCmd.AddCommand(deezerGenresCmd)
	deezerCmd.AddCommand(deezerLibraryCmd)
	deezerCmd.AddCommand(deezerMixesCmd)
	deezerCmd.AddCommand(deezerRecommendationsCmd)
	deezerCmd.AddCommand(deezerBrowseCmd)

	// Add queue flag to subcommands
	deezerSearchCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	deezerSearchCmd.Flags().BoolP("save-favorite", "f", false, "Save to Deezer favorites instead of playing")
	deezerChartsCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	deezerGenresCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	deezerLibraryCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	deezerMixesCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	deezerRecommendationsCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
	deezerBrowseCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
}
