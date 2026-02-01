package kefw2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AirableClient handles communication with the Airable API on KEF speakers.
// It provides access to streaming services including Radio, Podcasts, UPnP media servers, and Tidal.
type AirableClient struct {
	Speaker    *KEFSpeaker
	HTTPClient *http.Client
	QueueID    string
	Cache      *RowsCache // Optional cache for rows data

	// Dynamically discovered base URLs for services
	RadioBaseURL   string // e.g., "airable:https://8448239770.airable.io"
	PodcastBaseURL string
}

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

// ContentItem represents a content item from the Airable API.
// Used across all services (radio, podcast, UPnP, Tidal).
type ContentItem struct {
	Title             string     `json:"title"`
	Type              string     `json:"type"` // "container", "audio", "query", etc.
	ID                string     `json:"id"`
	Path              string     `json:"path"`
	ContainerType     string     `json:"containerType,omitempty"`
	ContainerPlayable bool       `json:"containerPlayable,omitempty"`
	AudioType         string     `json:"audioType,omitempty"` // "audioBroadcast" for radio
	Icon              string     `json:"icon,omitempty"`
	LongDescription   string     `json:"longDescription,omitempty"`
	MediaData         *MediaData `json:"mediaData,omitempty"`
	Images            *ImageSet  `json:"images,omitempty"`
	Context           *Context   `json:"context,omitempty"`
	Value             *Value     `json:"value,omitempty"`
}

// MediaData contains audio resource information and metadata.
type MediaData struct {
	MetaData  MediaMetaData   `json:"metaData,omitempty"`
	Resources []MediaResource `json:"resources,omitempty"`
}

// MediaMetaData contains metadata about the media.
type MediaMetaData struct {
	Artist                 string `json:"artist,omitempty"`
	Album                  string `json:"album,omitempty"`
	Genre                  string `json:"genre,omitempty"`
	Composer               string `json:"composer,omitempty"`
	ServiceID              string `json:"serviceID,omitempty"` // "airableRadios", "UPnP", etc.
	Live                   bool   `json:"live,omitempty"`
	PlayLogicPath          string `json:"playLogicPath,omitempty"`
	ContentPlayContextPath string `json:"contentPlayContextPath,omitempty"`
	PrePlayPath            string `json:"prePlayPath,omitempty"`
	MaximumRetryCount      int    `json:"maximumRetryCount,omitempty"`
}

// MediaResource represents a single audio stream resource.
type MediaResource struct {
	URI             string `json:"uri"`
	MimeType        string `json:"mimeType"`
	BitRate         int    `json:"bitRate,omitempty"`
	Codec           string `json:"codec,omitempty"`
	Duration        int    `json:"duration,omitempty"`        // milliseconds (UPnP)
	SampleFrequency int    `json:"sampleFrequency,omitempty"` // Hz (UPnP)
}

// ImageSet contains multiple image resolutions.
type ImageSet struct {
	Images []Image `json:"images"`
}

// Image represents a single image with dimensions.
type Image struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Context contains context menu information.
type Context struct {
	Path          string `json:"path,omitempty"`
	Title         string `json:"title,omitempty"`
	Type          string `json:"type,omitempty"`
	ContainerType string `json:"containerType,omitempty"`
}

// Value represents a typed value (used in UPnP responses).
type Value struct {
	Type string `json:"type,omitempty"`
	I32  int    `json:"i32_,omitempty"`
}

// RowsResponse represents a response from the getRows API.
type RowsResponse struct {
	RowsCount    int           `json:"rowsCount"`
	RowsVersion  int           `json:"rowsVersion,omitempty"`
	Rows         []ContentItem `json:"rows"`
	RowsRedirect string        `json:"rowsRedirect,omitempty"`
	Roles        *ContentItem  `json:"roles,omitempty"`
}

// SetDataResponse represents a response from the setData API.
type SetDataResponse struct {
	XClass string `json:"xclass,omitempty"`
	Result struct {
		Path string `json:"path,omitempty"`
	} `json:"result,omitempty"`
}

