package kefw2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// EventSubscription represents a path to subscribe to for events
type EventSubscription struct {
	Path string `json:"path"`
	Type string `json:"type"` // "itemWithValue", "item", or "rows"
}

// DefaultEventSubscriptions are the paths we subscribe to for common events
var DefaultEventSubscriptions = []EventSubscription{
	// Player events
	{Path: "player:volume", Type: "itemWithValue"},
	{Path: "player:player/data", Type: "itemWithValue"}, // Use itemWithValue to get state transitions
	{Path: "player:player/data/playTime", Type: "itemWithValue"},

	// Source and power
	{Path: "settings:/kef/play/physicalSource", Type: "itemWithValue"},
	{Path: "settings:/kef/host/speakerStatus", Type: "itemWithValue"},

	// Audio settings
	{Path: "settings:/mediaPlayer/mute", Type: "itemWithValue"},
	{Path: "settings:/mediaPlayer/playMode", Type: "itemWithValue"},
	{Path: "kef:eqProfile/v2", Type: "itemWithValue"},

	// Playlist
	{Path: "playlists:pq/getitems", Type: "rows"},

	// Network and system
	{Path: "network:info", Type: "itemWithValue"},
	{Path: "firmwareupdate:updateStatus", Type: "itemWithValue"},
	{Path: "notifications:/display/queue", Type: "rows"},

	// Bluetooth
	{Path: "bluetooth:state", Type: "itemWithValue"},
}

// EventClient manages event subscriptions and delivers events via channels
type EventClient struct {
	speaker       *KEFSpeaker
	queueID       string
	events        chan Event
	subscriptions []EventSubscription
	pollTimeout   int // seconds
	httpClient    *http.Client

	mu       sync.Mutex
	running  bool
	stopOnce sync.Once

	// Deduplication for player data events
	lastPlayerState string
	lastTrackTitle  string
	lastTrackArtist string
}

// EventClientOption is a functional option for configuring EventClient
type EventClientOption func(*EventClient)

// WithSubscriptions sets custom subscriptions (default: DefaultEventSubscriptions)
func WithSubscriptions(subs []EventSubscription) EventClientOption {
	return func(ec *EventClient) {
		ec.subscriptions = subs
	}
}

// WithPollTimeout sets the poll timeout in seconds (default: 5)
func WithPollTimeout(seconds int) EventClientOption {
	return func(ec *EventClient) {
		ec.pollTimeout = seconds
	}
}

// WithEventBufferSize sets the event channel buffer size (default: 100)
func WithEventBufferSize(size int) EventClientOption {
	return func(ec *EventClient) {
		ec.events = make(chan Event, size)
	}
}

// NewEventClient creates a new EventClient for receiving real-time events
func (s *KEFSpeaker) NewEventClient(opts ...EventClientOption) (*EventClient, error) {
	ec := &EventClient{
		speaker:       s,
		events:        make(chan Event, 100),
		subscriptions: DefaultEventSubscriptions,
		pollTimeout:   5,
		httpClient: &http.Client{
			Timeout: 15 * time.Second, // longer than poll timeout
		},
	}

	for _, opt := range opts {
		opt(ec)
	}

	// Register the event queue with the speaker
	queueID, err := ec.registerQueue()
	if err != nil {
		return nil, fmt.Errorf("failed to register event queue: %w", err)
	}
	ec.queueID = queueID

	return ec, nil
}

// Events returns a read-only channel for receiving events
func (ec *EventClient) Events() <-chan Event {
	return ec.events
}

// QueueID returns the queue ID assigned by the speaker
func (ec *EventClient) QueueID() string {
	return ec.queueID
}

// Start begins polling for events. It blocks until the context is cancelled
// or an unrecoverable error occurs. Events are sent to the Events() channel.
func (ec *EventClient) Start(ctx context.Context) error {
	ec.mu.Lock()
	if ec.running {
		ec.mu.Unlock()
		return fmt.Errorf("event client already running")
	}
	ec.running = true
	ec.mu.Unlock()

	defer func() {
		ec.mu.Lock()
		ec.running = false
		ec.mu.Unlock()
	}()

	return ec.pollLoop(ctx)
}

