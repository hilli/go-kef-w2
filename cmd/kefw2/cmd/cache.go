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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

// CachedItem represents a cached content item for completion.
type CachedItem struct {
	Title       string `json:"title"`
	Path        string `json:"path"`         // Actual API path
	DisplayPath string `json:"display_path"` // Human-readable path for display
	Type        string `json:"type"`         // "container" or "playable"
	Description string `json:"description"`  // Optional description
}

// CacheEntry represents a cached response for a browse path.
type CacheEntry struct {
	Items     []CachedItem `json:"items"`
	FetchedAt time.Time    `json:"fetched_at"`
}

// BrowseCache provides caching for hierarchical path completion.
type BrowseCache struct {
	cacheDir string
	entries  map[string]*CacheEntry
	mu       sync.RWMutex
	dirty    bool // Track if cache needs saving
}

// Global cache instance.
var browseCache *BrowseCache

// NewBrowseCache creates a new browse cache.
func NewBrowseCache() (*BrowseCache, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		// Fallback to temp dir
		cacheDir = os.TempDir()
	}
	cacheDir = filepath.Join(cacheDir, "kefw2")

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &BrowseCache{
		cacheDir: cacheDir,
		entries:  make(map[string]*CacheEntry),
	}

	// Load existing cache from disk
	_ = cache.Load() // Ignore error on load - start fresh if corrupted

	return cache, nil
}

// InitCache initializes the global browse cache.
func InitCache() {
	var err error
	browseCache, err = NewBrowseCache()
	if err != nil {
		// Log warning but don't fail - caching is optional
		fmt.Fprintf(os.Stderr, "Warning: Could not initialize cache: %v\n", err)
		browseCache = &BrowseCache{
			entries: make(map[string]*CacheEntry),
		}
	}
}

// cacheFilePath returns the path to the cache file.
func (c *BrowseCache) cacheFilePath() string {
	return filepath.Join(c.cacheDir, "browse_cache.json")
}

// IsEnabled returns whether caching is enabled.
func (c *BrowseCache) IsEnabled() bool {
	return viper.GetBool("cache.enabled")
}

// GetTTL returns the TTL for a given service type.
// Defaults are set in root.go initConfig(): default=300, radio=300, podcast=300, upnp=60.
func (c *BrowseCache) GetTTL(service string) time.Duration {
	key := fmt.Sprintf("cache.ttl_%s", service)
	seconds := viper.GetInt(key)
	if seconds <= 0 {
		// Use ttl-default for unknown services
		seconds = viper.GetInt("cache.ttl_default")
		if seconds <= 0 {
			seconds = 300
		}
	}
	return time.Duration(seconds) * time.Second
}

// Get retrieves items from cache if valid.
func (c *BrowseCache) Get(path string, service string) ([]CachedItem, bool) {
	if !c.IsEnabled() {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", service, path)
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// Check if expired
	ttl := c.GetTTL(service)
	if time.Since(entry.FetchedAt) > ttl {
		return nil, false
	}

	return entry.Items, true
}

// Set stores items in cache.
func (c *BrowseCache) Set(path string, service string, items []CachedItem) {
	if !c.IsEnabled() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("%s:%s", service, path)
	c.entries[key] = &CacheEntry{
		Items:     items,
		FetchedAt: time.Now(),
	}
	c.dirty = true
}

// Clear removes all cached data.
func (c *BrowseCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.dirty = false

	// Remove cache file
	return os.Remove(c.cacheFilePath())
}

// Status returns cache statistics.
func (c *BrowseCache) Status() (entries int, size int64, oldestAge time.Duration) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries = len(c.entries)

	var oldest time.Time
	for _, entry := range c.entries {
		if oldest.IsZero() || entry.FetchedAt.Before(oldest) {
			oldest = entry.FetchedAt
		}
	}

	if !oldest.IsZero() {
		oldestAge = time.Since(oldest)
	}

	// Get file size
	if info, err := os.Stat(c.cacheFilePath()); err == nil {
		size = info.Size()
	}

	return
}

// Load loads cache from disk.
func (c *BrowseCache) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.cacheFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No cache file yet
		}
		return err
	}

	var entries map[string]*CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	c.entries = entries
	return nil
}

// Save persists cache to disk.
func (c *BrowseCache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.dirty {
		return nil
	}

	// Clean expired entries before saving
	now := time.Now()
	for key, entry := range c.entries {
		// Use max TTL (5 min) for cleanup
		if now.Sub(entry.FetchedAt) > 5*time.Minute {
			delete(c.entries, key)
		}
	}

	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(c.cacheFilePath(), data, 0600); err != nil {
		return err
	}

	c.dirty = false
	return nil
}

// CacheDir returns the cache directory path.
func (c *BrowseCache) CacheDir() string {
	return c.cacheDir
}

// FindItemByTitle finds a cached item by its title within a parent path.
func (c *BrowseCache) FindItemByTitle(parentPath, service, title string) (*CachedItem, bool) {
	items, ok := c.Get(parentPath, service)
	if !ok {
		return nil, false
	}

	for _, item := range items {
		if item.Title == title {
			return &item, true
		}
	}
	return nil, false
}

