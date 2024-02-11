package kefw2

import "encoding/json"

type EQProfileV2 struct {
	SubwooferCount     int     `json:"subwooferCount"` // 0, 1, 2
	TrebleAmount       float32 `json:"trebleAmount"`
	DeskMode           bool    `json:"deskMode"`
	BassExtension      string  `json:"bassExtension"` // less, standard, more
	HighPassMode       bool    `json:"highPassMode"`
	AudioPolarity      string  `json:"audioPolarity"`
	IsExpertMode       bool    `json:"isExpertMode"`
	DeskModeSetting    int     `json:"deskModeSetting"`
	SubwooferPreset    string  `json:"subwooferPreset"`
	HighPassModeFreq   int     `json:"highPassModeFreq"`
	WallModeSetting    float32 `json:"wallModeSetting"`
	Balance            int     `json:"balance"`
	SubEnableStereo    bool    `json:"subEnableStereo"`
	SubwooferPolarity  string  `json:"subwooferPolarity"`
	SubwooferGain      int     `json:"subwooferGain"`
	IsKW1              bool    `json:"isKW1"`
	PhaseCorrection    bool    `json:"phaseCorrection"`
	WallMode           bool    `json:"wallMode"`
	ProfileId          string  `json:"profileId"`
	ProfileName        string  `json:"profileName"`
	SubOutLPFreq       float32 `json:"subOutLPFreq"`
	SubwooferOutHotfix bool    `json:"subwooferOutHotfix"`
	SubwooferOut       bool    `json:"subwooferOut"`
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
