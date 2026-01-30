package kefw2

import (
	"encoding/json"
	"fmt"
)

// kefTypedValue represents the structure of a typed value from the KEF API.
// The API returns values in a format like: [{“type”: “i32_”, “i32_”: 42}].
type kefTypedValue struct {
	Type              string       `json:"type"`
	I32               *int         `json:"i32_,omitempty"`
	I64               *int64       `json:"i64_,omitempty"`
	String            *string      `json:"string_,omitempty"`
	Bool              *string      `json:"bool_,omitempty"` // API returns "true"/"false" as strings
	KefPhysicalSource *string      `json:"kefPhysicalSource,omitempty"`
	KefSpeakerStatus  *string      `json:"kefSpeakerStatus,omitempty"`
	KefCableMode      *string      `json:"kefCableMode,omitempty"`
	KefEqProfileV2    *EQProfileV2 `json:"kefEqProfileV2,omitempty"`
}

// parseTypedInt parses a KEF API integer response.
func parseTypedInt(data []byte, err error) (int, error) {
	if err != nil {
		return 0, err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return 0, fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	switch val.Type {
	case "i32_":
		if val.I32 == nil {
			return 0, fmt.Errorf("%w: missing i32_ value", ErrInvalidJSONResponse)
		}
		return *val.I32, nil
	case "i64_":
		if val.I64 == nil {
			return 0, fmt.Errorf("%w: missing i64_ value", ErrInvalidJSONResponse)
		}
		return int(*val.I64), nil
	default:
		return 0, fmt.Errorf("%w: expected int type, got %s", ErrInvalidJSONResponse, val.Type)
	}
}

// parseTypedString parses a KEF API string response.
func parseTypedString(data []byte, err error) (string, error) {
	if err != nil {
		return "", err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return "", fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	if val.Type != "string_" {
		return "", fmt.Errorf("%w: expected string_ type, got %s", ErrInvalidJSONResponse, val.Type)
	}

	if val.String == nil {
		return "", fmt.Errorf("%w: missing string_ value", ErrInvalidJSONResponse)
	}

	return *val.String, nil
}

// parseTypedBool parses a KEF API boolean response.
func parseTypedBool(data []byte, err error) (bool, error) {
	if err != nil {
		return false, err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return false, fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return false, fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	if val.Type != "bool_" {
		return false, fmt.Errorf("%w: expected bool_ type, got %s", ErrInvalidJSONResponse, val.Type)
	}

	if val.Bool == nil {
		return false, fmt.Errorf("%w: missing bool_ value", ErrInvalidJSONResponse)
	}

	// KEF API returns booleans as strings "true" or "false"
	return *val.Bool != "false", nil
}

// parseTypedSource parses a KEF API Source (kefPhysicalSource) response.
func parseTypedSource(data []byte, err error) (Source, error) {
	if err != nil {
		return "", err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return "", fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	if val.Type != "kefPhysicalSource" {
		return "", fmt.Errorf("%w: expected kefPhysicalSource type, got %s", ErrInvalidJSONResponse, val.Type)
	}

	if val.KefPhysicalSource == nil {
		return "", fmt.Errorf("%w: missing kefPhysicalSource value", ErrInvalidJSONResponse)
	}

	return Source(*val.KefPhysicalSource), nil
}

// parseTypedSpeakerStatus parses a KEF API SpeakerStatus (kefSpeakerStatus) response.
func parseTypedSpeakerStatus(data []byte, err error) (SpeakerStatus, error) {
	if err != nil {
		return "", err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return "", fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	if val.Type != "kefSpeakerStatus" {
		return "", fmt.Errorf("%w: expected kefSpeakerStatus type, got %s", ErrInvalidJSONResponse, val.Type)
	}

	if val.KefSpeakerStatus == nil {
		return "", fmt.Errorf("%w: missing kefSpeakerStatus value", ErrInvalidJSONResponse)
	}

	return SpeakerStatus(*val.KefSpeakerStatus), nil
}

// parseTypedCableMode parses a KEF API CableMode (kefCableMode) response.
func parseTypedCableMode(data []byte, err error) (CableMode, error) {
	if err != nil {
		return "", err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return "", fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	if val.Type != "kefCableMode" {
		return "", fmt.Errorf("%w: expected kefCableMode type, got %s", ErrInvalidJSONResponse, val.Type)
	}

	if val.KefCableMode == nil {
		return "", fmt.Errorf("%w: missing kefCableMode value", ErrInvalidJSONResponse)
	}

	return CableMode(*val.KefCableMode), nil
}

// parseTypedEQProfile parses a KEF API EQProfileV2 (kefEqProfileV2) response.
func parseTypedEQProfile(data []byte, err error) (EQProfileV2, error) {
	if err != nil {
		return EQProfileV2{}, err
	}

	var values []kefTypedValue
	if err := json.Unmarshal(data, &values); err != nil {
		return EQProfileV2{}, fmt.Errorf("%w: %w", ErrInvalidJSONResponse, err)
	}

	if len(values) == 0 {
		return EQProfileV2{}, fmt.Errorf("%w: empty response array", ErrInvalidJSONResponse)
	}

	val := values[0]
	if val.Type != "kefEqProfileV2" {
		return EQProfileV2{}, fmt.Errorf("%w: expected kefEqProfileV2 type, got %s", ErrInvalidJSONResponse, val.Type)
	}

	if val.KefEqProfileV2 == nil {
		return EQProfileV2{}, fmt.Errorf("%w: missing kefEqProfileV2 value", ErrInvalidJSONResponse)
	}

	return *val.KefEqProfileV2, nil
}