// ResolveDisplayPath resolves a display path (title-based) to an API path.
// Returns the API path and the last item, or empty string if not found.
func (c *BrowseCache) ResolveDisplayPath(displayPath, service string) (string, *CachedItem, bool) {
	if displayPath == "" {
		return "", nil, true // Empty path = top level, no API path needed
	}

	// Parse the display path into segments
	segments := ParseHierarchicalPath(displayPath)
	if len(segments) == 0 {
		return "", nil, true
	}

	// Walk the path, resolving each segment
	currentParent := ""
	var lastItem *CachedItem

	for i, segment := range segments {
		item, found := c.FindItemByTitle(currentParent, service, segment)
		if !found {
			return "", nil, false
		}
		lastItem = item

		// Build the display path for the next level
		if i < len(segments)-1 {
			if currentParent == "" {
				currentParent = EscapePathSegment(segment)
			} else {
				currentParent = currentParent + "/" + EscapePathSegment(segment)
			}
		}
	}

	if lastItem != nil {
		return lastItem.Path, lastItem, true
	}
	return "", nil, false
}

// ============================================
// Cache Command
// ============================================

// cacheCmd represents the cache command.
var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage browse cache for tab completion",
	Long: `Manage the browse cache used for tab completion.

The cache stores browse results to speed up tab completion.
Cache location: ~/.cache/kefw2/`,
}

// cacheClearCmd clears the cache.
var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all cached browse data",
	Run: func(_ *cobra.Command, _ []string) {
		if browseCache == nil {
			errorPrinter.Println("Cache not initialized")
			return
		}
		if err := browseCache.Clear(); err != nil && !os.IsNotExist(err) {
			errorPrinter.Printf("Failed to clear cache: %v\n", err)
			return
		}
		taskConpletedPrinter.Println("Cache cleared")
	},
}

// cacheStatusCmd shows cache status.
var cacheStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show cache statistics",
	Run: func(_ *cobra.Command, _ []string) {
		// Get cache directory
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			cacheDir = os.TempDir()
		}
		cacheDir = filepath.Join(cacheDir, "kefw2")

		headerPrinter.Println("API Response Cache:")
		rowsCachePath := filepath.Join(cacheDir, "rows_cache.json")
		if info, err := os.Stat(rowsCachePath); err == nil {
			// Read and parse the cache file to count entries
			data, readErr := os.ReadFile(rowsCachePath) //nolint:gosec // Path is constructed from user's cache dir
			var entryCount int
			var oldestAge time.Duration
			if readErr == nil {
				var entries map[string]json.RawMessage
				if json.Unmarshal(data, &entries) == nil {
					entryCount = len(entries)
					// Try to find oldest entry
					type entryWithTime struct {
						FetchedAt time.Time `json:"fetched_at"`
					}
					var oldest time.Time
					for _, raw := range entries {
						var e entryWithTime
						if json.Unmarshal(raw, &e) == nil && !e.FetchedAt.IsZero() {
							if oldest.IsZero() || e.FetchedAt.Before(oldest) {
								oldest = e.FetchedAt
							}
						}
					}
					if !oldest.IsZero() {
						oldestAge = time.Since(oldest)
					}
				}
			}
			contentPrinter.Printf("  Location: %s\n", rowsCachePath)
			contentPrinter.Printf("  Entries: %d\n", entryCount)
			contentPrinter.Printf("  Size: %s\n", formatBytes(info.Size()))
			if oldestAge > 0 {
				contentPrinter.Printf("  Oldest: %v ago\n", oldestAge.Round(time.Second))
			}
		} else {
			contentPrinter.Println("  No cache file found")
		}

		// Show TTL settings from config
		if browseCache != nil {
			contentPrinter.Println("\n  TTL Settings:")
			contentPrinter.Printf("    Radio: %v\n", browseCache.GetTTL("radio"))
			contentPrinter.Printf("    Podcast: %v\n", browseCache.GetTTL("podcast"))
			contentPrinter.Printf("    UPnP: %v\n", browseCache.GetTTL("upnp"))
		}

		// Show UPnP track index status
		headerPrinter.Println("\nUPnP Track Index:")
		index, err := kefw2.LoadTrackIndex()
		if err != nil || index == nil {
			contentPrinter.Println("  No index found")
			contentPrinter.Println("  Run 'kefw2 upnp index --rebuild' to create one")
		} else {
			contentPrinter.Printf("  Server: %s\n", index.ServerName)
			if index.ContainerName != "" {
				contentPrinter.Printf("  Container: %s\n", index.ContainerName)
			}
			contentPrinter.Printf("  Tracks: %d\n", index.TrackCount)
			contentPrinter.Printf("  Age: %v\n", time.Since(index.IndexedAt).Round(time.Second))
			contentPrinter.Printf("  Location: %s\n", kefw2.TrackIndexPath())
		}
	},
}

// formatBytes formats bytes as human-readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d bytes", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cacheStatusCmd)
}