// Close stops the event client and closes the events channel
func (ec *EventClient) Close() error {
	ec.stopOnce.Do(func() {
		close(ec.events)
	})
	return nil
}

// registerQueue calls /api/event/modifyQueue to register a new event queue
func (ec *EventClient) registerQueue() (string, error) {
	subscribeJSON, err := json.Marshal(ec.subscriptions)
	if err != nil {
		return "", fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	params := url.Values{}
	params.Set("subscribe", string(subscribeJSON))
	params.Set("unsubscribe", "[]")
	params.Set("queueId", "")

	requestURL := fmt.Sprintf("http://%s/api/event/modifyQueue?%s", ec.speaker.IPAddress, params.Encode())

	resp, err := ec.httpClient.Get(requestURL)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Response is a quoted string like: "{uuid-here}"
	var queueID string
	if err := json.Unmarshal(body, &queueID); err != nil {
		return "", fmt.Errorf("failed to parse queue ID: %w (body: %s)", err, string(body))
	}

	if queueID == "" {
		return "", fmt.Errorf("empty queue ID returned")
	}

	return queueID, nil
}

// pollLoop continuously polls for events until context is cancelled
func (ec *EventClient) pollLoop(ctx context.Context) error {
	baseURL := fmt.Sprintf("http://%s/api/event/pollQueue", ec.speaker.IPAddress)
	consecutiveErrors := 0
	maxErrors := 5

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{}
		params.Set("queueId", ec.queueID)
		params.Set("timeout", fmt.Sprintf("%d", ec.pollTimeout))
		requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

		events, err := ec.poll(ctx, requestURL)
		if err != nil {
			consecutiveErrors++
			if consecutiveErrors >= maxErrors {
				return fmt.Errorf("too many consecutive errors: %w", err)
			}
			// Brief pause before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}

		consecutiveErrors = 0

		// Send events to channel
		for _, event := range events {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ec.events <- event:
			}
		}
	}
}

// poll makes a single poll request and returns parsed events
func (ec *EventClient) poll(ctx context.Context, requestURL string) ([]Event, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := ec.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse raw events
	var rawEvents []rawEvent
	if err := json.Unmarshal(body, &rawEvents); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to typed events
	events := make([]Event, 0, len(rawEvents))
	for _, raw := range rawEvents {
		if event := ec.parseRawEvent(raw); event != nil {
			events = append(events, event)
		}
	}

	return events, nil
}

// rawEvent represents the JSON structure from the speaker
type rawEvent struct {
	Path      string          `json:"path,omitempty"`
	ItemType  string          `json:"itemType,omitempty"`
	ItemValue json.RawMessage `json:"itemValue,omitempty"`

	RowsType       string     `json:"rowsType,omitempty"`
	RowsOldVersion int        `json:"rowsOldVersion,omitempty"`
	RowsVersion    int        `json:"rowsVersion,omitempty"`
	RowsEvents     []rowEvent `json:"rowsEvents,omitempty"`
}

type rowEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// typedValue represents a typed value from the KEF API
type typedValue struct {
	Type   string `json:"type"`
	I32    int    `json:"i32_,omitempty"`
	I64    int64  `json:"i64_,omitempty"`
	String string `json:"string_,omitempty"`
	Bool   bool   `json:"bool_,omitempty"`

	KefPhysicalSource string       `json:"kefPhysicalSource,omitempty"`
	KefSpeakerStatus  string       `json:"kefSpeakerStatus,omitempty"`
	KefEqProfileV2    *EQProfileV2 `json:"kefEqProfileV2,omitempty"`
	BluetoothState    *btState     `json:"bluetoothState,omitempty"`
}

type btState struct {
	State     string `json:"state,omitempty"`
	Connected bool   `json:"connected,omitempty"`
	Pairing   bool   `json:"pairing,omitempty"`
}

// parseTypedValue parses a json.RawMessage into a typedValue
func parseTypedValue(raw json.RawMessage) *typedValue {
	if len(raw) == 0 {
		return nil
	}
	var value typedValue
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return &value
}
