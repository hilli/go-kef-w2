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
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheConfig configures the rows cache behavior.
type CacheConfig struct {
	// Enabled controls whether caching is active (default: true)
	Enabled bool

	// TTL is the time-to-live for cached entries (default: 5 minutes)
	TTL time.Duration

	// PersistDir is the directory for disk persistence.
	// If empty, cache is memory-only.
	// If set to "auto", uses os.UserCacheDir()/kefw2
	PersistDir string
}

// DefaultCacheConfig returns the default cache configuration (memory-only).
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled: true,
		TTL:     24 * time.Hour,
	}
}

// DefaultDiskCacheConfig returns default config with disk persistence.
// Uses os.UserCacheDir()/kefw2 for storage.
func DefaultDiskCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:    true,
		TTL:        24 * time.Hour,
		PersistDir: "auto",
	}
}

// rowsCacheEntry represents a cached response with timestamp.
type rowsCacheEntry struct {
	Response  *RowsResponse `json:"response"`
	FetchedAt time.Time     `json:"fetched_at"`
}

// RowsCache caches RowsResponse data with TTL support.
// It supports both in-memory and disk-persisted caching.
type RowsCache struct {
	config     CacheConfig
	entries    map[string]*rowsCacheEntry
	mu         sync.RWMutex
	dirty      bool
	persistDir string // Resolved persist directory
}

// NewRowsCache creates a new cache with the given configuration.
func NewRowsCache(config CacheConfig) *RowsCache {
	cache := &RowsCache{
		config:  config,
		entries: make(map[string]*rowsCacheEntry),
	}

	// Resolve persist directory
	if config.PersistDir == "auto" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			cacheDir = os.TempDir()
		}
		cache.persistDir = filepath.Join(cacheDir, "kefw2")
	} else if config.PersistDir != "" {
		cache.persistDir = config.PersistDir
	}

	// Create cache directory if needed
	if cache.persistDir != "" {
		_ = os.MkdirAll(cache.persistDir, 0755)
	}

	// Load existing cache from disk
	if cache.persistDir != "" {
		_ = cache.Load()
	}

	return cache
}

// cacheFilePath returns the path to the cache file.
func (c *RowsCache) cacheFilePath() string {
	if c.persistDir == "" {
		return ""
	}
	return filepath.Join(c.persistDir, "rows_cache.json")
}

// Get retrieves a cached response if valid (not expired).
func (c *RowsCache) Get(key string) (*RowsResponse, bool) {
	if !c.config.Enabled {
		return nil, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// Check if expired
	if time.Since(entry.FetchedAt) > c.config.TTL {
		return nil, false
	}

	return entry.Response, true
}

// Set stores a response in the cache.
func (c *RowsCache) Set(key string, resp *RowsResponse) {
	if !c.config.Enabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &rowsCacheEntry{
		Response:  resp,
		FetchedAt: time.Now(),
	}
	c.dirty = true

	// Auto-save to disk if persistence is enabled
	if c.persistDir != "" {
		_ = c.saveLocked()
	}
}

// Clear removes all cached entries and deletes the cache file.
func (c *RowsCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*rowsCacheEntry)
	c.dirty = false

	// Remove cache file
	if c.persistDir != "" {
		return os.Remove(c.cacheFilePath())
	}
	return nil
}

// Load loads cache from disk.
func (c *RowsCache) Load() error {
	if c.persistDir == "" {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.cacheFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No cache file yet
		}
		return err
	}

	var entries map[string]*rowsCacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	// Filter out expired entries while loading
	now := time.Now()
	for key, entry := range entries {
		if now.Sub(entry.FetchedAt) > c.config.TTL {
			delete(entries, key)
		}
	}

	c.entries = entries
	return nil
}

// Save persists cache to disk.
func (c *RowsCache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

// saveLocked saves the cache (must be called with lock held).
func (c *RowsCache) saveLocked() error {
	if c.persistDir == "" {
		return nil
	}

	if !c.dirty {
		return nil
	}

	// Clean expired entries before saving
	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.FetchedAt) > c.config.TTL {
			delete(c.entries, key)
		}
	}

	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(c.cacheFilePath(), data, 0644); err != nil {
		return err
	}

	c.dirty = false
	return nil
}

// Stats returns cache statistics.
func (c *RowsCache) Stats() (entries int, size int64, location string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries = len(c.entries)
	location = c.persistDir

	// Get file size if persisted
	if c.persistDir != "" {
		if info, err := os.Stat(c.cacheFilePath()); err == nil {
			size = info.Size()
		}
	}

	return
}

// IsEnabled returns whether caching is enabled.
func (c *RowsCache) IsEnabled() bool {
	return c.config.Enabled
}

// TTL returns the configured TTL.
func (c *RowsCache) TTL() time.Duration {
	return c.config.TTL
}
