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

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hilli/go-kef-w2/kefw2"
)

// ContentAction represents the action to take on a selected item
type ContentAction int

const (
	ActionPlay ContentAction = iota
	ActionSaveFavorite
	ActionRemoveFavorite
	ActionAddToQueue
)

// ContentPickerCallbacks defines the callbacks for content picker actions.
// This allows the picker to work with different services (radio, podcast, upnp).
type ContentPickerCallbacks struct {
	// Navigate is called when entering a container. Returns items and new path.
	Navigate func(item *kefw2.ContentItem, currentPath string) ([]kefw2.ContentItem, string, error)

	// Play is called when a playable item is selected with ActionPlay.
	Play func(item *kefw2.ContentItem) error

	// SaveFavorite is called when a playable item is selected with ActionSaveFavorite.
	SaveFavorite func(item *kefw2.ContentItem) error

	// RemoveFavorite is called when a playable item is selected with ActionRemoveFavorite.
	RemoveFavorite func(item *kefw2.ContentItem) error

	// AddToQueue is called when a playable item is selected with ActionAddToQueue.
	AddToQueue func(item *kefw2.ContentItem) error

	// DeleteFromQueue is called when Ctrl+d is pressed to delete an item from the queue.
	DeleteFromQueue func(item *kefw2.ContentItem) error

	// ClearQueue is called when Ctrl+x is pressed to clear the entire queue.
	ClearQueue func() error

	// IsPlayable determines if an item is playable (not a container to navigate into).
	IsPlayable func(item *kefw2.ContentItem) bool
}

// ContentPickerResult represents the outcome of running the content picker.
type ContentPickerResult struct {
	Selected  *kefw2.ContentItem
	Action    ContentAction
	Played    bool
	Saved     bool
	Removed   bool
	Queued    bool
	Cancelled bool
	Error     error
}

// ContentPickerModel is a unified picker for browsing content from any service.
// It handles containers, playable items, fuzzy filtering, and multiple actions.
type ContentPickerModel struct {
	// Configuration
	serviceType ServiceType
	callbacks   ContentPickerCallbacks
	action      ContentAction
	styles      BrowserStyles

	// State
	allItems    []kefw2.ContentItem
	filtered    []kefw2.ContentItem
	filterInput textinput.Model
	cursor      int
	currentPath string
	title       string
	loading     bool
	quitting    bool
	statusMsg   string // Temporary status message (e.g., "Added to queue")

	// Result
	selected *kefw2.ContentItem
	played   bool
	saved    bool
	removed  bool
	queued   bool
	err      error
}

// ContentPickerConfig contains configuration for creating a new content picker.
type ContentPickerConfig struct {
	ServiceType ServiceType
	Items       []kefw2.ContentItem
	Title       string
	CurrentPath string
	Action      ContentAction
	Callbacks   ContentPickerCallbacks
	SearchQuery string // Optional: if set, auto-focus on matching item
}

// NewContentPickerModel creates a new content picker model.
func NewContentPickerModel(cfg ContentPickerConfig) ContentPickerModel {
	ti := textinput.New()
	ti.Placeholder = "Type to filter..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	model := ContentPickerModel{
		serviceType: cfg.ServiceType,
		callbacks:   cfg.Callbacks,
		action:      cfg.Action,
		styles:      NewBrowserStyles(cfg.ServiceType),
		allItems:    cfg.Items,
		filtered:    cfg.Items,
		filterInput: ti,
		title:       cfg.Title,
		currentPath: cfg.CurrentPath,
	}

	// Auto-focus on matching item if search query provided
	if cfg.SearchQuery != "" {
		query := strings.TrimSuffix(cfg.SearchQuery, "/")
		for i, item := range cfg.Items {
			itemTitle := strings.TrimSuffix(item.Title, "/")
			if strings.EqualFold(itemTitle, query) {
				model.cursor = i
				break
			}
		}
	}

	return model
}

// Init implements tea.Model.
func (m ContentPickerModel) Init() tea.Cmd {
	return textinput.Blink
}

