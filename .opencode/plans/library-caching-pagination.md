# Plan: Library-Level Caching with Pagination for Tab Completion

## Problem

Tab completion for radio commands (`radio hq`, `radio local`, etc.) only shows stations starting with A-C because:
1. The API only returns 50 stations per request
2. No pagination is performed to fetch all stations
3. Caching is currently in the CLI layer, not the library

## Solution

Move caching to the library layer (`kefw2/`) and add pagination support to fetch all stations. The CLI will use disk-based caching by default since it runs for short periods.

---

## Files to Create/Modify

### 1. CREATE: `kefw2/cache.go`

New file implementing `RowsCache` with hybrid memory+disk support.

```go
/*
Copyright 2023-2025 Jens Hilligsoe
... (MIT License header)
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
		TTL:     5 * time.Minute,
	}
}

// DefaultDiskCacheConfig returns default config with disk persistence.
// Uses os.UserCacheDir()/kefw2 for storage.
func DefaultDiskCacheConfig() CacheConfig {
	return CacheConfig{
		Enabled:    true,
		TTL:        5 * time.Minute,
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
```

---

### 2. MODIFY: `kefw2/airable.go`

Add Cache field to AirableClient, options pattern, and GetAllRows method.

#### Add to imports:
```go
// No new imports needed - already has what we need
```

#### Modify AirableClient struct (around line 15):
```go
// AirableClient handles communication with the Airable API on KEF speakers.
type AirableClient struct {
	Speaker    *KEFSpeaker
	HTTPClient *http.Client
	QueueID    string
	Cache      *RowsCache // Optional cache for rows data

	// Dynamically discovered base URLs for services
	RadioBaseURL   string
	PodcastBaseURL string
}
```

#### Add after AirableClient struct (before NewAirableClient):
```go
// AirableClientOption configures an AirableClient.
type AirableClientOption func(*AirableClient)

// WithCache enables caching with the given configuration.
func WithCache(config CacheConfig) AirableClientOption {
	return func(a *AirableClient) {
		a.Cache = NewRowsCache(config)
	}
}

// WithDefaultCache enables in-memory caching with default settings.
func WithDefaultCache() AirableClientOption {
	return func(a *AirableClient) {
		a.Cache = NewRowsCache(DefaultCacheConfig())
	}
}

// WithDiskCache enables disk-persisted caching with default settings.
// Uses os.UserCacheDir()/kefw2 for storage.
func WithDiskCache() AirableClientOption {
	return func(a *AirableClient) {
		a.Cache = NewRowsCache(DefaultDiskCacheConfig())
	}
}
```

#### Modify NewAirableClient (around line 116):
```go
// NewAirableClient creates a new client for the Airable API.
func NewAirableClient(speaker *KEFSpeaker, opts ...AirableClientOption) *AirableClient {
	client := &AirableClient{
		Speaker: speaker,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		QueueID: "{8b2c3eca-b4ce-4c6f-9f63-fc29928150f4}",
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}
```

#### Add after GetRows method (around line 159):
```go
// GetAllRows retrieves all content rows from the specified path by paginating.
// It fetches in batches of 100 and combines all results.
// Results are cached if caching is enabled.
func (a *AirableClient) GetAllRows(path string) (*RowsResponse, error) {
	cacheKey := "all:" + path

	// Check cache first
	if a.Cache != nil {
		if cached, ok := a.Cache.Get(cacheKey); ok {
			return cached, nil
		}
	}

	// First request to get initial batch and total count
	resp, err := a.GetRows(path, 0, 100)
	if err != nil {
		return nil, err
	}

	// If we got all rows, we're done
	if len(resp.Rows) >= resp.RowsCount {
		if a.Cache != nil {
			a.Cache.Set(cacheKey, resp)
		}
		return resp, nil
	}

	// Paginate to get remaining rows
	allRows := resp.Rows
	for from := 100; from < resp.RowsCount; from += 100 {
		to := from + 100
		batch, err := a.GetRows(path, from, to)
		if err != nil {
			break // Return what we have so far
		}
		allRows = append(allRows, batch.Rows...)
	}

	resp.Rows = allRows

	// Cache the complete result
	if a.Cache != nil {
		a.Cache.Set(cacheKey, resp)
	}

	return resp, nil
}
```

---

### 3. MODIFY: `kefw2/airable_radio.go`

Add 6 `*All()` methods after their corresponding methods.

#### After GetRadioFavorites (around line 114):
```go
// GetRadioFavoritesAll returns all favorite radio stations (paginated).
func (a *AirableClient) GetRadioFavoritesAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/favorites", baseURL))
}
```

#### After GetRadioLocal (around line 131):
```go
// GetRadioLocalAll returns all local radio stations (paginated).
func (a *AirableClient) GetRadioLocalAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/local", baseURL))
}
```

#### After GetRadioPopular (around line 140):
```go
// GetRadioPopularAll returns all popular radio stations (paginated).
func (a *AirableClient) GetRadioPopularAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/popular", baseURL))
}
```

