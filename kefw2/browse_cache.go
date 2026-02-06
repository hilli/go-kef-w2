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

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BrowseCacheConfig configures the browse cache behavior.
type BrowseCacheConfig struct {
	// Enabled controls whether caching is active (default: true)
	Enabled bool

	// CacheDir is the directory for storing the cache file.
	// If empty or "auto", uses os.UserCacheDir()/kefw2
	CacheDir string

	// DefaultTTL is the default time-to-live for cached entries.
	// Used when no service-specific TTL is configured.
	DefaultTTL time.Duration

	// ServiceTTLs maps service names to their specific TTLs.
	// Supported keys: "radio", "podcast", "upnp"
	ServiceTTLs map[string]time.Duration

	// CleanupTTL is the maximum age for entries during cleanup (on save).
	// Entries older than this are removed. Default: 5 minutes.
	CleanupTTL time.Duration
}

// DefaultBrowseCacheConfig returns the default browse cache configuration.
func DefaultBrowseCacheConfig() BrowseCacheConfig {
	return BrowseCacheConfig{
		Enabled:    true,
		CacheDir:   "auto",
		DefaultTTL: 5 * time.Minute,
		ServiceTTLs: map[string]time.Duration{
			"radio":   5 * time.Minute,
			"podcast": 5 * time.Minute,
			"upnp":    1 * time.Minute,
		},
		CleanupTTL: 5 * time.Minute,
	}
}

// CachedItem represents a cached content item for path completion/navigation.
type CachedItem struct {
	Title       string `json:"title"`
	Path        string `json:"path"`         // Actual API path
	DisplayPath string `json:"display_path"` // Human-readable path for display
	Type        string `json:"type"`         // "container" or "playable"
	Description string `json:"description"`  // Optional description
}

// browseCacheEntry represents a cached response for a browse path.
type browseCacheEntry struct {
	Items     []CachedItem `json:"items"`
	FetchedAt time.Time    `json:"fetched_at"`
}

// BrowseCache provides caching for hierarchical path completion and navigation.
// It is thread-safe and supports optional disk persistence.
type BrowseCache struct {
	config   BrowseCacheConfig
	cacheDir string // Resolved cache directory
	entries  map[string]*browseCacheEntry
	mu       sync.RWMutex
	dirty    bool // Track if cache needs saving
}

const browseCacheFilename = "browse_cache.json"

// NewBrowseCache creates a new browse cache with the given configuration.
func NewBrowseCache(config BrowseCacheConfig) (*BrowseCache, error) {
	cache := &BrowseCache{
		config:  config,
		entries: make(map[string]*browseCacheEntry),
	}

	// Resolve cache directory
	if config.CacheDir == "" || config.CacheDir == "auto" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			cacheDir = os.TempDir()
		}
		cache.cacheDir = filepath.Join(cacheDir, "kefw2")
	} else {
		cache.cacheDir = config.CacheDir
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cache.cacheDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Load existing cache from disk
	_ = cache.Load() // Ignore error - start fresh if corrupted

	return cache, nil
}

// cacheFilePath returns the path to the cache file.
func (c *BrowseCache) cacheFilePath() string {
	return filepath.Join(c.cacheDir, browseCacheFilename)
}

// CacheDir returns the cache directory path.
func (c *BrowseCache) CacheDir() string {
	return c.cacheDir
}

// IsEnabled returns whether caching is enabled.
func (c *BrowseCache) IsEnabled() bool {
	return c.config.Enabled
}

// GetTTL returns the TTL for a given service type.
func (c *BrowseCache) GetTTL(service string) time.Duration {
	if ttl, ok := c.config.ServiceTTLs[service]; ok {
		return ttl
	}
	if c.config.DefaultTTL > 0 {
		return c.config.DefaultTTL
	}
	return 5 * time.Minute
}

// Get retrieves items from cache if valid (not expired).
func (c *BrowseCache) Get(path, service string) ([]CachedItem, bool) {
	if !c.config.Enabled {
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
func (c *BrowseCache) Set(path, service string, items []CachedItem) {
	if !c.config.Enabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := fmt.Sprintf("%s:%s", service, path)
	c.entries[key] = &browseCacheEntry{
		Items:     items,
		FetchedAt: time.Now(),
	}
	c.dirty = true
}

// Clear removes all cached entries and deletes the cache file.
func (c *BrowseCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*browseCacheEntry)
	c.dirty = false

	// Remove cache file
	err := os.Remove(c.cacheFilePath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Load loads cache from disk.
func (c *BrowseCache) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.cacheFilePath()) //nolint:gosec // Path is from config
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No cache file yet
		}
		return err
	}

	var entries map[string]*browseCacheEntry
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
	cleanupTTL := c.config.CleanupTTL
	if cleanupTTL <= 0 {
		cleanupTTL = 5 * time.Minute
	}

	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.FetchedAt) > cleanupTTL {
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
func (c *BrowseCache) ResolveDisplayPath(displayPath, service string) (apiPath string, item *CachedItem, ok bool) {
	if displayPath == "" {
		return "", nil, true // Empty path = top level
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
		foundItem, found := c.FindItemByTitle(currentParent, service, segment)
		if !found {
			return "", nil, false
		}
		lastItem = foundItem

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
// Path Parsing Helpers
// ============================================

// ParseHierarchicalPath splits a path into segments, handling escaped characters.
// For example: "Rock/Classic Rock" -> ["Rock", "Classic Rock"]
func ParseHierarchicalPath(path string) []string {
	if path == "" {
		return nil
	}

	var segments []string
	var current strings.Builder
	escaped := false

	for _, r := range path {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}

		switch r {
		case '\\':
			escaped = true
		case '/':
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	return segments
}

// EscapePathSegment escapes special characters in a path segment.
func EscapePathSegment(segment string) string {
	// Escape forward slashes and backslashes
	segment = strings.ReplaceAll(segment, "\\", "\\\\")
	segment = strings.ReplaceAll(segment, "/", "\\/")
	return segment
}

// UnescapePathSegment unescapes special characters in a path segment.
func UnescapePathSegment(segment string) string {
	segment = strings.ReplaceAll(segment, "\\/", "/")
	segment = strings.ReplaceAll(segment, "\\\\", "\\")
	return segment
}
