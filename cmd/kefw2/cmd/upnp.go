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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

// Styles for the UPnP TUI
var (
	upnpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")).
			MarginBottom(1)

	upnpStatusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)

	upnpSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))

	upnpPlayingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("82"))
)

// upnpItem represents a media server, container, or track in the list
type upnpItem struct {
	item kefw2.ContentItem
}

func (i upnpItem) Title() string {
	prefix := ""
	switch i.item.Type {
	case "container":
		prefix = "[Folder] "
	case "audio":
		prefix = "[Track] "
	}
	return prefix + i.item.Title
}

func (i upnpItem) Description() string {
	if i.item.MediaData != nil && i.item.MediaData.MetaData.Artist != "" {
		return i.item.MediaData.MetaData.Artist
	}
	return i.item.LongDescription
}

func (i upnpItem) FilterValue() string { return i.item.Title }

// upnpModel is the Bubbletea model for the UPnP browser
type upnpModel struct {
	client      *kefw2.AirableClient
	list        list.Model
	items       []kefw2.ContentItem
	loading     bool
	playing     string
	err         error
	width       int
	height      int
	breadcrumbs []breadcrumb // Navigation history
	quitting    bool
}

type breadcrumb struct {
	title string
	path  string
}

// Messages for async operations
type upnpBrowseResultMsg struct {
	items []kefw2.ContentItem
	err   error
}

type upnpPlayResultMsg struct {
	trackName string
	err       error
}

type upnpQueueResultMsg struct {
	trackName string
	count     int
	err       error
}

func initialUpnpModel(client *kefw2.AirableClient) upnpModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = upnpSelectedStyle
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Media Servers"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	return upnpModel{
		client:      client,
		list:        l,
		breadcrumbs: []breadcrumb{{title: "Media Servers", path: "ui:/upnp"}},
	}
}

func (m upnpModel) Init() tea.Cmd {
	return m.loadServers()
}

func (m upnpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't handle navigation keys when filtering
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if item, ok := m.list.SelectedItem().(upnpItem); ok {
				switch item.item.Type {
				case "container":
					// Navigate into container
					m.breadcrumbs = append(m.breadcrumbs, breadcrumb{
						title: item.item.Title,
						path:  item.item.Path,
					})
					m.loading = true
					return m, m.browseContainer(item.item.Path)
				case "audio":
					// Play the track
					return m, m.playTrack(&item.item)
				default:
					// Try to browse it (might be a server)
					m.breadcrumbs = append(m.breadcrumbs, breadcrumb{
						title: item.item.Title,
						path:  item.item.Path,
					})
					m.loading = true
					return m, m.browseContainer(item.item.Path)
				}
			}
		case "backspace", "esc", "h", "left":
			// Go back
			if len(m.breadcrumbs) > 1 {
				m.breadcrumbs = m.breadcrumbs[:len(m.breadcrumbs)-1]
				current := m.breadcrumbs[len(m.breadcrumbs)-1]
				m.loading = true
				return m, m.browseContainer(current.path)
			}
		case "p":
			// Play all tracks in current container
			if len(m.items) > 0 {
				return m, m.playAll()
			}
		case "r":
			// Refresh current view
			if len(m.breadcrumbs) > 0 {
				current := m.breadcrumbs[len(m.breadcrumbs)-1]
				m.loading = true
				return m, m.browseContainer(current.path)
			}
		case "a":
			// Add selected track to queue
			if i, ok := m.list.SelectedItem().(upnpItem); ok {
				if i.item.Type == "audio" {
					return m, m.addToQueue(&i.item)
				}
			}
		case "A":
			// Add all audio tracks to queue
			return m, m.addAllToQueue()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 4
		footerHeight := 3
		listHeight := m.height - headerHeight - footerHeight
		if listHeight < 5 {
			listHeight = 5
		}
		m.list.SetSize(m.width-4, listHeight)

	case upnpBrowseResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.items = msg.items
			m.err = nil
			items := make([]list.Item, len(msg.items))
			for i, s := range msg.items {
				items[i] = upnpItem{item: s}
			}
			m.list.SetItems(items)
		}

	case upnpPlayResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.playing = msg.trackName
			m.err = nil
		}

	case upnpQueueResultMsg:
		if msg.err != nil {
			m.err = msg.err
		} else if msg.count == 1 {
			m.playing = fmt.Sprintf("Added to queue: %s", msg.trackName)
		} else {
			m.playing = fmt.Sprintf("Added %d tracks to queue", msg.count)
		}
	}

	// Update list
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m upnpModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Build breadcrumb path
	var pathParts []string
	for _, bc := range m.breadcrumbs {
		pathParts = append(pathParts, bc.title)
	}
	title := strings.Join(pathParts, " > ")
	b.WriteString(upnpTitleStyle.Render(title))
	b.WriteString("\n\n")

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
		status = upnpPlayingStyle.Render(fmt.Sprintf("Now playing: %s", m.playing))
	} else {
		status = "Enter: play | a: add to queue | A: add all | p: play all | Backspace: back | q: quit"
	}
	b.WriteString("\n")
	b.WriteString(upnpStatusStyle.Render(status))

	return b.String()
}

