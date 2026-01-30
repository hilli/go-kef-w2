package kefw2

import (
	"context"
	"encoding/json"
)

// EQProfileV2 represents the equalizer and DSP settings for the speaker.
// These settings are specific to the current audio source and include
// bass extension, desk/wall mode compensation, subwoofer configuration, and more.
type EQProfileV2 struct {
	AudioPolarity      string  `json:"audioPolarity"`      // Audio polarity setting
	Balance            int     `json:"balance"`            // Left/right balance (-10 to +10)
	BassExtension      string  `json:"bassExtension"`      // Bass extension mode: "less", "standard", or "more"
	DeskMode           bool    `json:"deskMode"`           // Whether desk mode is enabled
	DeskModeSetting    int     `json:"deskModeSetting"`    // Desk mode compensation level
	HighPassMode       bool    `json:"highPassMode"`       // Whether high-pass filter is enabled (for use with subwoofer)
	HighPassModeFreq   int     `json:"highPassModeFreq"`   // High-pass filter frequency in Hz
	IsExpertMode       bool    `json:"isExpertMode"`       // Whether expert mode is enabled
	IsKW1              bool    `json:"isKW1"`              // Whether KW1 subwoofer mode is active
	PhaseCorrection    bool    `json:"phaseCorrection"`    // Whether phase correction is enabled
	ProfileID          string  `json:"profileId"`          // Unique identifier for this profile
	ProfileName        string  `json:"profileName"`        // User-friendly profile name
	SubEnableStereo    bool    `json:"subEnableStereo"`    // Whether stereo subwoofer output is enabled
	SubOutLPFreq       float32 `json:"subOutLPFreq"`       // Subwoofer low-pass filter frequency
	SubwooferCount     int     `json:"subwooferCount"`     // Number of connected subwoofers (0, 1, or 2)
	SubwooferGain      int     `json:"subwooferGain"`      // Subwoofer gain adjustment in dB
	SubwooferOut       bool    `json:"subwooferOut"`       // Whether subwoofer output is enabled
	SubwooferOutHotfix bool    `json:"subwooferOutHotfix"` // Subwoofer output hotfix (internal use)
	SubwooferPolarity  string  `json:"subwooferPolarity"`  // Subwoofer polarity setting
	SubwooferPreset    string  `json:"subwooferPreset"`    // Subwoofer preset configuration
	TrebleAmount       float32 `json:"trebleAmount"`       // Treble adjustment in dB
	WallMode           bool    `json:"wallMode"`           // Whether wall mode is enabled
	WallModeSetting    float32 `json:"wallModeSetting"`    // Wall mode compensation level
}

// GetEQProfileV2 returns the current EQ profile for the active audio source.
// Each source can have its own EQ profile settings.
func (s *KEFSpeaker) GetEQProfileV2(ctx context.Context) (EQProfileV2, error) {
	eqProfile, err := JSONUnmarshalValue(s.getData(ctx, "kef:eqProfile/v2"))
	return eqProfile.(EQProfileV2), err
}

// String returns the EQ profile as a formatted JSON string.
func (e EQProfileV2) String() string {
	profile, _ := json.MarshalIndent(e, "", "  ")
	return string(profile)
}