// navigateMsg is sent when navigation into a container completes.
type contentNavigateMsg struct {
	items []kefw2.ContentItem
	path  string
	title string
	err   error
}

// navigateToContainer creates a command to navigate into a container.
func (m ContentPickerModel) navigateToContainer(item *kefw2.ContentItem) tea.Cmd {
	return func() tea.Msg {
		if m.callbacks.Navigate == nil {
			return contentNavigateMsg{err: fmt.Errorf("navigation not supported")}
		}

		items, newPath, err := m.callbacks.Navigate(item, m.currentPath)
		if err != nil {
			return contentNavigateMsg{err: err}
		}

		return contentNavigateMsg{
			items: items,
			path:  newPath,
			title: item.Title,
		}
	}
}

// isItemPlayable checks if an item is playable (not a navigable container).
func (m ContentPickerModel) isItemPlayable(item *kefw2.ContentItem) bool {
	if m.callbacks.IsPlayable != nil {
		return m.callbacks.IsPlayable(item)
	}
	// Default: containers that aren't playable are navigable
	return item.Type != "container" || item.ContainerPlayable
}

// Update implements tea.Model.
func (m ContentPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case contentNavigateMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Update to show new items
		m.allItems = msg.items
		m.filtered = msg.items
		m.currentPath = msg.path
		m.title = fmt.Sprintf("Browse: %s", msg.path)
		m.cursor = 0
		m.filterInput.SetValue("")
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}

		case "enter":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				selected := &m.filtered[m.cursor]

				// Check if it's a navigable container
				if !m.isItemPlayable(selected) {
					// Navigate into container
					m.loading = true
					return m, m.navigateToContainer(selected)
				}

				// It's playable - perform the configured action
				m.selected = selected

				switch m.action {
				case ActionPlay:
					if m.callbacks.Play != nil {
						err := m.callbacks.Play(m.selected)
						if err != nil {
							m.err = err
						} else {
							m.played = true
						}
					}
				case ActionSaveFavorite:
					if m.callbacks.SaveFavorite != nil {
						err := m.callbacks.SaveFavorite(m.selected)
						if err != nil {
							m.err = err
						} else {
							m.saved = true
						}
					}
				case ActionRemoveFavorite:
					if m.callbacks.RemoveFavorite != nil {
						err := m.callbacks.RemoveFavorite(m.selected)
						if err != nil {
							m.err = err
						} else {
							m.removed = true
						}
					}
				case ActionAddToQueue:
					if m.callbacks.AddToQueue != nil {
						err := m.callbacks.AddToQueue(m.selected)
						if err != nil {
							m.err = err
						} else {
							m.queued = true
						}
					}
				}
				return m, tea.Quit
			}

		case "ctrl+a":
			// Add selected item to queue without exiting
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				selected := &m.filtered[m.cursor]
				if m.isItemPlayable(selected) && m.callbacks.AddToQueue != nil {
					err := m.callbacks.AddToQueue(selected)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
					} else {
						m.statusMsg = fmt.Sprintf("Added to queue: %s", selected.Title)
					}
				}
			}
			return m, nil

		case "ctrl+d":
			// Delete selected item from queue
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				selected := &m.filtered[m.cursor]
				if m.callbacks.DeleteFromQueue != nil {
					err := m.callbacks.DeleteFromQueue(selected)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
					} else {
						m.statusMsg = fmt.Sprintf("Deleted: %s", selected.Title)
						// Remove from allItems and filtered
						m.allItems = removeItem(m.allItems, selected)
						m.filtered = removeItem(m.filtered, selected)
						if m.cursor >= len(m.filtered) && m.cursor > 0 {
							m.cursor--
						}
					}
				}
			}
			return m, nil

		case "ctrl+x":
			// Clear entire queue
			if m.callbacks.ClearQueue != nil {
				err := m.callbacks.ClearQueue()
				if err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.statusMsg = "Queue cleared"
					m.allItems = nil
					m.filtered = nil
					m.cursor = 0
				}
			}
			return m, nil

		default:
			// Handle text input for filtering
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.applyFilter()
			return m, cmd
		}
	}

	// Update text input for cursor blink etc
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

