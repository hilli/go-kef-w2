package kefw2

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Parsing errors.
var (
	ErrEmptyData     = errors.New("empty JSON data")
	ErrNoValue       = errors.New("no value in response")
	ErrUnknownType   = errors.New("unknown value type")
	ErrInvalidFormat = errors.New("invalid JSON format")
)

// jsonResponse represents a single item in the KEF API response array.
type jsonResponse struct {
	Type              string          `json:"type"`
	I32               json.Number     `json:"i32_,omitempty"`
	I64               json.Number     `json:"i64_,omitempty"`
	String            string          `json:"string_,omitempty"`
	Bool              string          `json:"bool_,omitempty"`
	KefPhysicalSource string          `json:"kefPhysicalSource,omitempty"`
	KefSpeakerStatus  string          `json:"kefSpeakerStatus,omitempty"`
	KefCableMode      string          `json:"kefCableMode,omitempty"`
	KefEqProfileV2    json.RawMessage `json:"kefEqProfileV2,omitempty"`
}

// parseResponse unmarshals the KEF API response and returns the first item.
func parseResponse(data []byte) (*jsonResponse, error) {
	if len(data) == 0 {
		return nil, ErrEmptyData
	}

	var responses []jsonResponse
	if err := json.Unmarshal(data, &responses); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFormat, err)
	}

	if len(responses) == 0 {
		return nil, ErrNoValue
	}

	return &responses[0], nil
}

// parseJSONString extracts a string value from a KEF API response.
func parseJSONString(data []byte) (string, error) {
	resp, err := parseResponse(data)
	if err != nil {
		return "", err
	}

	if resp.Type != "string_" && resp.String == "" {
		return "", fmt.Errorf("%w: expected string, got %s", ErrUnknownType, resp.Type)
	}

	return resp.String, nil
}

// parseJSONInt extracts an integer value from a KEF API response.
func parseJSONInt(data []byte) (int, error) {
	resp, err := parseResponse(data)
	if err != nil {
		return 0, err
	}

	// Try i32_ first, then i64_
	var numStr string
	switch resp.Type {
	case "i32_":
		numStr = string(resp.I32)
	case "i64_":
		numStr = string(resp.I64)
	default:
		// Fallback: try to get value from either field
		if resp.I32 != "" {
			numStr = string(resp.I32)
		} else if resp.I64 != "" {
			numStr = string(resp.I64)
		} else {
			return 0, fmt.Errorf("%w: expected integer, got %s", ErrUnknownType, resp.Type)
		}
	}

	if numStr == "" {
		return 0, nil
	}

	var val int64
	if err := json.Unmarshal([]byte(numStr), &val); err != nil {
		return 0, fmt.Errorf("parsing integer %q: %w", numStr, err)
	}

	return int(val), nil
}

// parseJSONBool extracts a boolean value from a KEF API response.
func parseJSONBool(data []byte) (bool, error) {
	resp, err := parseResponse(data)
	if err != nil {
		return false, err
	}

	if resp.Type != "bool_" {
		return false, fmt.Errorf("%w: expected bool, got %s", ErrUnknownType, resp.Type)
	}

	// The KEF API returns bools as string "true" or "false"
	return resp.Bool == "true", nil
}

// parseJSONValue extracts a typed value from a KEF API response.
// It returns the appropriate Go type based on the KEF type field.
func parseJSONValue(data []byte) (any, error) {
	resp, err := parseResponse(data)
	if err != nil {
		return nil, err
	}

	switch resp.Type {
	case "i32_":
		if resp.I32 == "" {
			return 0, nil
		}
		var val int
		if err := json.Unmarshal([]byte(resp.I32), &val); err != nil {
			return nil, fmt.Errorf("parsing i32: %w", err)
		}
		return val, nil

	case "i64_":
		if resp.I64 == "" {
			return int64(0), nil
		}
		var val int64
		if err := json.Unmarshal([]byte(resp.I64), &val); err != nil {
			return nil, fmt.Errorf("parsing i64: %w", err)
		}
		return val, nil

	case "string_":
		return resp.String, nil

	case "bool_":
		return resp.Bool == "true", nil

	case "kefPhysicalSource":
		return Source(resp.KefPhysicalSource), nil

	case "kefSpeakerStatus":
		return SpeakerStatus(resp.KefSpeakerStatus), nil

	case "kefCableMode":
		return CableMode(resp.KefCableMode), nil

	case "kefEqProfileV2":
		if len(resp.KefEqProfileV2) == 0 {
			return EQProfileV2{}, nil
		}
		var profile EQProfileV2
		if err := json.Unmarshal(resp.KefEqProfileV2, &profile); err != nil {
			return nil, fmt.Errorf("parsing EQ profile: %w", err)
		}
		return profile, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownType, resp.Type)
	}
}

// Deprecated: JSONStringValue is deprecated. Use parseJSONString instead.
// This function is kept for backward compatibility.
func JSONStringValue(data []byte, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return parseJSONString(data)
}

// Deprecated: JSONIntValue is deprecated. Use parseJSONInt instead.
func JSONIntValue(data []byte, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	return parseJSONInt(data)
}

// Deprecated: JSONUnmarshalValue is deprecated. Use parseJSONValue instead.
func JSONUnmarshalValue(data []byte, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return parseJSONValue(data)
}
