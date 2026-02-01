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

import "github.com/charmbracelet/lipgloss"

// ServiceType represents the type of content service (radio, podcast, upnp)
type ServiceType string

const (
	ServiceRadio   ServiceType = "radio"
	ServicePodcast ServiceType = "podcast"
	ServiceUPnP    ServiceType = "upnp"
)

// ServiceColors defines the color scheme for each service type
var ServiceColors = map[ServiceType]lipgloss.Color{
	ServiceRadio:   lipgloss.Color("39"),  // Blue for radio
	ServicePodcast: lipgloss.Color("207"), // Pink/magenta for podcast
	ServiceUPnP:    lipgloss.Color("214"), // Orange for UPnP
}

// BrowserStyles holds styled renderers for the content browser TUI
type BrowserStyles struct {
	Title    lipgloss.Style
	Search   lipgloss.Style
	Status   lipgloss.Style
	Selected lipgloss.Style
	Playing  lipgloss.Style
	Error    lipgloss.Style
	Dimmed   lipgloss.Style
}

// NewBrowserStyles creates a new set of browser styles for the given service type
func NewBrowserStyles(service ServiceType) BrowserStyles {
	color := ServiceColors[service]
	if color == "" {
		color = lipgloss.Color("39") // Default to blue
	}

	return BrowserStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(color).
			MarginBottom(1),

		Search: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color).
			Padding(0, 1),

		Status: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1),

		Selected: lipgloss.NewStyle().
			Foreground(color),

		Playing: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("82")), // Green for playing

		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")), // Red for errors

		Dimmed: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
	}
}

// Common styles used across all services
var (
	// containerSuffix is shown after container names
	containerSuffix = "/"

	// maxVisibleItems is the number of items visible in the picker at once
	maxVisibleItems = 15
)
