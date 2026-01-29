package kefw2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default HTTP configuration values
const (
	DefaultTimeout = 2 * time.Second
	DefaultBaseURL = "http://%s/api"
)

// Common errors
var (
	ErrConnectionRefused = errors.New("connection refused")
	ErrConnectionTimeout = errors.New("connection timed out")
	ErrHostNotFound      = errors.New("host not found")
)

// KEFPostRequest represents the JSON structure for POST requests to the KEF API.
type KEFPostRequest struct {
	Path  string           `json:"path"`
	Roles string           `json:"roles"`
	Value *json.RawMessage `json:"value"`
}

// httpClient returns the speaker's HTTP client, creating one if necessary.
func (s *KEFSpeaker) httpClient() *http.Client {
	if s.client == nil {
		timeout := s.timeout
		if timeout == 0 {
			timeout = DefaultTimeout
		}
		s.client = &http.Client{Timeout: timeout}
	}
	return s.client
}

// handleConnectionError transforms network errors into user-friendly error messages.
func (s *KEFSpeaker) handleConnectionError(err error) error {
	if err == nil {
		return nil
	}

	// Check for timeout errors using errors.As (modern Go pattern)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: speaker at %s is not responding", ErrConnectionTimeout, s.IPAddress)
	}

	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "connection refused"):
		return fmt.Errorf("%w: speaker at %s - ensure it is powered on", ErrConnectionRefused, s.IPAddress)
	case strings.Contains(errStr, "timeout"):
		return fmt.Errorf("%w: speaker at %s is not responding", ErrConnectionTimeout, s.IPAddress)
	case strings.Contains(errStr, "no such host"):
		return fmt.Errorf("%w: %s - check the IP address", ErrHostNotFound, s.IPAddress)
	}

	return fmt.Errorf("connection error to %s: %w", s.IPAddress, err)
}

// requestConfig holds configuration for an HTTP request.
type requestConfig struct {
	method  string
	path    string
	params  url.Values
	body    []byte
	timeout time.Duration
}

// doRequest performs an HTTP request with the given configuration.
func (s *KEFSpeaker) doRequest(ctx context.Context, cfg requestConfig) ([]byte, error) {
	baseURL := fmt.Sprintf(DefaultBaseURL, s.IPAddress)
	requestURL := fmt.Sprintf("%s/%s", baseURL, cfg.path)

	var bodyReader io.Reader
	if cfg.body != nil {
		bodyReader = bytes.NewReader(cfg.body)
	}

	req, err := http.NewRequestWithContext(ctx, cfg.method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	if cfg.params != nil {
		req.URL.RawQuery = cfg.params.Encode()
	}

	client := s.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, s.handleConnectionError(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.Debug("HTTP request failed",
			"status", resp.StatusCode,
			"url", requestURL,
			"response", string(body),
		)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// getData fetches data from the speaker API for a given path.
func (s *KEFSpeaker) getData(path string) ([]byte, error) {
	params := url.Values{}
	params.Set("path", path)
	params.Set("roles", "value")

	return s.doRequest(context.Background(), requestConfig{
		method: http.MethodGet,
		path:   "getData",
		params: params,
	})
}

// getAllData fetches all data (including metadata) for a given path.
func (s *KEFSpeaker) getAllData(path string) ([]byte, error) {
	params := url.Values{}
	params.Set("path", path)
	params.Set("roles", "@all")

	return s.doRequest(context.Background(), requestConfig{
		method: http.MethodGet,
		path:   "getData",
		params: params,
	})
}

// getRows fetches row data from the speaker API.
func (s *KEFSpeaker) getRows(path string, extraParams map[string]string) ([]byte, error) {
	params := url.Values{}
	params.Set("path", path)
	for key, value := range extraParams {
		params.Set(key, value)
	}

	return s.doRequest(context.Background(), requestConfig{
		method: http.MethodGet,
		path:   "getRows",
		params: params,
	})
}

// setActivate sends an activate command to the speaker.
func (s *KEFSpeaker) setActivate(path, item, value string) error {
	jsonStr, err := json.Marshal(map[string]string{item: value})
	if err != nil {
		return fmt.Errorf("marshaling value: %w", err)
	}
	rawValue := json.RawMessage(jsonStr)

	reqBody, err := json.Marshal(KEFPostRequest{
		Path:  path,
		Roles: "activate",
		Value: &rawValue,
	})
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	_, err = s.doRequest(context.Background(), requestConfig{
		method: http.MethodPost,
		path:   "setData",
		body:   reqBody,
	})
	return err
}

// TypeEncoder interface for types that can encode themselves for the KEF API.
type TypeEncoder interface {
	KEFTypeInfo() (typeName string, value string)
}

// KEFTypeInfo implements TypeEncoder for Source.
func (s Source) KEFTypeInfo() (string, string) {
	return "kefPhysicalSource", string(s)
}

// KEFTypeInfo implements TypeEncoder for SpeakerStatus.
func (s SpeakerStatus) KEFTypeInfo() (string, string) {
	return "kefSpeakerStatus", fmt.Sprintf("%q", string(s))
}

// KEFTypeInfo implements TypeEncoder for CableMode.
func (c CableMode) KEFTypeInfo() (string, string) {
	return "kefCableMode", fmt.Sprintf("%q", string(c))
}

// setTypedValue sets a typed value on the speaker.
func (s *KEFSpeaker) setTypedValue(path string, value any) error {
	var typeName, typeValue string

	switch v := value.(type) {
	case int:
		typeName = "i32_"
		typeValue = fmt.Sprintf("%d", v)
	case string:
		typeName = "string_"
		typeValue = fmt.Sprintf("%q", v)
	case bool:
		typeName = "bool_"
		typeValue = fmt.Sprintf("%t", v)
	case TypeEncoder:
		typeName, typeValue = v.KEFTypeInfo()
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	// Build the JSON value
	jsonStr, err := json.Marshal(map[string]string{
		"type":   typeName,
		typeName: typeValue,
	})
	if err != nil {
		return fmt.Errorf("marshaling value: %w", err)
	}
	rawValue := json.RawMessage(jsonStr)

	reqBody, err := json.Marshal(KEFPostRequest{
		Path:  path,
		Roles: "value",
		Value: &rawValue,
	})
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	slog.Debug("setTypedValue", "path", path, "body", string(reqBody))

	_, err = s.doRequest(context.Background(), requestConfig{
		method: http.MethodPost,
		path:   "setData",
		body:   reqBody,
	})
	return err
}
