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
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

// Styles for the radio TUI
var (
	radioTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			MarginBottom(1)

	radioSearchStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("39")).
				Padding(0, 1)

	radioStatusStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241")).
				MarginTop(1)

	radioSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("170"))

	radioPlayingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("82"))
)

// Flags for radio commands
var saveFavoriteFlag bool

// radioItem represents a radio station in the list
type radioItem struct {
	station kefw2.ContentItem
}

func (i radioItem) Title() string       { return i.station.Title }
func (i radioItem) Description() string { return i.station.LongDescription }
func (i radioItem) FilterValue() string { return i.station.Title }

// radioModel is the Bubbletea model for the radio browser
type radioModel struct {
	client      *kefw2.AirableClient
	searchInput textinput.Model
	list        list.Model
	stations    []kefw2.ContentItem
	searching   bool
	loading     bool
	playing     string // Currently playing station name
	err         error
	width       int
	height      int
	mode        string // "search", "favorites", "popular", "local", etc.
	quitting    bool
}

// Messages for async operations
type searchResultMsg struct {
	stations []kefw2.ContentItem
	err      error
}

type playResultMsg struct {
	stationName string
	err         error
}

func initialRadioModel(client *kefw2.AirableClient) radioModel {
	ti := textinput.New()
	ti.Placeholder = "Search radio stations..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	// Create delegate for list items
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = radioSelectedStyle
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Radio Stations"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(true)

	return radioModel{
		client:      client,
		searchInput: ti,
		list:        l,
		mode:        "search",
	}
}

func (m radioModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m radioModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				m.searching = true
				query := m.searchInput.Value()
				return m, tea.Batch(
					m.searchRadio(query),
				)
			} else if !m.searchInput.Focused() {
				// Play selected station
				if item, ok := m.list.SelectedItem().(radioItem); ok {
					return m, m.playStation(&item.station)
				}
			}
		case "tab":
			// Toggle between search input and list
			if m.searchInput.Focused() {
				m.searchInput.Blur()
			} else {
				m.searchInput.Focus()
			}
		case "esc":
			if m.searchInput.Focused() {
				m.searchInput.Blur()
			} else {
				m.searchInput.Focus()
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
		case "l":
			if !m.searchInput.Focused() {
				m.mode = "local"
				m.loading = true
				return m, m.loadLocal()
			}
		case "t":
			if !m.searchInput.Focused() {
				m.mode = "trending"
				m.loading = true
				return m, m.loadTrending()
			}
		case "h":
			if !m.searchInput.Focused() {
				m.mode = "hq"
				m.loading = true
				return m, m.loadHQ()
			}
		case "n":
			if !m.searchInput.Focused() {
				m.mode = "new"
				m.loading = true
				return m, m.loadNew()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Update list size
		headerHeight := 6 // Title + search box + margins
		footerHeight := 3 // Status + help
		listHeight := m.height - headerHeight - footerHeight
		if listHeight < 5 {
			listHeight = 5
		}
		m.list.SetSize(m.width-4, listHeight)

	case searchResultMsg:
		m.loading = false
		m.searching = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.stations = msg.stations
			m.err = nil
			items := make([]list.Item, len(msg.stations))
			for i, s := range msg.stations {
				items[i] = radioItem{station: s}
			}
			m.list.SetItems(items)
			// Focus on list after search
			m.searchInput.Blur()
		}

	case playResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.playing = msg.stationName
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

func (m radioModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title
	title := radioTitleStyle.Render("Internet Radio")
	b.WriteString(title)
	b.WriteString("\n")

	// Search input
	searchBox := radioSearchStyle.Render(m.searchInput.View())
	b.WriteString(searchBox)
	b.WriteString("\n\n")

	// Loading indicator
	if m.loading {
		b.WriteString("Loading...")
		b.WriteString("\n")
	} else {
		// List
		b.WriteString(m.list.View())
	}

	// Status bar
	var status string
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("Error: %v", m.err))
	} else if m.playing != "" {
		status = radioPlayingStyle.Render(fmt.Sprintf("Now playing: %s", m.playing))
	} else {
		status = fmt.Sprintf("Mode: %s | Tab: focus | f: fav | p: popular | l: local | t: trend | h: hq | n: new | q: quit", m.mode)
	}
	b.WriteString("\n")
	b.WriteString(radioStatusStyle.Render(status))

	return b.String()
}