// Command functions
func (m upnpModel) loadServers() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.GetMediaServers()
		if err != nil {
			return upnpBrowseResultMsg{err: err}
		}
		return upnpBrowseResultMsg{items: resp.Rows}
	}
}

func (m upnpModel) browseContainer(path string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.client.BrowseContainer(path)
		if err != nil {
			return upnpBrowseResultMsg{err: err}
		}
		return upnpBrowseResultMsg{items: resp.Rows}
	}
}

func (m upnpModel) playTrack(track *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.PlayUPnPTrack(track)
		if err != nil {
			return upnpPlayResultMsg{err: err}
		}
		return upnpPlayResultMsg{trackName: track.Title}
	}
}

func (m upnpModel) playAll() tea.Cmd {
	return func() tea.Msg {
		// Filter for audio tracks
		var tracks []kefw2.ContentItem
		for _, item := range m.items {
			if item.Type == "audio" {
				tracks = append(tracks, item)
			}
		}

		if len(tracks) == 0 {
			return upnpPlayResultMsg{err: fmt.Errorf("no audio tracks in current folder")}
		}

		err := m.client.PlayUPnPTracks(tracks)
		if err != nil {
			return upnpPlayResultMsg{err: err}
		}
		return upnpPlayResultMsg{trackName: fmt.Sprintf("%d tracks", len(tracks))}
	}
}

func (m upnpModel) addToQueue(track *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		err := m.client.AddToQueue([]kefw2.ContentItem{*track}, false)
		if err != nil {
			return upnpQueueResultMsg{err: err}
		}
		return upnpQueueResultMsg{trackName: track.Title, count: 1}
	}
}

func (m upnpModel) addAllToQueue() tea.Cmd {
	return func() tea.Msg {
		var tracks []kefw2.ContentItem
		for _, item := range m.items {
			if item.Type == "audio" {
				tracks = append(tracks, item)
			}
		}
		if len(tracks) == 0 {
			return upnpQueueResultMsg{err: fmt.Errorf("no audio tracks in current folder")}
		}
		err := m.client.AddToQueue(tracks, false)
		if err != nil {
			return upnpQueueResultMsg{err: err}
		}
		return upnpQueueResultMsg{count: len(tracks)}
	}
}

// upnpCmd represents the upnp command
var upnpCmd = &cobra.Command{
	Use:     "upnp",
	Aliases: []string{"media", "dlna"},
	Short:   "Browse and play from UPnP/DLNA media servers",
	Long: `Browse and play music from UPnP/DLNA media servers (Plex, Sonos, etc.)
on your local network.

Interactive mode: Opens a TUI for browsing media servers.

Keyboard shortcuts:
  Enter     - Open folder or play track
  a         - Add selected track to queue
  A         - Add all tracks in folder to queue
  p         - Play all tracks in current folder
  Backspace - Go back to parent folder
  r         - Refresh current view
  /         - Filter items
  q         - Quit`,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		model := initialUpnpModel(client)

		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			errorPrinter.Printf("Error running media browser: %v\n", err)
			os.Exit(1)
		}
	},
}

// upnpBrowseCmd browses a specific path
var upnpBrowseCmd = &cobra.Command{
	Use:   "browse [path]",
	Short: "Browse a media server path with interactive picker",
	Long: `Browse a specific path on a media server with an interactive picker.
If no path is provided and a default server is configured, browses the default server.
Otherwise, lists available media servers.

Paths can be display paths (e.g., "Music/Albums") or full API paths (e.g., "upnp:/...").
Use tab completion to navigate the folder hierarchy.

Flags:
  -q, --queue    Add to queue instead of playing when selecting a track

You can navigate into folders and play tracks directly from the picker.`,
	ValidArgsFunction: HierarchicalUPnPCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		client := kefw2.NewAirableClient(currentSpeaker)
		addToQueue, _ := cmd.Flags().GetBool("queue")

		var resp *kefw2.RowsResponse
		var err error
		var title string

		if len(args) > 0 {
			path := args[0]
			// Check if it's an API path or display path
			if strings.HasPrefix(path, "upnp:/") || strings.HasPrefix(path, "ui:/") {
				// API path - use directly
				resp, err = client.BrowseContainer(path)
				title = fmt.Sprintf("Browse: %s", path)
			} else {
				// Display path - resolve via default server
				serverPath := viper.GetString("upnp.default_server_path")
				resp, err = client.BrowseUPnPByDisplayPath(path, serverPath)
				title = fmt.Sprintf("Browse: %s", path)
			}
		} else {
			// No args - check for default server or show server list
			serverPath := viper.GetString("upnp.default_server_path")
			if serverPath != "" {
				resp, err = client.BrowseContainer(serverPath)
				title = viper.GetString("upnp.default_server")
			} else {
				resp, err = client.GetMediaServers()
				title = "Media Servers"
			}
		}

		if err != nil {
			errorPrinter.Printf("Failed to browse: %v\n", err)
			os.Exit(1)
		}

		if len(resp.Rows) == 0 {
			contentPrinter.Println("No items found.")
			return
		}

		// Show interactive picker using unified content picker
		action := ActionPlay
		if addToQueue {
			action = ActionAddToQueue
			title = title + " (queue mode)"
		}
		result, err := RunContentPicker(ContentPickerConfig{
			ServiceType: ServiceUPnP,
			Items:       resp.Rows,
			Title:       title,
			CurrentPath: "",
			Action:      action,
			Callbacks:   DefaultUPnPCallbacks(client),
		})
		if err != nil {
			errorPrinter.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if result.Queued && result.Selected != nil {
			taskConpletedPrinter.Printf("Added to queue: %s\n", result.Selected.Title)
		} else if result.Played && result.Selected != nil {
			taskConpletedPrinter.Printf("Now playing: %s\n", result.Selected.Title)
		} else if result.Error != nil {
			errorPrinter.Printf("Failed to play: %v\n", result.Error)
			os.Exit(1)
		}
	},
}

