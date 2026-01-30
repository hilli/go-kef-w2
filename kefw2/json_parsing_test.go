package kefw2

import (
	"errors"
	"testing"
)

func TestParseJSONString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid string response",
			input:   `[{"type":"string_","string_":"KEF LS50 II"}]`,
			want:    "KEF LS50 II",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   `[{"type":"string_","string_":""}]`,
			want:    "",
			wantErr: false,
		},
		{
			name:    "mac address format",
			input:   `[{"type":"string_","string_":"AA:BB:CC:DD:EE:FF"}]`,
			want:    "AA:BB:CC:DD:EE:FF",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			want:    "",
			wantErr: true,
		},
		{
			name:    "empty array",
			input:   `[]`,
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			want:    "",
			wantErr: true,
		},
		{
			name:    "wrong type",
			input:   `[{"type":"i32_","i32_":42}]`,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONString([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseJSONString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJSONInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{
			name:    "valid i32 response",
			input:   `[{"type":"i32_","i32_":42}]`,
			want:    42,
			wantErr: false,
		},
		{
			name:    "valid i64 response",
			input:   `[{"type":"i64_","i64_":12345678}]`,
			want:    12345678,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   `[{"type":"i32_","i32_":0}]`,
			want:    0,
			wantErr: false,
		},
		{
			name:    "volume level",
			input:   `[{"type":"i32_","i32_":25}]`,
			want:    25,
			wantErr: false,
		},
		{
			name:    "max volume",
			input:   `[{"type":"i32_","i32_":100}]`,
			want:    100,
			wantErr: false,
		},
		{
			name:    "play time in ms",
			input:   `[{"type":"i64_","i64_":180000}]`,
			want:    180000,
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			want:    0,
			wantErr: true,
		},
		{
			name:    "wrong type",
			input:   `[{"type":"string_","string_":"hello"}]`,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONInt([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseJSONInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJSONBool(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    bool
		wantErr bool
	}{
		{
			name:    "true value",
			input:   `[{"type":"bool_","bool_":"true"}]`,
			want:    true,
			wantErr: false,
		},
		{
			name:    "false value",
			input:   `[{"type":"bool_","bool_":"false"}]`,
			want:    false,
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			want:    false,
			wantErr: true,
		},
		{
			name:    "wrong type",
			input:   `[{"type":"string_","string_":"true"}]`,
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONBool([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONBool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseJSONBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseJSONValue(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    any
		wantErr bool
	}{
		{
			name:    "source wifi",
			input:   `[{"type":"kefPhysicalSource","kefPhysicalSource":"wifi"}]`,
			want:    SourceWiFi,
			wantErr: false,
		},
		{
			name:    "source bluetooth",
			input:   `[{"type":"kefPhysicalSource","kefPhysicalSource":"bluetooth"}]`,
			want:    SourceBluetooth,
			wantErr: false,
		},
		{
			name:    "source standby",
			input:   `[{"type":"kefPhysicalSource","kefPhysicalSource":"standby"}]`,
			want:    SourceStandby,
			wantErr: false,
		},
		{
			name:    "speaker status powerOn",
			input:   `[{"type":"kefSpeakerStatus","kefSpeakerStatus":"powerOn"}]`,
			want:    SpeakerStatusOn,
			wantErr: false,
		},
		{
			name:    "speaker status standby",
			input:   `[{"type":"kefSpeakerStatus","kefSpeakerStatus":"standby"}]`,
			want:    SpeakerStatusStandby,
			wantErr: false,
		},
		{
			name:    "cable mode wired",
			input:   `[{"type":"kefCableMode","kefCableMode":"wired"}]`,
			want:    Wired,
			wantErr: false,
		},
		{
			name:    "cable mode wireless",
			input:   `[{"type":"kefCableMode","kefCableMode":"wireless"}]`,
			want:    Wireless,
			wantErr: false,
		},
		{
			name:    "string value",
			input:   `[{"type":"string_","string_":"test"}]`,
			want:    "test",
			wantErr: false,
		},
		{
			name:    "int value",
			input:   `[{"type":"i32_","i32_":50}]`,
			want:    50,
			wantErr: false,
		},
		{
			name:    "bool true",
			input:   `[{"type":"bool_","bool_":"true"}]`,
			want:    true,
			wantErr: false,
		},
		{
			name:    "bool false",
			input:   `[{"type":"bool_","bool_":"false"}]`,
			want:    false,
			wantErr: false,
		},
		{
			name:    "unknown type",
			input:   `[{"type":"unknown_type"}]`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJSONValue([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseJSONValue() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestParseJSONValueEQProfile(t *testing.T) {
	input := `[{
		"type": "kefEqProfileV2",
		"kefEqProfileV2": {
			"profileName": "Default",
			"profileId": "default-id",
			"balance": 0,
			"bassExtension": "standard",
			"deskMode": false,
			"wallMode": false,
			"trebleAmount": 0.0
		}
	}]`

	got, err := parseJSONValue([]byte(input))
	if err != nil {
		t.Fatalf("parseJSONValue() error = %v", err)
	}

	profile, ok := got.(EQProfileV2)
	if !ok {
		t.Fatalf("parseJSONValue() returned %T, want EQProfileV2", got)
	}

	if profile.ProfileName != "Default" {
		t.Errorf("ProfileName = %q, want %q", profile.ProfileName, "Default")
	}
	if profile.Balance != 0 {
		t.Errorf("Balance = %d, want 0", profile.Balance)
	}
	if profile.BassExtension != "standard" {
		t.Errorf("BassExtension = %q, want %q", profile.BassExtension, "standard")
	}
}

// Test the deprecated functions for backward compatibility.
func TestDeprecatedJSONFunctions(t *testing.T) {
	t.Run("JSONStringValue with error", func(t *testing.T) {
		_, err := JSONStringValue(nil, ErrEmptyData)
		if !errors.Is(err, ErrEmptyData) {
			t.Errorf("JSONStringValue() should pass through error")
		}
	})

	t.Run("JSONStringValue without error", func(t *testing.T) {
		data := []byte(`[{"type":"string_","string_":"test"}]`)
		got, err := JSONStringValue(data, nil)
		if err != nil {
			t.Errorf("JSONStringValue() error = %v", err)
		}
		if got != "test" {
			t.Errorf("JSONStringValue() = %q, want %q", got, "test")
		}
	})

	t.Run("JSONIntValue with error", func(t *testing.T) {
		_, err := JSONIntValue(nil, ErrEmptyData)
		if !errors.Is(err, ErrEmptyData) {
			t.Errorf("JSONIntValue() should pass through error")
		}
	})

	t.Run("JSONUnmarshalValue with error", func(t *testing.T) {
		_, err := JSONUnmarshalValue(nil, ErrEmptyData)
		if !errors.Is(err, ErrEmptyData) {
			t.Errorf("JSONUnmarshalValue() should pass through error")
		}
	})
}