// Command functions for async operations
func (m radioModel) searchRadio(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.SearchRadio(query)
		if err != nil {
			return searchResultMsg{err: err}
		}

		// Filter for playable radio stations
		// Radio stations come as type "container" with containerPlayable=true and audioType="audioBroadcast"
		var stations []kefw2.ContentItem
		for _, row := range resp.Rows {
			if row.ContainerPlayable && row.AudioType == "audioBroadcast" {
				stations = append(stations, row)
			} else if row.Type == "audio" {
				// Also include direct audio items
				stations = append(stations, row)
			}
		}
		return searchResultMsg{stations: stations}
	}
}

func (m radioModel) playStation(station *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.PlayRadioStation(station)
		if err != nil {
			return playResultMsg{err: err}
		}
		return playResultMsg{stationName: station.Title}
	}
}

func (m radioModel) loadFavorites() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRadioFavorites()
		if err != nil {
			return searchResultMsg{err: err}
		}
		return searchResultMsg{stations: resp.Rows}
	}
}

func (m radioModel) loadPopular() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRadioPopular()
		if err != nil {
			return searchResultMsg{err: err}
		}
		return searchResultMsg{stations: resp.Rows}
	}
}

func (m radioModel) loadLocal() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRadioLocal()
		if err != nil {
			return searchResultMsg{err: err}
		}
		return searchResultMsg{stations: resp.Rows}
	}
}

func (m radioModel) loadTrending() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRadioTrending()
		if err != nil {
			return searchResultMsg{err: err}
		}
		return searchResultMsg{stations: resp.Rows}
	}
}

func (m radioModel) loadHQ() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRadioHQ()
		if err != nil {
			return searchResultMsg{err: err}
		}
		return searchResultMsg{stations: resp.Rows}
	}
}

func (m radioModel) loadNew() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetRadioNew()
		if err != nil {
			return searchResultMsg{err: err}
		}
		return searchResultMsg{stations: resp.Rows}
	}
}

// filterPlayableStations filters stations to only include playable radio stations
func filterPlayableStations(rows []kefw2.ContentItem) []kefw2.ContentItem {
	var stations []kefw2.ContentItem
	for _, row := range rows {
		if row.ContainerPlayable && row.AudioType == "audioBroadcast" || row.Type == "audio" {
			stations = append(stations, row)
		}
	}
	return stations
}

// playRadioStationWithDetails plays a radio station, resolving it to playable form first.
// Stations from list endpoints (hq, trending, etc.) are containers that need to be
// browsed into to get the actual playable stream.
func playRadioStationWithDetails(client *kefw2.AirableClient, station *kefw2.ContentItem) error {
	return client.ResolveAndPlayRadioStation(station)
}

