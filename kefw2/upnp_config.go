/*
Copyright 2023-2026 Jens Hilligsoe

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

package kefw2

import "fmt"

// UPnPConfig holds UPnP/DLNA configuration settings.
// This struct is designed to be serialized to/from YAML config files.
type UPnPConfig struct {
	// DefaultServer is the display name of the default media server
	// e.g., "Plex Media Server: homesrv"
	DefaultServer string `json:"default_server,omitempty" yaml:"default_server,omitempty"`

	// DefaultServerPath is the API path to the default server
	// e.g., "upnp:/uuid:e4016f9f-0337-80b4-4f03-ba488f89f8f0?itemType=server"
	DefaultServerPath string `json:"default_server_path,omitempty" yaml:"default_server_path,omitempty"`

	// BrowseContainer is the container path to start browsing from.
	// e.g., "Music/Hilli's Music"
	// When set, users won't see parent containers or other servers.
	BrowseContainer string `json:"browse_container,omitempty" yaml:"browse_container,omitempty"`

	// IndexContainer is the container path to start indexing from.
	// e.g., "Music/Hilli's Music/By Folder"
	// This determines the scope of the search index.
	// Tip: Use "By Folder" structure for best results (avoids media server shenanigans).
	IndexContainer string `json:"index_container,omitempty" yaml:"index_container,omitempty"`
}

// DefaultUPnPConfig returns an empty config with no restrictions.
func DefaultUPnPConfig() UPnPConfig {
	return UPnPConfig{}
}

// HasServer returns true if a default server is configured.
func (c *UPnPConfig) HasServer() bool {
	return c.DefaultServerPath != ""
}

// HasBrowseContainer returns true if a browse container is configured.
func (c *UPnPConfig) HasBrowseContainer() bool {
	return c.BrowseContainer != ""
}

// HasIndexContainer returns true if an index container is configured.
func (c *UPnPConfig) HasIndexContainer() bool {
	return c.IndexContainer != ""
}

// Validate checks that the config is consistent.
// Returns an error if browse/index containers are set but no server is configured.
func (c *UPnPConfig) Validate() error {
	if c.BrowseContainer != "" && c.DefaultServerPath == "" {
		return fmt.Errorf("browse_container requires a default server to be set")
	}
	if c.IndexContainer != "" && c.DefaultServerPath == "" {
		return fmt.Errorf("index_container requires a default server to be set")
	}
	return nil
}

// ResolvedBrowsePath returns the full API path to the browse container.
// If no browse container is set, returns the server path.
// The client is used to resolve the human-readable path to an API path.
func (c *UPnPConfig) ResolvedBrowsePath(client *AirableClient) (string, error) {
	if c.DefaultServerPath == "" {
		return "", fmt.Errorf("no default server configured")
	}

	if c.BrowseContainer == "" {
		return c.DefaultServerPath, nil
	}

	resolvedPath, _, err := FindContainerByPath(client, c.DefaultServerPath, c.BrowseContainer)
	if err != nil {
		return "", fmt.Errorf("could not resolve browse container '%s': %w", c.BrowseContainer, err)
	}
	return resolvedPath, nil
}

// ResolvedIndexPath returns the full API path to the index container.
// If no index container is set, returns the server path.
// The client is used to resolve the human-readable path to an API path.
func (c *UPnPConfig) ResolvedIndexPath(client *AirableClient) (string, error) {
	if c.DefaultServerPath == "" {
		return "", fmt.Errorf("no default server configured")
	}

	if c.IndexContainer == "" {
		return c.DefaultServerPath, nil
	}

	resolvedPath, _, err := FindContainerByPath(client, c.DefaultServerPath, c.IndexContainer)
	if err != nil {
		return "", fmt.Errorf("could not resolve index container '%s': %w", c.IndexContainer, err)
	}
	return resolvedPath, nil
}

// EffectiveBrowseRoot returns the path that should be used as the "root" for browsing.
// This is either the browse container path or the server path if no container is set.
func (c *UPnPConfig) EffectiveBrowseRoot() string {
	if c.BrowseContainer != "" {
		return c.BrowseContainer
	}
	return ""
}

// IsAtBrowseRoot checks if the given display path is at or above the browse root.
// Returns true if the user should not be able to navigate further up.
func (c *UPnPConfig) IsAtBrowseRoot(displayPath string) bool {
	if c.BrowseContainer == "" {
		// No restriction - only the server itself is root
		return displayPath == "" || displayPath == c.DefaultServer
	}
	// At browse root if path matches or is empty
	return displayPath == "" || displayPath == c.BrowseContainer
}
