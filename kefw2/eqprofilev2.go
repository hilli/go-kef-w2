package kefw2

type EQProfileV2 struct {
	SubwooferCount    int     `json:"subwooferCount"` // 0, 1, 2
	TrebleAmount      float32 `json:"trebleAmount"`
	DeskMode          bool    `json:"deskMode"`
	BassExtension     string  `json:"bassExtension"` // less, standard, more
	HighPassMode      bool    `json:"highPassMode"`
	AudioPolarity     string  `json:"audioPolarity"`
	IsExpertMode      bool    `json:"isExpertMode"`
	DeskModeSetting   int     `json:"deskModeSetting"`
	SubwooferPreset   string  `json:"subwooferPreset"`
	HighPassModeFreq  int     `json:"highPassModeFreq"`
	WallModeSetting   float32 `json:"wallModeSetting"`
	Balance           int     `json:"balance"`
	SubEnableStereo   bool    `json:"subEnableStereo"`
	SubwooferPolarity string  `json:"subwooferPolarity"`
	SubwooferGain     int     `json:"subwooferGain"`
	IsKW1             bool    `json:"isKW1"`
	PhaseCorrection   bool    `json:"phaseCorrection"`
	WallMode          bool    `json:"wallMode"`
	ProfileId         string  `json:"profileId"`
	ProfileName       string  `json:"profileName"`
	SubOutLPFreq      float32 `json:"subOutLPFreq"`
}