// upnpPlayCmd plays content from a path
var upnpPlayCmd = &cobra.Command{
	Use:   "play <path>",
	Short: "Play content from a UPnP path",
	Long: `Play all audio tracks from a UPnP path (album, folder, etc.)

Paths can be display paths (e.g., "Music/Albums/Abbey Road") or full API paths.
Use tab completion to navigate the folder hierarchy.

Flags:
  -q, --queue    Add to queue instead of playing immediately
                 (starts playback only if queue was empty)

Examples:
  kefw2 upnp play "Music/Albums/Abbey Road"
  kefw2 upnp play -q "Music/Albums/Abbey Road"    # Add to queue
  kefw2 upnp play "upnp:/uuid:xxx/container123?itemType=container"`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: HierarchicalUPnPCompletion,
	Run: func(cmd *cobra.Command, args []string) {
		path := args[0]
		client := kefw2.NewAirableClient(currentSpeaker)
		addToQueue, _ := cmd.Flags().GetBool("queue")

		// Check if it's an API path or display path
		if strings.HasPrefix(path, "upnp:/") || strings.HasPrefix(path, "ui:/") {
			// API path - we need to browse to get the tracks
			resp, err := client.BrowseContainer(path)
			if err != nil {
				errorPrinter.Printf("Failed to browse path: %v\n", err)
				os.Exit(1)
			}

			var tracks []kefw2.ContentItem
			for _, item := range resp.Rows {
				if item.Type == "audio" {
					tracks = append(tracks, item)
				}
			}

			if len(tracks) == 0 {
				errorPrinter.Println("No audio tracks found at this path.")
				os.Exit(1)
			}

			if addToQueue {
				headerPrinter.Printf("Adding to queue: %s\n", path)
				if err := client.AddToQueue(tracks, true); err != nil {
					errorPrinter.Printf("Failed to add to queue: %v\n", err)
					os.Exit(1)
				}
				taskConpletedPrinter.Printf("Added %d tracks to queue.\n", len(tracks))
			} else {
				headerPrinter.Printf("Playing from: %s\n", path)
				if err := client.PlayUPnPTracks(tracks); err != nil {
					errorPrinter.Printf("Failed to play: %v\n", err)
					os.Exit(1)
				}
				taskConpletedPrinter.Println("Playback started!")
			}
		} else {
			// Display path - resolve via default server then play
			serverPath := viper.GetString("upnp.default_server_path")
			resp, err := client.BrowseUPnPByDisplayPath(path, serverPath)
			if err != nil {
				errorPrinter.Printf("Failed to resolve path: %v\n", err)
				os.Exit(1)
			}

			// Collect audio tracks from the response
			var tracks []kefw2.ContentItem
			for _, item := range resp.Rows {
				if item.Type == "audio" {
					tracks = append(tracks, item)
				}
			}

			if len(tracks) == 0 {
				errorPrinter.Println("No audio tracks found at this path.")
				os.Exit(1)
			}

			if addToQueue {
				headerPrinter.Printf("Adding to queue: %s\n", path)
				if err := client.AddToQueue(tracks, true); err != nil {
					errorPrinter.Printf("Failed to add to queue: %v\n", err)
					os.Exit(1)
				}
				taskConpletedPrinter.Printf("Added %d tracks to queue.\n", len(tracks))
			} else {
				headerPrinter.Printf("Playing from: %s\n", path)
				if err := client.PlayUPnPTracks(tracks); err != nil {
					errorPrinter.Printf("Failed to play: %v\n", err)
					os.Exit(1)
				}
				taskConpletedPrinter.Println("Playback started!")
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(upnpCmd)
	upnpCmd.AddCommand(upnpBrowseCmd)
	upnpCmd.AddCommand(upnpPlayCmd)

	// Add -q flag for queue mode
	upnpBrowseCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing when selecting a track")
	upnpPlayCmd.Flags().BoolP("queue", "q", false, "Add to queue instead of playing immediately")
}