// radioCmd represents the radio command
var radioCmd = &cobra.Command{
	Use:     "radio",
	Aliases: []string{"r"},
	Short:   "Browse and play internet radio stations",
	Long: `Browse and play internet radio stations on your KEF speaker.

Interactive mode: Opens a TUI for searching and browsing radio stations.
Use Tab to switch between search and results, Enter to play.

Keyboard shortcuts:
  Tab    - Toggle between search input and station list
  Enter  - Search (when in input) or Play selected station
  f      - Load favorites
  p      - Load popular stations
  l      - Load local stations
  t      - Load trending stations
  h      - Load high quality stations
  n      - Load new stations
  q      - Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		model := initialRadioModel(client)

		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			errorPrinter.Printf("Error running radio browser: %v\n", err)
			os.Exit(1)
		}
	},
}

// radioSearchCmd handles direct search from command line
var radioSearchCmd = &cobra.Command{
	Use:               "search [query]",
	Short:             "Search for radio stations",
	Long:              `Search for radio stations by name or keyword. Shows an interactive picker to select and play.`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: DynamicRadioSearchCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		query := strings.Join(args, " ")
		client := kefw2.NewAirableClient(currentSpeaker)

		headerPrinter.Printf("Searching for: %s\n", query)

		resp, err := client.SearchRadio(query)
		if err != nil {
			errorPrinter.Printf("Search failed: %v\n", err)
			os.Exit(1)
		}

		stations := filterPlayableStations(resp.Rows)

		if len(stations) == 0 {
			contentPrinter.Println("No stations found.")
			return
		}

		// Show interactive picker using unified content picker
		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceRadio,
			Items:       stations,
			Title:       fmt.Sprintf("Search results for '%s'", query),
			CurrentPath: "",
			Action:      ActionPlay,
			Callbacks:   DefaultRadioCallbacks(client),
		})
		if err != nil {
			errorPrinter.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if HandlePickerResult(result, ActionPlay) {
			os.Exit(1)
		}
	},
}

// radioFavoritesCmd lists favorite stations
var radioFavoritesCmd = &cobra.Command{
	Use:               "favorites [station]",
	Aliases:           []string{"fav"},
	Short:             "Browse and play favorite radio stations",
	ValidArgsFunction: RadioFavoritesCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		removeFav, _ := cmd.Flags().GetBool("remove")
		client := kefw2.NewAirableClient(currentSpeaker)

		resp, err := client.GetRadioFavoritesAll()
		if err != nil {
			errorPrinter.Printf("Failed to get favorites: %v\n", err)
			os.Exit(1)
		}

		stations := filterPlayableStations(resp.Rows)

		if len(stations) == 0 {
			contentPrinter.Println("No favorites found.")
			return
		}

		// If a station name was provided, find and play/remove it directly
		if len(args) > 0 {
			stationName := strings.Join(args, " ")
			if station, found := findItemByName(stations, stationName); found {
				if removeFav {
					headerPrinter.Printf("Removing: %s\n", station.Title)
					if err := client.RemoveRadioFavorite(station); err != nil {
						errorPrinter.Printf("Failed to remove favorite: %v\n", err)
						os.Exit(1)
					}
					taskConpletedPrinter.Printf("Removed from favorites: %s\n", station.Title)
				} else {
					headerPrinter.Printf("Playing: %s\n", station.Title)
					if err := playRadioStationWithDetails(client, station); err != nil {
						errorPrinter.Printf("Failed to play: %v\n", err)
						os.Exit(1)
					}
					taskConpletedPrinter.Printf("Now playing: %s\n", station.Title)
				}
				return
			}
			errorPrinter.Printf("Station '%s' not found in favorites.\n", stationName)
			os.Exit(1)
		}

		// Show interactive picker using unified content picker
		action := ActionPlay
		title := "Favorite Radio Stations"
		if removeFav {
			action = ActionRemoveFavorite
			title = title + " (remove mode)"
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceRadio,
			Items:       stations,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultRadioCallbacks(client),
		})
		if err != nil {
			errorPrinter.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if HandlePickerResult(result, action) {
			os.Exit(1)
		}
	},
}

// radioPlayCmd plays a station by name
var radioPlayCmd = &cobra.Command{
	Use:               "play [station name]",
	Short:             "Play a radio station by searching and playing the first match, or select from results",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: RadioPlayCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		query := strings.Join(args, " ")
		client := kefw2.NewAirableClient(currentSpeaker)

		headerPrinter.Printf("Searching for: %s\n", query)

		resp, err := client.SearchRadio(query)
		if err != nil {
			errorPrinter.Printf("Search failed: %v\n", err)
			os.Exit(1)
		}

		stations := filterPlayableStations(resp.Rows)

		if len(stations) == 0 {
			errorPrinter.Println("No playable stations found.")
			os.Exit(1)
		}

		// If only one result, play it directly
		if len(stations) == 1 {
			station := stations[0]
			headerPrinter.Printf("Playing: %s\n", station.Title)
			if err := client.PlayRadioStation(&station); err != nil {
				errorPrinter.Printf("Failed to play: %v\n", err)
				os.Exit(1)
			}
			taskConpletedPrinter.Printf("Now playing: %s\n", station.Title)
			return
		}

		// Multiple results - show interactive picker using unified content picker
		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceRadio,
			Items:       stations,
			Title:       fmt.Sprintf("Multiple matches for '%s' - select one", query),
			CurrentPath: "",
			Action:      ActionPlay,
			Callbacks:   DefaultRadioCallbacks(client),
		})
		if err != nil {
			errorPrinter.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if HandlePickerResult(result, ActionPlay) {
			os.Exit(1)
		}
	},
}

// radioPopularCmd lists popular stations
var radioPopularCmd = MakeCategoryCommand(CategoryConfig{
	Use:               "popular [station]",
	Short:             "Browse and play popular radio stations",
	ValidArgsFunction: RadioPopularCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetRadioPopularAll() },
	EmptyMessage:      "No popular stations found.",
	Title:             "Popular Radio Stations",
	ServiceType:       ServiceRadio,
	Callbacks:         DefaultRadioCallbacks,
	FilterItems:       filterPlayableStations,
	FindByName:        findItemByName,
	PlayItem:          func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.ResolveAndPlayRadioStation(i) },
	AddFavorite:       func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.AddRadioFavorite(i) },
	SupportsSaveFav:   true,
})

// radioLocalCmd lists local stations
var radioLocalCmd = MakeCategoryCommand(CategoryConfig{
	Use:               "local [station]",
	Short:             "Browse and play local radio stations",
	ValidArgsFunction: RadioLocalCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetRadioLocalAll() },
	EmptyMessage:      "No local stations found.",
	Title:             "Local Radio Stations",
	ServiceType:       ServiceRadio,
	Callbacks:         DefaultRadioCallbacks,
	FilterItems:       filterPlayableStations,
	FindByName:        findItemByName,
	PlayItem:          func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.ResolveAndPlayRadioStation(i) },
	AddFavorite:       func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.AddRadioFavorite(i) },
	SupportsSaveFav:   true,
})

// radioTrendingCmd lists trending stations
var radioTrendingCmd = MakeCategoryCommand(CategoryConfig{
	Use:               "trending [station]",
	Short:             "Browse and play trending radio stations",
	ValidArgsFunction: RadioTrendingCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetRadioTrendingAll() },
	EmptyMessage:      "No trending stations found.",
	Title:             "Trending Radio Stations",
	ServiceType:       ServiceRadio,
	Callbacks:         DefaultRadioCallbacks,
	FilterItems:       filterPlayableStations,
	FindByName:        findItemByName,
	PlayItem:          func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.ResolveAndPlayRadioStation(i) },
	AddFavorite:       func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.AddRadioFavorite(i) },
	SupportsSaveFav:   true,
})

// radioHQCmd lists high quality stations
var radioHQCmd = MakeCategoryCommand(CategoryConfig{
	Use:               "hq [station]",
	Aliases:           []string{"highquality"},
	Short:             "Browse and play high quality radio stations",
	ValidArgsFunction: RadioHQCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetRadioHQAll() },
	EmptyMessage:      "No HQ stations found.",
	Title:             "High Quality Radio Stations",
	ServiceType:       ServiceRadio,
	Callbacks:         DefaultRadioCallbacks,
	FilterItems:       filterPlayableStations,
	FindByName:        findItemByName,
	PlayItem:          func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.ResolveAndPlayRadioStation(i) },
	AddFavorite:       func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.AddRadioFavorite(i) },
	SupportsSaveFav:   true,
})

// radioNewCmd lists new stations
var radioNewCmd = MakeCategoryCommand(CategoryConfig{
	Use:               "new [station]",
	Short:             "Browse and play new radio stations",
	ValidArgsFunction: RadioNewCompletion,
	Fetcher:           func(c *kefw2.AirableClient) (*kefw2.RowsResponse, error) { return c.GetRadioNewAll() },
	EmptyMessage:      "No new stations found.",
	Title:             "New Radio Stations",
	ServiceType:       ServiceRadio,
	Callbacks:         DefaultRadioCallbacks,
	FilterItems:       filterPlayableStations,
	FindByName:        findItemByName,
	PlayItem:          func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.ResolveAndPlayRadioStation(i) },
	AddFavorite:       func(c *kefw2.AirableClient, i *kefw2.ContentItem) error { return c.AddRadioFavorite(i) },
	SupportsSaveFav:   true,
})

// radioBrowseCmd browses radio categories with hierarchical path completion
var radioBrowseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Browse radio categories with tab completion",
	Long: `Browse radio categories and stations with hierarchical tab completion.

Use TAB to navigate through categories:
  kefw2 radio browse <TAB>              - Show top-level categories
  kefw2 radio browse "by Genre/"<TAB>   - Show available genres
  kefw2 radio browse "by Genre/Jazz"    - Show Jazz stations in picker

Path escaping:
  Names containing "/" are escaped as "%2F"
  Example: "AC/DC" becomes "AC%2FDC" in paths

When you reach a category with playable stations, press Enter to open
an interactive fuzzy-filter picker.`,
	ValidArgsFunction: HierarchicalRadioCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)

		browsePath := ""
		if len(args) > 0 {
			browsePath = args[0]
		}

		resp, err := client.BrowseRadioByDisplayPath(browsePath)
		if err != nil {
			errorPrinter.Printf("Browse failed: %v\n", err)
			os.Exit(1)
		}

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No items found at this path.")
			return
		}

		// Count playable items vs containers
		var playableItems []kefw2.ContentItem
		for _, item := range resp.Rows {
			if item.Type != "container" || item.ContainerPlayable {
				playableItems = append(playableItems, item)
			}
		}

		// If there's exactly 1 playable item, play/save it directly (no picker)
		if len(playableItems) == 1 && !saveFavoriteFlag {
			station := &playableItems[0]
			headerPrinter.Printf("Playing: %s\n", station.Title)
			if err := client.ResolveAndPlayRadioStation(station); err != nil {
				errorPrinter.Printf("Failed to play: %v\n", err)
				os.Exit(1)
			}
			taskConpletedPrinter.Printf("Now playing: %s\n", station.Title)
			return
		}

		if len(playableItems) == 1 && saveFavoriteFlag {
			station := &playableItems[0]
			headerPrinter.Printf("Saving: %s\n", station.Title)
			if err := client.AddRadioFavorite(station); err != nil {
				errorPrinter.Printf("Failed to save favorite: %v\n", err)
				os.Exit(1)
			}
			taskConpletedPrinter.Printf("Saved to favorites: %s\n", station.Title)
			return
		}

		// Multiple items or only containers - use the unified content picker
		title := "Radio"
		if browsePath != "" {
			title = fmt.Sprintf("Browse: %s (%d items)", browsePath, len(resp.Rows))
		}

		action := ActionPlay
		if saveFavoriteFlag {
			action = ActionSaveFavorite
			title = title + " (save mode)"
		}

		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceRadio,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: browsePath,
			Action:      action,
			Callbacks:   DefaultRadioCallbacks(client),
		})
		if err != nil {
			errorPrinter.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if HandlePickerResult(result, action) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(radioCmd)
	radioCmd.AddCommand(radioSearchCmd)
	radioCmd.AddCommand(radioFavoritesCmd)
	radioCmd.AddCommand(radioPlayCmd)
	radioCmd.AddCommand(radioPopularCmd)
	radioCmd.AddCommand(radioLocalCmd)
	radioCmd.AddCommand(radioTrendingCmd)
	radioCmd.AddCommand(radioHQCmd)
	radioCmd.AddCommand(radioNewCmd)
	radioCmd.AddCommand(radioBrowseCmd)

	// Flags for radioBrowseCmd
	radioBrowseCmd.Flags().BoolVarP(&saveFavoriteFlag, "save-favorite", "f", false, "Save selected station as favorite instead of playing")

	// Note: radioPopularCmd, radioLocalCmd, radioTrendingCmd, radioHQCmd, radioNewCmd
	// get their --save-favorite flag automatically from MakeCategoryCommand

	// Flags for radioFavoritesCmd - remove favorite
	radioFavoritesCmd.Flags().BoolP("remove", "r", false, "Remove selected station from favorites instead of playing")
}

// RadioSearchCompletion provides completion for radio search queries
func RadioSearchCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	// Suggest common radio search terms
	return []string{
		"BBC",
		"NPR",
		"Jazz",
		"Classical",
		"News",
		"Rock",
		"Pop",
	}, cobra.ShellCompDirectiveNoFileComp
}

// RadioPlayCompletion provides dynamic completion for radio play command
// It fetches popular stations from the speaker for suggestions
func RadioPlayCompletion(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if currentSpeaker.IPAddress == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	client := kefw2.NewAirableClient(currentSpeaker)

	// Try to get popular stations for completion
	resp, err := client.GetRadioPopular()
	if err != nil {
		// Fall back to favorites if popular fails
		resp, err = client.GetRadioFavorites()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
	}

	var completions []string
	for _, station := range resp.Rows {
		if station.ContainerPlayable && station.AudioType == "audioBroadcast" || station.Type == "audio" {
			// Filter by what user has typed so far
			if toComplete == "" || strings.HasPrefix(strings.ToLower(station.Title), strings.ToLower(toComplete)) {
				completions = append(completions, station.Title)
			}
		}
		// Limit to 20 suggestions
		if len(completions) >= 20 {
			break
		}
	}

	return completions, cobra.ShellCompDirectiveNoFileComp
}
