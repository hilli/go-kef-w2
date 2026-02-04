package kefw2

import (
	"fmt"
	"strings"
)

// AirableServiceType represents a type of Airable service (radio, podcast, etc.)
type AirableServiceType string

const (
	// ServiceRadio represents the internet radio service.
	ServiceRadio AirableServiceType = "radios"
	// ServicePodcast represents the podcast/feeds service.
	ServicePodcast AirableServiceType = "feeds"
)

// serviceConfig holds configuration for each service type.
type serviceConfig struct {
	entryPoint  string // Initial path to discover the service
	urlPath     string // Path segment in the base URL (e.g., "/airable/radios")
	actionType  string // Type name used in action paths (e.g., "radio" vs "feed")
	serviceName string // Human-readable name for error messages
}

// serviceConfigs maps service types to their configurations.
var serviceConfigs = map[AirableServiceType]serviceConfig{
	ServiceRadio: {
		entryPoint:  "ui:/airableradios",
		urlPath:     "/airable/radios",
		actionType:  "radio",
		serviceName: "radio",
	},
	ServicePodcast: {
		entryPoint:  "airable:linkService_airable.feeds",
		urlPath:     "/airable/feeds",
		actionType:  "feed",
		serviceName: "podcast",
	},
}

// getServiceBaseURL returns the cached base URL for the given service.
func (a *AirableClient) getServiceBaseURL(service AirableServiceType) string {
	switch service {
	case ServiceRadio:
		return a.RadioBaseURL
	case ServicePodcast:
		return a.PodcastBaseURL
	default:
		return ""
	}
}

// setServiceBaseURL caches the base URL for the given service.
func (a *AirableClient) setServiceBaseURL(service AirableServiceType, url string) {
	switch service {
	case ServiceRadio:
		a.RadioBaseURL = url
	case ServicePodcast:
		a.PodcastBaseURL = url
	}
}

// ensureServiceBaseURL ensures the base URL for the given service is discovered.
// The API uses redirects that need to be followed until we get the final airable:https:// URL.
func (a *AirableClient) ensureServiceBaseURL(service AirableServiceType) error {
	// Already have the final URL
	baseURL := a.getServiceBaseURL(service)
	if baseURL != "" && strings.HasPrefix(baseURL, "airable:https://") {
		return nil
	}

	config, ok := serviceConfigs[service]
	if !ok {
		return fmt.Errorf("unknown service type: %s", service)
	}

	// Follow redirects starting from the entry point
	path := config.entryPoint
	for i := 0; i < 5; i++ { // Max 5 redirects to prevent infinite loops
		resp, err := a.GetRows(path, 0, 19)
		if err != nil {
			return fmt.Errorf("failed to discover %s base URL: %w", config.serviceName, err)
		}

		// No more redirects - we're at the final destination
		if resp.RowsRedirect == "" {
			break
		}

		path = resp.RowsRedirect

		// Found the final airable:https:// URL
		if strings.HasPrefix(path, "airable:https://") {
			a.setServiceBaseURL(service, path)
			return nil
		}
	}

	// If we get here without finding airable:https://, use whatever we have
	if a.getServiceBaseURL(service) == "" && path != config.entryPoint {
		a.setServiceBaseURL(service, path)
	}

	if a.getServiceBaseURL(service) == "" {
		return fmt.Errorf("could not discover %s base URL", config.serviceName)
	}

	return nil
}

// getServiceBaseURLPath extracts the base path for service API calls.
// Converts "airable:https://8448239770.airable.io/airable/radios" to "airable:https://8448239770.airable.io"
func (a *AirableClient) getServiceBaseURLPath(service AirableServiceType) (string, error) {
	if err := a.ensureServiceBaseURL(service); err != nil {
		return "", err
	}

	config := serviceConfigs[service]
	baseURL := a.getServiceBaseURL(service)

	// Extract base URL (remove service-specific suffix if present)
	if idx := strings.Index(baseURL, config.urlPath); idx != -1 {
		baseURL = baseURL[:idx]
	}
	return baseURL, nil
}