// NewAirableClient creates a new client for the Airable API.
func NewAirableClient(speaker *KEFSpeaker, opts ...AirableClientOption) *AirableClient {
	client := &AirableClient{
		Speaker: speaker,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		QueueID: "{8b2c3eca-b4ce-4c6f-9f63-fc29928150f4}", // Default queue ID
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

// SetQueueID sets a custom queue ID for event polling.
func (a *AirableClient) SetQueueID(queueID string) {
	a.QueueID = queueID
}

// GetRows retrieves content rows from the specified path.
func (a *AirableClient) GetRows(path string, from, to int) (*RowsResponse, error) {
	requestURL := fmt.Sprintf("http://%s/api/getRows?path=%s&roles=@all&from=%d&to=%d",
		a.Speaker.IPAddress, url.QueryEscape(path), from, to)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var rowsResp RowsResponse
	if err := json.Unmarshal(body, &rowsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &rowsResp, nil
}

// GetAllRows retrieves all content rows from the specified path by paginating.
// It dynamically determines batch size based on what the API returns (typically ~30).
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
	// Request 100 but API typically returns ~30 max
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

	// Use actual returned count as batch size (API limits to ~30)
	batchSize := len(resp.Rows)
	if batchSize == 0 {
		batchSize = 30 // Fallback
	}

	// Paginate to get remaining rows
	allRows := resp.Rows
	for from := batchSize; from < resp.RowsCount; from += batchSize {
		to := from + batchSize
		batch, err := a.GetRows(path, from, to)
		if err != nil {
			break // Return what we have so far
		}
		if len(batch.Rows) == 0 {
			break // No more rows
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

// SetData sends a setData request to the speaker API.
func (a *AirableClient) SetData(payload interface{}) (*SetDataResponse, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	requestURL := fmt.Sprintf("http://%s/api/setData", a.Speaker.IPAddress)
	req, err := http.NewRequest(http.MethodPost, requestURL, bytes.NewBuffer(payloadJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var setDataResp SetDataResponse
	if err := json.Unmarshal(body, &setDataResp); err != nil {
		// Some setData calls return empty or non-JSON responses
		return &SetDataResponse{}, nil
	}

	return &setDataResp, nil
}

// GetData retrieves data from the specified path using the getData API.
// Returns the raw JSON response body.
func (a *AirableClient) GetData(path string) ([]byte, error) {
	requestURL := fmt.Sprintf("http://%s/api/getData?path=%s&roles=value",
		a.Speaker.IPAddress, url.QueryEscape(path))

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get data: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return body, nil
}

// ClearPlaylist clears the current playlist.
func (a *AirableClient) ClearPlaylist() error {
	payload := map[string]interface{}{
		"path": "playlists:pl/clear",
		"role": "activate",
		"value": map[string]interface{}{
			"plid": 0,
		},
	}
	_, err := a.SetData(payload)
	return err
}

// PollEvents polls for events from the speaker.
func (a *AirableClient) PollEvents(timeout int) ([]interface{}, error) {
	requestURL := fmt.Sprintf("http://%s/api/event/pollQueue?queueId=%s&timeout=%d",
		a.Speaker.IPAddress, url.QueryEscape(a.QueueID), timeout)

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to poll events: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read events: %w", err)
	}

	var events []interface{}
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("failed to parse events: %w", err)
	}

	return events, nil
}

// GetBestImage returns the URL of the best quality image available.
// Returns empty string if no images are available.
func (item *ContentItem) GetBestImage() string {
	if item.Images == nil || len(item.Images.Images) == 0 {
		return item.Icon
	}

	var best Image
	for _, img := range item.Images.Images {
		if img.Width > best.Width {
			best = img
		}
	}
	if best.URL != "" {
		return best.URL
	}
	return item.Icon
}

// GetThumbnail returns the URL of a small thumbnail image.
// Prefers images around 128px, falls back to icon or best available.
func (item *ContentItem) GetThumbnail() string {
	if item.Images == nil || len(item.Images.Images) == 0 {
		return item.Icon
	}

	// Look for an image close to 128px
	var best Image
	bestDiff := 10000
	for _, img := range item.Images.Images {
		diff := abs(img.Width - 128)
		if diff < bestDiff {
			bestDiff = diff
			best = img
		}
	}
	if best.URL != "" {
		return best.URL
	}
	return item.Icon
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ========================================
// Tidal Methods (kept for future use)
// ========================================

// GetTidalMenu returns the top-level Tidal menu.
func (a *AirableClient) GetTidalMenu() (*RowsResponse, error) {
	return a.GetRows("ui:/airabletidal", 0, 19)
}

// SearchTidal performs a search on Tidal using form-based submission.
func (a *AirableClient) SearchTidal(query string) (*RowsResponse, error) {
	// Set the search query
	queryPayload := map[string]interface{}{
		"path": "airable:form7:q",
		"role": "value",
		"value": map[string]string{
			"type":    "string_",
			"string_": query,
		},
	}
	if _, err := a.SetData(queryPayload); err != nil {
		return nil, fmt.Errorf("failed to set search query: %w", err)
	}

	// Submit the search
	submitPayload := map[string]interface{}{
		"path":  "airable:form7:submit",
		"role":  "activate",
		"value": true,
	}
	resp, err := a.SetData(submitPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to submit search: %w", err)
	}

	// Get the search results
	if resp.Result.Path != "" {
		return a.GetRows(resp.Result.Path, 0, 23)
	}

	return nil, fmt.Errorf("no search results path returned")
}

// parseDisplayPath splits a display path into segments, handling %2F escapes.
// Used by both radio and podcast browsing to handle paths like "by Genre/Jazz"
// where names containing "/" are escaped as "%2F".
func parseDisplayPath(path string) []string {
	if path == "" {
		return nil
	}

	// Use a placeholder for escaped slashes, split, then unescape
	const placeholder = "\x00"
	escaped := strings.ReplaceAll(path, "%2F", placeholder)
	segments := strings.Split(escaped, "/")

	// Unescape and clean up
	result := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.ReplaceAll(seg, placeholder, "/")
		if seg != "" {
			result = append(result, seg)
		}
	}
	return result
}