#### After GetRadioTrending (around line 149):
```go
// GetRadioTrendingAll returns all trending radio stations (paginated).
func (a *AirableClient) GetRadioTrendingAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/trending", baseURL))
}
```

#### After GetRadioHQ (around line 167):
```go
// GetRadioHQAll returns all high quality radio stations (paginated).
func (a *AirableClient) GetRadioHQAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/hq", baseURL))
}
```

#### After GetRadioNew (around line 176):
```go
// GetRadioNewAll returns all new radio stations (paginated).
func (a *AirableClient) GetRadioNewAll() (*RowsResponse, error) {
	baseURL, err := a.getRadioBaseURLPath()
	if err != nil {
		return nil, err
	}
	return a.GetAllRows(fmt.Sprintf("%s/airable/radios/new", baseURL))
}
```

---

### 4. MODIFY: `cmd/kefw2/cmd/completion_helpers.go`

Update completion functions to use disk cache and `*All()` methods.

#### RadioLocalCompletion (around line 320):
Change:
```go
client := kefw2.NewAirableClient(currentSpeaker)
resp, err := client.GetRadioLocal()
```
To:
```go
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
resp, err := client.GetRadioLocalAll()
```

#### RadioFavoritesCompletion (around line 340):
Change:
```go
client := kefw2.NewAirableClient(currentSpeaker)
resp, err := client.GetRadioFavorites()
```
To:
```go
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
resp, err := client.GetRadioFavoritesAll()
```

#### RadioTrendingCompletion (around line 360):
Change:
```go
client := kefw2.NewAirableClient(currentSpeaker)
resp, err := client.GetRadioTrending()
```
To:
```go
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
resp, err := client.GetRadioTrendingAll()
```

#### RadioHQCompletion (around line 377):
Change:
```go
client := kefw2.NewAirableClient(currentSpeaker)
resp, err := client.GetRadioHQ()
```
To:
```go
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
resp, err := client.GetRadioHQAll()
```

#### RadioNewCompletion (around line 397):
Change:
```go
client := kefw2.NewAirableClient(currentSpeaker)
resp, err := client.GetRadioNew()
```
To:
```go
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
resp, err := client.GetRadioNewAll()
```

#### RadioPopularCompletion (around line 417):
Change:
```go
client := kefw2.NewAirableClient(currentSpeaker)
resp, err := client.GetRadioPopular()
```
To:
```go
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
resp, err := client.GetRadioPopularAll()
```

---

### 5. MODIFY: `cmd/kefw2/cmd/cache.go`

Simplify to delegate to library cache. The existing BrowseCache can be kept for hierarchical path completion, but we should add library cache awareness.

Add to cache status command (around line 280 in cacheStatusCmd):
```go
// Show library cache info if available
contentPrinter.Println("\nLibrary Cache (for completion):")
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
if client.Cache != nil {
	entries, size, location := client.Cache.Stats()
	contentPrinter.Printf("  Location: %s\n", location)
	contentPrinter.Printf("  Entries: %d\n", entries)
	contentPrinter.Printf("  Size: %d bytes\n", size)
	contentPrinter.Printf("  TTL: %s\n", client.Cache.TTL())
}
```

Add to cache clear command (around line 300 in cacheClearCmd):
```go
// Also clear library cache
client := kefw2.NewAirableClient(currentSpeaker, kefw2.WithDiskCache())
if client.Cache != nil {
	if err := client.Cache.Clear(); err != nil && !os.IsNotExist(err) {
		errorPrinter.Printf("Warning: could not clear library cache: %v\n", err)
	}
}
```

---

## Testing Plan

After implementation:

1. **Build**:
   ```bash
   task build
   ```

2. **Run tests**:
   ```bash
   go test ./kefw2/...
   ```

3. **Test completion with all letters**:
   ```bash
   ./bin/kefw2 __complete radio hq ""    # Should show all 271 stations
   ./bin/kefw2 __complete radio hq "H"   # Should show House Of Prog, etc.
   ./bin/kefw2 __complete radio hq "Z"   # Should show Z* stations
   ```

4. **Test cache persistence**:
   ```bash
   # First run - should fetch from API
   time ./bin/kefw2 __complete radio hq "" | wc -l
   
   # Second run - should be instant from cache
   time ./bin/kefw2 __complete radio hq "" | wc -l
   
   # Check cache file exists
   ls -la ~/Library/Caches/kefw2/rows_cache.json
   ```

5. **Test cache commands**:
   ```bash
   ./bin/kefw2 cache status
   ./bin/kefw2 cache clear
   ```

---

## Notes

- Cache key format: `"all:{path}"` - path only, shared across speakers
- TTL: 5 minutes default
- Disk location: `~/Library/Caches/kefw2/rows_cache.json` (macOS)
- Backward compatible: `NewAirableClient(speaker)` still works without caching