// getServiceEndpoint retrieves content from a service endpoint.
// The endpoint is appended to the service's base path.
func (a *AirableClient) getServiceEndpoint(service AirableServiceType, endpoint string) (*RowsResponse, error) {
	baseURL, err := a.getServiceBaseURLPath(service)
	if err != nil {
		return nil, err
	}
	config := serviceConfigs[service]
	return a.GetRows(fmt.Sprintf("%s%s/%s", baseURL, config.urlPath, endpoint), 0, 50)
}

// getAllServiceEndpoint retrieves all content from a service endpoint (paginated and cached).
func (a *AirableClient) getAllServiceEndpoint(service AirableServiceType, endpoint string) (*RowsResponse, error) {
	baseURL, err := a.getServiceBaseURLPath(service)
	if err != nil {
		return nil, err
	}
	config := serviceConfigs[service]
	return a.GetAllRows(fmt.Sprintf("%s%s/%s", baseURL, config.urlPath, endpoint))
}

// modifyFavorite adds or removes an item from the service's favorites.
func (a *AirableClient) modifyFavorite(service AirableServiceType, item *ContentItem, add bool) error {
	// Extract item ID
	itemID := extractItemID(item.ID)
	if itemID == "" {
		return fmt.Errorf("could not extract item ID from: %s", item.ID)
	}

	// Get the base URL path
	baseURLPath, err := a.getServiceBaseURLPath(service)
	if err != nil {
		return fmt.Errorf("failed to get %s base URL: %w", service, err)
	}

	config := serviceConfigs[service]

	// Determine action (insert or remove)
	action := "insert"
	if !add {
		action = "remove"
	}

	// Convert base URL path to action URL format
	// "airable:https://8448239770.airable.io" -> "airable:action:https://8448239770.airable.io"
	baseURL := strings.TrimPrefix(baseURLPath, "airable:")
	actionPath := fmt.Sprintf("airable:action:%s/actions/favorites/airable/%s/%s/%s", baseURL, config.actionType, itemID, action)

	payload := map[string]interface{}{
		"path":  actionPath,
		"role":  "activate",
		"value": true,
	}

	_, err = a.SetData(payload)
	if err != nil {
		actionName := "add"
		if !add {
			actionName = "remove"
		}
		return fmt.Errorf("failed to %s %s favorite: %w", actionName, config.serviceName, err)
	}

	return nil
}

// extractItemID extracts the ID from various Airable ID formats.
// Works for both radio stations and podcasts.
// Input formats:
//   - "airable://airable.radios/station/1022963300812989"
//   - "airable://airable/radio/1022963300812989"
//   - "airable://airable.feeds/podcast/12345"
//   - "airable://airable/feeds/12345"
//
// Output: the last non-keyword segment (e.g., "1022963300812989" or "12345").
func extractItemID(id string) string {
	// Split by "/" and find the last meaningful segment
	parts := strings.Split(id, "/")
	skipKeywords := map[string]bool{
		"podcast": true, "feeds": true, "station": true, "radio": true, "radios": true,
	}

	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part != "" && !skipKeywords[part] {
			return part
		}
	}
	return ""
}

// browseByDisplayPath browses using a display path (title-based) for any service.
// It walks the hierarchy to resolve titles to actual API paths.
func (a *AirableClient) browseByDisplayPath(service AirableServiceType, displayPath string) (*RowsResponse, error) {
	if err := a.ensureServiceBaseURL(service); err != nil {
		return nil, err
	}

	baseURL := a.getServiceBaseURL(service)

	// Empty path = top-level menu
	if displayPath == "" {
		return a.GetRows(baseURL, 0, 100)
	}

	// Parse the display path into segments
	segments := parseDisplayPath(displayPath)
	if len(segments) == 0 {
		return a.GetRows(baseURL, 0, 100)
	}

	// Walk the hierarchy, resolving each segment
	currentPath := baseURL

	for _, segment := range segments {
		// Get items at current level
		resp, err := a.GetRows(currentPath, 0, 100)
		if err != nil {
			return nil, fmt.Errorf("failed to browse %s: %w", currentPath, err)
		}

		// Find the item matching this segment title
		found := false
		for _, item := range resp.Rows {
			if item.Title == segment {
				currentPath = item.Path
				found = true
				break
			}
		}

		if !found {
			return nil, fmt.Errorf("item not found: %s", segment)
		}
	}

	// Return items at the resolved path
	return a.GetRows(currentPath, 0, 100)
}

