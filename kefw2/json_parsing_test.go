package kefw2

import (
	"errors"
	"testing"
)

func TestParseTypedInt(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		err     error
		want    int
		wantErr bool
	}{
		{
			name: "valid i32",
			data: []byte(`[{"type":"i32_","i32_":42}]`),
			want: 42,
		},
		{
			name: "valid i64",
			data: []byte(`[{"type":"i64_","i64_":12345}]`),
			want: 12345,
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"string_","string_":"hello"}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"i32_"}]`),
			wantErr: true,
		},
		{
			name:    "invalid json",
			data:    []byte(`{invalid`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedInt(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseTypedInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTypedString(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		err     error
		want    string
		wantErr bool
	}{
		{
			name: "valid string",
			data: []byte(`[{"type":"string_","string_":"test value"}]`),
			want: "test value",
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"i32_","i32_":42}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"string_"}]`),
			wantErr: true,
		},
		{
			name:    "invalid json",
			data:    []byte(`not json`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedString(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseTypedString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTypedBool(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		err     error
		want    bool
		wantErr bool
	}{
		{
			name: "true value",
			data: []byte(`[{"type":"bool_","bool_":"true"}]`),
			want: true,
		},
		{
			name: "false value",
			data: []byte(`[{"type":"bool_","bool_":"false"}]`),
			want: false,
		},
		{
			name: "any non-false is true",
			data: []byte(`[{"type":"bool_","bool_":"1"}]`),
			want: true,
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"i32_","i32_":1}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"bool_"}]`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedBool(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedBool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseTypedBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTypedSource(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		err     error
		want    Source
		wantErr bool
	}{
		{
			name: "wifi source",
			data: []byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`),
			want: SourceWiFi,
		},
		{
			name: "bluetooth source",
			data: []byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"bluetooth"}]`),
			want: SourceBluetooth,
		},
		{
			name: "standby source",
			data: []byte(`[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`),
			want: SourceStandby,
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"string_","string_":"wifi"}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"kefPhysicalSource"}]`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedSource(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseTypedSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTypedSpeakerStatus(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		err     error
		want    SpeakerStatus
		wantErr bool
	}{
		{
			name: "power on",
			data: []byte(`[{"type":"kefSpeakerStatus","kefSpeakerStatus":"powerOn"}]`),
			want: SpeakerStatusOn,
		},
		{
			name: "standby",
			data: []byte(`[{"type":"kefSpeakerStatus","kefSpeakerStatus":"standby"}]`),
			want: SpeakerStatusStandby,
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"string_","string_":"powerOn"}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"kefSpeakerStatus"}]`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedSpeakerStatus(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedSpeakerStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseTypedSpeakerStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTypedCableMode(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		err     error
		want    CableMode
		wantErr bool
	}{
		{
			name: "wired mode",
			data: []byte(`[{"type":"kefCableMode","kefCableMode":"wired"}]`),
			want: Wired,
		},
		{
			name: "wireless mode",
			data: []byte(`[{"type":"kefCableMode","kefCableMode":"wireless"}]`),
			want: Wireless,
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"string_","string_":"wired"}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"kefCableMode"}]`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedCableMode(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedCableMode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseTypedCableMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTypedEQProfile(t *testing.T) {
	validProfile := `[{"type":"kefEqProfileV2","kefEqProfileV2":{
		"audioPolarity":"positive",
		"balance":0,
		"bassExtension":"standard",
		"deskMode":false,
		"deskModeSetting":-3.5,
		"highPassMode":false,
		"highPassModeFreq":80,
		"isExpertMode":false,
		"isKW1":false,
		"phaseCorrection":true,
		"profileId":"default-123",
		"profileName":"Default",
		"soundProfile":"default",
		"subEnableStereo":false,
		"subOutLPFreq":80.0,
		"subwooferCount":0,
		"subwooferGain":0,
		"subwooferOut":false,
		"subwooferPolarity":"positive",
		"subwooferPreset":"none",
		"trebleAmount":0.0,
		"wallMode":false,
		"wallModeSetting":0.0,
		"wirelessSub":"none"
	}}]`

	tests := []struct {
		name    string
		data    []byte
		err     error
		want    EQProfileV2
		wantErr bool
	}{
		{
			name: "valid profile",
			data: []byte(validProfile),
			want: EQProfileV2{
				AudioPolarity:     "positive",
				Balance:           0,
				BassExtension:     "standard",
				DeskMode:          false,
				DeskModeSetting:   -3.5,
				HighPassMode:      false,
				HighPassModeFreq:  80,
				IsExpertMode:      false,
				IsKW1:             false,
				PhaseCorrection:   true,
				ProfileID:         "default-123",
				ProfileName:       "Default",
				SoundProfile:      "default",
				SubEnableStereo:   false,
				SubOutLPFreq:      80.0,
				SubwooferCount:    0,
				SubwooferGain:     0,
				SubwooferOut:      false,
				SubwooferPolarity: "positive",
				SubwooferPreset:   "none",
				TrebleAmount:      0.0,
				WallMode:          false,
				WallModeSetting:   0.0,
				WirelessSub:       "none",
			},
		},
		{
			name:    "empty array",
			data:    []byte(`[]`),
			wantErr: true,
		},
		{
			name:    "wrong type",
			data:    []byte(`[{"type":"string_","string_":"test"}]`),
			wantErr: true,
		},
		{
			name:    "missing value",
			data:    []byte(`[{"type":"kefEqProfileV2"}]`),
			wantErr: true,
		},
		{
			name:    "invalid json",
			data:    []byte(`{invalid`),
			wantErr: true,
		},
		{
			name:    "input error propagated",
			err:     errors.New("upstream error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTypedEQProfile(tt.data, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTypedEQProfile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.ProfileName != tt.want.ProfileName {
					t.Errorf("parseTypedEQProfile() ProfileName = %v, want %v", got.ProfileName, tt.want.ProfileName)
				}
				if got.ProfileID != tt.want.ProfileID {
					t.Errorf("parseTypedEQProfile() ProfileID = %v, want %v", got.ProfileID, tt.want.ProfileID)
				}
				if got.BassExtension != tt.want.BassExtension {
					t.Errorf("parseTypedEQProfile() BassExtension = %v, want %v", got.BassExtension, tt.want.BassExtension)
				}
				if got.DeskModeSetting != tt.want.DeskModeSetting {
					t.Errorf("parseTypedEQProfile() DeskModeSetting = %v, want %v", got.DeskModeSetting, tt.want.DeskModeSetting)
				}
				if got.PhaseCorrection != tt.want.PhaseCorrection {
					t.Errorf("parseTypedEQProfile() PhaseCorrection = %v, want %v", got.PhaseCorrection, tt.want.PhaseCorrection)
				}
			}
		})
	}
}
