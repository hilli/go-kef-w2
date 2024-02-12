package kefw2

import "encoding/json"

type EQProfileV2 struct {
	AudioPolarity      string  `json:"audioPolarity"`
	Balance            int     `json:"balance"`
	BassExtension      string  `json:"bassExtension"` // less, standard, more
	DeskMode           bool    `json:"deskMode"`
	DeskModeSetting    int     `json:"deskModeSetting"`
	HighPassMode       bool    `json:"highPassMode"`
	HighPassModeFreq   int     `json:"highPassModeFreq"`
	IsExpertMode       bool    `json:"isExpertMode"`
	IsKW1              bool    `json:"isKW1"`
	PhaseCorrection    bool    `json:"phaseCorrection"`
	ProfileId          string  `json:"profileId"`
	ProfileName        string  `json:"profileName"`
	SubEnableStereo    bool    `json:"subEnableStereo"`
	SubOutLPFreq       float32 `json:"subOutLPFreq"`
	SubwooferCount     int     `json:"subwooferCount"` // 0, 1, 2
	SubwooferGain      int     `json:"subwooferGain"`
	SubwooferOut       bool    `json:"subwooferOut"`
	SubwooferOutHotfix bool    `json:"subwooferOutHotfix"`
	SubwooferPolarity  string  `json:"subwooferPolarity"`
	SubwooferPreset    string  `json:"subwooferPreset"`
	TrebleAmount       float32 `json:"trebleAmount"`
	WallMode           bool    `json:"wallMode"`
	WallModeSetting    float32 `json:"wallModeSetting"`
}

// GetEQProfileV2 returns the current EQProfileV2 for the speaker
// EQ Profiles are connected to the selected source
func (s *KEFSpeaker) GetEQProfileV2() (EQProfileV2, error) {
	eqProfile, err := JSONUnmarshalValue(s.getData("kef:eqProfile/v2"))
	return eqProfile.(EQProfileV2), err
}

// String dumps a json EQProfileV2
func (e EQProfileV2) String() string {
	profile, _ := json.MarshalIndent(e, "", "  ")
	return string(profile)
}