// buildMediaRoles constructs the mediaRoles payload for playback.
// This is the shared implementation used by both radio and podcast playback.
func buildMediaRoles(item *ContentItem) map[string]interface{} {
	roles := map[string]interface{}{
		"containerPlayable": item.ContainerPlayable,
		"title":             item.Title,
		"path":              item.Path,
		"type":              item.Type,
	}

	// Handle type-specific defaults
	if item.Type == "" {
		roles["type"] = "audio"
	}

	if item.ContainerType != "" {
		roles["containerType"] = item.ContainerType
	} else {
		roles["containerType"] = "none"
	}

	if item.AudioType != "" {
		roles["audioType"] = item.AudioType
	}

	if item.Icon != "" {
		roles["icon"] = item.Icon
	}

	if item.ID != "" {
		roles["id"] = item.ID
	}

	if item.LongDescription != "" {
		roles["longDescription"] = item.LongDescription
	}

	// Add media data if available
	if item.MediaData != nil {
		mediaData := map[string]interface{}{}

		// Metadata
		metaData := map[string]interface{}{
			"serviceID": item.MediaData.MetaData.ServiceID,
		}
		if item.MediaData.MetaData.Live {
			metaData["live"] = true
		}
		if item.MediaData.MetaData.ContentPlayContextPath != "" {
			metaData["contentPlayContextPath"] = item.MediaData.MetaData.ContentPlayContextPath
		}
		if item.MediaData.MetaData.PrePlayPath != "" {
			metaData["prePlayPath"] = item.MediaData.MetaData.PrePlayPath
		}
		if item.MediaData.MetaData.MaximumRetryCount > 0 {
			metaData["maximumRetryCount"] = item.MediaData.MetaData.MaximumRetryCount
		}
		mediaData["metaData"] = metaData

		// Resources
		if len(item.MediaData.Resources) > 0 {
			resources := make([]map[string]interface{}, len(item.MediaData.Resources))
			for i, res := range item.MediaData.Resources {
				resources[i] = map[string]interface{}{
					"uri":      res.URI,
					"mimeType": res.MimeType,
				}
				if res.BitRate > 0 {
					resources[i]["bitRate"] = res.BitRate
				}
				if res.Codec != "" {
					resources[i]["codec"] = res.Codec
				}
				if res.Duration > 0 {
					resources[i]["duration"] = res.Duration
				}
			}
			mediaData["resources"] = resources
		}

		roles["mediaData"] = mediaData
	}

	// Add images if available
	if item.Images != nil && len(item.Images.Images) > 0 {
		images := make([]map[string]interface{}, len(item.Images.Images))
		for i, img := range item.Images.Images {
			images[i] = map[string]interface{}{
				"url":    img.URL,
				"width":  img.Width,
				"height": img.Height,
			}
		}
		roles["images"] = map[string]interface{}{"images": images}
	}

	// Add context if available
	if item.Context != nil {
		roles["context"] = map[string]interface{}{
			"path": item.Context.Path,
		}
	}

	return roles
}

// playItem plays an item (radio station or podcast episode) using the player:player/control pattern.
func (a *AirableClient) playItem(item *ContentItem) error {
	// First clear the playlist
	if err := a.ClearPlaylist(); err != nil {
		return fmt.Errorf("failed to clear playlist: %w", err)
	}

	// Build the mediaRoles payload
	mediaRoles := buildMediaRoles(item)

	// Send play command
	payload := map[string]interface{}{
		"path": "player:player/control",
		"role": "activate",
		"value": map[string]interface{}{
			"type":       "none",
			"control":    "play",
			"mediaRoles": mediaRoles,
		},
	}

	_, err := a.SetData(payload)
	return err
}

// getItemDetails fetches full details for an item (radio station or podcast).
func (a *AirableClient) getItemDetails(itemPath string) (*ContentItem, error) {
	resp, err := a.GetRows(itemPath, 0, 19)
	if err != nil {
		return nil, err
	}

	// The roles field often contains the item details
	if resp.Roles != nil {
		return resp.Roles, nil
	}

	// Or return the first row if available
	if len(resp.Rows) > 0 {
		return &resp.Rows[0], nil
	}

	return nil, fmt.Errorf("no details found for path: %s", itemPath)
}
