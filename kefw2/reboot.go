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
package kefw2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// ScheduledReboot describes the speaker's automatic-reboot schedule.
//
// DayOfWeek encoding has not yet been confirmed against the KEF Connect app.
// Observed values are in the range 0-6; the most likely encodings are
// 0=Sunday..6=Saturday or 1=Monday..7=Sunday. Test against the speaker UI
// before relying on this field.
type ScheduledReboot struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	DayOfWeek int    `json:"day_of_week" yaml:"day_of_week"`
	Time      string `json:"time" yaml:"time"`
}

const (
	pathScheduledRebootEnabled   = "settings:/kef/scheduledReboot/enabled"
	pathScheduledRebootDayOfWeek = "settings:/kef/scheduledReboot/dayOfWeek"
	pathScheduledRebootTime      = "settings:/kef/scheduledReboot/time"
)

var rebootTimeRegexp = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// GetScheduledReboot returns the speaker's current scheduled-reboot configuration.
func (s *KEFSpeaker) GetScheduledReboot(ctx context.Context) (ScheduledReboot, error) {
	var sr ScheduledReboot

	enabled, err := parseTypedBool(s.getData(ctx, pathScheduledRebootEnabled))
	if err != nil {
		return sr, fmt.Errorf("failed to get scheduled reboot enabled: %w", err)
	}
	sr.Enabled = enabled

	day, err := parseTypedInt(s.getData(ctx, pathScheduledRebootDayOfWeek))
	if err != nil {
		return sr, fmt.Errorf("failed to get scheduled reboot day: %w", err)
	}
	sr.DayOfWeek = day

	t, err := parseTypedString(s.getData(ctx, pathScheduledRebootTime))
	if err != nil {
		return sr, fmt.Errorf("failed to get scheduled reboot time: %w", err)
	}
	sr.Time = t

	return sr, nil
}

// SetScheduledReboot updates the speaker's scheduled-reboot configuration.
// dayOfWeek must be in 0-6 and timeStr must match HH:MM (24-hour).
func (s *KEFSpeaker) SetScheduledReboot(ctx context.Context, sr ScheduledReboot) error {
	if sr.DayOfWeek < 0 || sr.DayOfWeek > 6 {
		return fmt.Errorf("dayOfWeek must be between 0 and 6, got %d", sr.DayOfWeek)
	}
	if !rebootTimeRegexp.MatchString(sr.Time) {
		return fmt.Errorf("time must be in HH:MM 24-hour format, got %q", sr.Time)
	}

	if err := s.setAuthenticatedTypedValue(ctx, pathScheduledRebootDayOfWeek, sr.DayOfWeek); err != nil {
		return fmt.Errorf("failed to set scheduled reboot day: %w", err)
	}
	if err := s.setAuthenticatedTypedValue(ctx, pathScheduledRebootTime, sr.Time); err != nil {
		return fmt.Errorf("failed to set scheduled reboot time: %w", err)
	}
	if err := s.setAuthenticatedTypedValue(ctx, pathScheduledRebootEnabled, sr.Enabled); err != nil {
		return fmt.Errorf("failed to set scheduled reboot enabled: %w", err)
	}
	return nil
}

// SetScheduledRebootEnabled toggles whether the scheduled reboot is active
// without changing the day or time.
func (s *KEFSpeaker) SetScheduledRebootEnabled(ctx context.Context, enabled bool) error {
	return s.setAuthenticatedTypedValue(ctx, pathScheduledRebootEnabled, enabled)
}

// setAuthenticatedTypedValue is the HMAC-authenticated (TLS:4430) counterpart
// of setTypedValue, required for writes under settings:/kef/scheduledReboot/*
// which the unauthenticated HTTP:80 setData endpoint rejects with HTTP 401.
func (s *KEFSpeaker) setAuthenticatedTypedValue(ctx context.Context, path string, value any) error {
	var typeName string
	var typeValue any

	switch v := value.(type) {
	case int:
		typeName = typeNameI32
		typeValue = v
	case int64:
		typeName = typeNameI64
		typeValue = v
	case string:
		typeName = typeNameString
		typeValue = v
	case bool:
		typeName = typeNameBool
		typeValue = v
	case TypeEncoder:
		var strVal string
		typeName, strVal = v.KEFTypeInfo()
		typeValue = strVal
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	jsonStr, err := json.Marshal(map[string]any{
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

	resp, raw, err := s.doAuthenticatedPOST(ctx, authPathSetData, reqBody)
	if err != nil {
		return fmt.Errorf("authenticated setData %s: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("authenticated setData %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// Reboot triggers an immediate reboot of the speaker.
//
// The endpoint lives on the TLS-only authenticated API (port 4430) and
// requires HMAC_SHA256 authentication; see kefw2/auth.go for the protocol
// details recovered from the KEF Connect Android app.
//
// Because the speaker tears down its network stack immediately after
// accepting the command, a connection reset / EOF after the request was
// sent is treated as a successful reboot trigger.
func (s *KEFSpeaker) Reboot(ctx context.Context) error {
	body := []byte(`{"path":"powermanager:goReboot","role":"activate","value":"{}"}`)

	resp, raw, err := s.doAuthenticatedPOST(ctx, authPathSetData, body)
	if err != nil {
		if isRebootDisconnect(err) {
			return nil
		}
		return fmt.Errorf("reboot request: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("reboot: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}

// isRebootDisconnect reports whether the error looks like the speaker
// dropping the connection mid-response because it's rebooting.
func isRebootDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "eof"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "use of closed network connection"):
		return true
	}
	return false
}