// removeItem removes an item from a slice by matching Path.
func removeItem(items []kefw2.ContentItem, target *kefw2.ContentItem) []kefw2.ContentItem {
	result := make([]kefw2.ContentItem, 0, len(items)-1)
	for _, item := range items {
		if item.Path != target.Path {
			result = append(result, item)
		}
	}
	return result
}

// applyFilter filters the items based on the current filter input.
func (m *ContentPickerModel) applyFilter() {
	query := m.filterInput.Value()
	if query == "" {
		m.filtered = m.allItems
		m.cursor = 0
		return
	}

	m.filtered = nil
	for _, item := range m.allItems {
		if FuzzyMatch(item.Title, query) {
			m.filtered = append(m.filtered, item)
		}
	}

	// Reset cursor if out of bounds
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// View implements tea.Model.
func (m ContentPickerModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title with result count
	title := fmt.Sprintf("%s (%d items)", m.title, len(m.filtered))
	b.WriteString(m.styles.Title.Render(title))
	b.WriteString("\n")

	// Filter input box
	b.WriteString("Filter: ")
	b.WriteString(m.filterInput.View())
	b.WriteString("\n\n")

	// Loading indicator
	if m.loading {
		b.WriteString(m.styles.Status.Render("  Loading..."))
		b.WriteString("\n")
		return b.String()
	}

	// Show filtered list (max visible items around cursor)
	if len(m.filtered) == 0 {
		b.WriteString(m.styles.Status.Render("  No matching items"))
		b.WriteString("\n")
	} else {
		visibleStart := m.cursor - (maxVisibleItems / 2)
		if visibleStart < 0 {
			visibleStart = 0
		}
		visibleEnd := visibleStart + maxVisibleItems
		if visibleEnd > len(m.filtered) {
			visibleEnd = len(m.filtered)
			visibleStart = visibleEnd - maxVisibleItems
			if visibleStart < 0 {
				visibleStart = 0
			}
		}

		for i := visibleStart; i < visibleEnd; i++ {
			item := m.filtered[i]
			cursor := "  "
			if m.cursor == i {
				cursor = "> "
			}

			// Show "/" suffix for navigable containers
			suffix := ""
			if !m.isItemPlayable(&item) {
				suffix = containerSuffix
			}

			line := item.Title + suffix
			if item.LongDescription != "" && m.isItemPlayable(&item) {
				// Truncate description if too long
				desc := item.LongDescription
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				line = fmt.Sprintf("%s - %s", item.Title, desc)
			}

			if m.cursor == i {
				b.WriteString(m.styles.Selected.Render(cursor + line))
			} else {
				b.WriteString(cursor + line)
			}
			b.WriteString("\n")
		}
	}

	// Error display
	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(m.styles.Error.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n")
	}

	// Status message (e.g., "Added to queue: ...")
	if m.statusMsg != "" {
		b.WriteString("\n")
		b.WriteString(m.styles.Status.Render(m.statusMsg))
	}

	// Status bar with action hint
	b.WriteString("\n")
	actionHint := "select/play"
	switch m.action {
	case ActionSaveFavorite:
		actionHint = "save favorite"
	case ActionRemoveFavorite:
		actionHint = "remove favorite"
	case ActionAddToQueue:
		actionHint = "add to queue"
	}
	extraHints := ""
	if m.callbacks.AddToQueue != nil {
		extraHints += " | Ctrl+a: add to queue"
	}
	if m.callbacks.DeleteFromQueue != nil {
		extraHints += " | Ctrl+d: delete"
	}
	if m.callbacks.ClearQueue != nil {
		extraHints += " | Ctrl+x: clear"
	}
	statusText := fmt.Sprintf("↑/↓: navigate | Type to filter | Enter: %s%s | Esc: quit", actionHint, extraHints)
	b.WriteString(m.styles.Status.Render(statusText))

	return b.String()
}

// Result returns the picker result after the program has exited.
func (m ContentPickerModel) Result() ContentPickerResult {
	return ContentPickerResult{
		Selected:  m.selected,
		Action:    m.action,
		Played:    m.played,
		Saved:     m.saved,
		Removed:   m.removed,
		Queued:    m.queued,
		Cancelled: m.quitting && m.selected == nil,
		Error:     m.err,
	}
}

// DefaultRadioCallbacks returns the default callbacks for radio browsing.
func DefaultRadioCallbacks(client *kefw2.AirableClient) ContentPickerCallbacks {
	return ContentPickerCallbacks{
		Navigate: func(item *kefw2.ContentItem, currentPath string) ([]kefw2.ContentItem, string, error) {
			newPath := item.Title
			if currentPath != "" {
				newPath = currentPath + "/" + item.Title
			}
			resp, err := client.BrowseRadioByDisplayPath(newPath)
			if err != nil {
				return nil, "", err
			}
			return resp.Rows, newPath, nil
		},
		Play: func(item *kefw2.ContentItem) error {
			return client.PlayRadioStation(item)
		},
		SaveFavorite: func(item *kefw2.ContentItem) error {
			return client.AddRadioFavorite(item)
		},
		RemoveFavorite: func(item *kefw2.ContentItem) error {
			return client.RemoveRadioFavorite(item)
		},
		IsPlayable: func(item *kefw2.ContentItem) bool {
			return item.Type != "container" || item.ContainerPlayable
		},
	}
}

// DefaultPodcastCallbacks returns the default callbacks for podcast browsing.
func DefaultPodcastCallbacks(client *kefw2.AirableClient) ContentPickerCallbacks {
	return ContentPickerCallbacks{
		Navigate: func(item *kefw2.ContentItem, currentPath string) ([]kefw2.ContentItem, string, error) {
			// For podcasts, navigate using the item's Path directly
			resp, err := client.GetPodcastEpisodes(item.Path)
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
			return client.PlayPodcastEpisode(item)
		},
		SaveFavorite: func(item *kefw2.ContentItem) error {
			return client.AddPodcastFavorite(item)
		},
		RemoveFavorite: func(item *kefw2.ContentItem) error {
			return client.RemovePodcastFavorite(item)
		},
		AddToQueue: func(item *kefw2.ContentItem) error {
			return client.AddToQueue([]kefw2.ContentItem{*item}, false)
		},
		IsPlayable: func(item *kefw2.ContentItem) bool {
			// Only actual audio episodes are playable; all containers should be navigable
			return item.Type == "audio"
		},
	}
}

// DefaultUPnPCallbacks returns the default callbacks for UPnP browsing.
func DefaultUPnPCallbacks(client *kefw2.AirableClient) ContentPickerCallbacks {
	return ContentPickerCallbacks{
		Navigate: func(item *kefw2.ContentItem, currentPath string) ([]kefw2.ContentItem, string, error) {
			// For UPnP, navigate using the item's Path directly
			resp, err := client.BrowseContainer(item.Path)
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
			return client.PlayUPnPTrack(item)
		},
		AddToQueue: func(item *kefw2.ContentItem) error {
			// UPnP supports adding to queue
			return client.AddToQueue([]kefw2.ContentItem{*item}, false)
		},
		IsPlayable: func(item *kefw2.ContentItem) bool {
			return item.Type != "container" || item.ContainerPlayable
		},
	}
}

// RunContentPicker runs the content picker and returns the result.
func RunContentPicker(cfg ContentPickerConfig) (ContentPickerResult, error) {
	model := NewContentPickerModel(cfg)
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return ContentPickerResult{Error: err}, err
	}

	if m, ok := finalModel.(ContentPickerModel); ok {
		return m.Result(), nil
	}

	return ContentPickerResult{}, fmt.Errorf("unexpected model type")
}
