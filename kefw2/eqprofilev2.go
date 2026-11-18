package kefw2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// EQProfileV2 represents the equalizer and DSP settings for the speaker.
// These settings are specific to the current audio source and include
// bass extension, desk/wall mode compensation, subwoofer configuration, and more.
type EQProfileV2 struct {
	AudioPolarity      string  `json:"audioPolarity"`                // Audio polarity setting
	Balance            int     `json:"balance"`                      // Left/right balance (-10 to +10)
	BassExtension      string  `json:"bassExtension"`                // Bass extension mode: "less", "standard", or "more"
	DeskMode           bool    `json:"deskMode"`                     // Whether desk mode is enabled
	DeskModeSetting    float64 `json:"deskModeSetting"`              // Desk mode compensation level (can be fractional, e.g., -3.5)
	HighPassMode       bool    `json:"highPassMode"`                 // Whether high-pass filter is enabled (for use with subwoofer)
	HighPassModeFreq   float64 `json:"highPassModeFreq"`             // High-pass filter frequency in Hz
	IsExpertMode       bool    `json:"isExpertMode"`                 // Whether expert mode is enabled
	IsKW1              bool    `json:"isKW1"`                        // Whether KW1 subwoofer mode is active
	PhaseCorrection    bool    `json:"phaseCorrection"`              // Whether phase correction is enabled
	ProfileID          string  `json:"profileId"`                    // Unique identifier for this profile
	ProfileName        string  `json:"profileName"`                  // User-friendly profile name
	SoundProfile       string  `json:"soundProfile"`                 // Sound profile preset (e.g., "default")
	SubEnableStereo    bool    `json:"subEnableStereo"`              // Whether stereo subwoofer output is enabled
	SubOutLPFreq       float32 `json:"subOutLPFreq"`                 // Subwoofer low-pass filter frequency
	SubwooferCount     int     `json:"subwooferCount"`               // Number of connected subwoofers (0, 1, or 2)
	SubwooferGain      int     `json:"subwooferGain"`                // Subwoofer gain adjustment in dB
	SubwooferOut       bool    `json:"subwooferOut"`                 // Whether subwoofer output is enabled
	SubwooferOutHotfix bool    `json:"subwooferOutHotfix,omitempty"` // Subwoofer output hotfix (internal use, may not be present)
	SubwooferPolarity  string  `json:"subwooferPolarity"`            // Subwoofer polarity setting
	SubwooferPreset    string  `json:"subwooferPreset"`              // Subwoofer preset configuration
	TrebleAmount       float32 `json:"trebleAmount"`                 // Treble adjustment in dB
	WallMode           bool    `json:"wallMode"`                     // Whether wall mode is enabled
	WallModeSetting    float32 `json:"wallModeSetting"`              // Wall mode compensation level
	WirelessSub        string  `json:"wirelessSub"`                  // Wireless subwoofer mode (e.g., "none")
}

// GetEQProfileV2 returns the current EQ profile for the active audio source.
// Each source can have its own EQ profile settings.
func (s *KEFSpeaker) GetEQProfileV2(ctx context.Context) (EQProfileV2, error) {
	return parseTypedEQProfile(s.getData(ctx, "kef:eqProfile/v2"))
}

// GetEQProfileV2Raw returns the active EQ profile without discarding unknown fields.
func (s *KEFSpeaker) GetEQProfileV2Raw(ctx context.Context) (json.RawMessage, error) {
	return parseRawEQProfileV2(s.getData(ctx, "kef:eqProfile/v2"))
}

// SetEQProfileV2 applies an EQ profile to the currently selected audio source.
func (s *KEFSpeaker) SetEQProfileV2(ctx context.Context, profile EQProfileV2) error {
	raw, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("marshaling EQ profile: %w", err)
	}
	return s.SetEQProfileV2Raw(ctx, raw)
}

// SetEQProfileV2Raw applies a raw profile object, including unknown firmware fields.
func (s *KEFSpeaker) SetEQProfileV2Raw(ctx context.Context, profile json.RawMessage) error {
	trimmed := bytes.TrimSpace(profile)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return fmt.Errorf("invalid EQ profile: expected a JSON object")
	}
	value, err := json.Marshal(struct {
		Type    string          `json:"type"`
		Profile json.RawMessage `json:"kefEqProfileV2"`
	}{"kefEqProfileV2", trimmed})
	if err != nil {
		return fmt.Errorf("marshaling EQ profile: %w", err)
	}

	body, err := json.Marshal(struct {
		Path  string          `json:"path"`
		Role  string          `json:"role"`
		Value json.RawMessage `json:"value"`
	}{"kef:eqProfile/v2", roleValue, value})
	if err != nil {
		return fmt.Errorf("marshaling EQ profile request: %w", err)
	}

	_, err = s.doRequest(ctx, requestConfig{
		method: http.MethodPost,
		path:   setDataEndpoint,
		body:   body,
	})
	return err
}

// String returns the EQ profile as a formatted JSON string.
func (e EQProfileV2) String() string {
	profile, _ := json.MarshalIndent(e, "", "  ")
	return string(profile)
}
