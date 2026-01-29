package kefw2

import (
	"testing"
)

func TestSourceString(t *testing.T) {
	tests := []struct {
		source Source
		want   string
	}{
		{SourceWiFi, "wifi"},
		{SourceBluetooth, "bluetooth"},
		{SourceAux, "analog"},
		{SourceOptical, "optical"},
		{SourceCoaxial, "coaxial"},
		{SourceTV, "tv"},
		{SourceUSB, "usb"},
		{SourceStandby, "standby"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.source.String()
			if got != tt.want {
				t.Errorf("Source.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSpeakerStatusString(t *testing.T) {
	tests := []struct {
		status SpeakerStatus
		want   string
	}{
		{SpeakerStatusOn, "powerOn"},
		{SpeakerStatusStandby, "standby"},
		{SpeakerInNetworkSetup, "networkSetup"},
		{SpeakerInFirmwareUpgrade, "firmwareUpgrade"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("SpeakerStatus.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCableModeString(t *testing.T) {
	tests := []struct {
		mode CableMode
		want string
	}{
		{Wired, "wired"},
		{Wireless, "wireless"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.mode.String()
			if got != tt.want {
				t.Errorf("CableMode.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEQProfileV2String(t *testing.T) {
	profile := EQProfileV2{
		ProfileName:   "Custom",
		ProfileId:     "custom-123",
		Balance:       0,
		BassExtension: "standard",
		DeskMode:      false,
		WallMode:      true,
		TrebleAmount:  0.5,
	}

	str := profile.String()

	// Should be valid JSON
	if str == "" {
		t.Error("EQProfileV2.String() returned empty string")
	}

	// Should contain profile name
	if !containsString(str, "Custom") {
		t.Error("EQProfileV2.String() should contain profile name")
	}

	// Should contain wallMode
	if !containsString(str, "wallMode") {
		t.Error("EQProfileV2.String() should contain wallMode field")
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || containsString(s[1:], substr)))
}

func TestSourceKEFTypeInfo(t *testing.T) {
	tests := []struct {
		source    Source
		wantType  string
		wantValue string
	}{
		{SourceWiFi, "kefPhysicalSource", "wifi"},
		{SourceBluetooth, "kefPhysicalSource", "bluetooth"},
		{SourceStandby, "kefPhysicalSource", "standby"},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			typeName, value := tt.source.KEFTypeInfo()
			if typeName != tt.wantType {
				t.Errorf("KEFTypeInfo() type = %q, want %q", typeName, tt.wantType)
			}
			if value != tt.wantValue {
				t.Errorf("KEFTypeInfo() value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

func TestSpeakerStatusKEFTypeInfo(t *testing.T) {
	tests := []struct {
		status    SpeakerStatus
		wantType  string
		wantValue string
	}{
		{SpeakerStatusOn, "kefSpeakerStatus", `"powerOn"`},
		{SpeakerStatusStandby, "kefSpeakerStatus", `"standby"`},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			typeName, value := tt.status.KEFTypeInfo()
			if typeName != tt.wantType {
				t.Errorf("KEFTypeInfo() type = %q, want %q", typeName, tt.wantType)
			}
			if value != tt.wantValue {
				t.Errorf("KEFTypeInfo() value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

func TestCableModeKEFTypeInfo(t *testing.T) {
	tests := []struct {
		mode      CableMode
		wantType  string
		wantValue string
	}{
		{Wired, "kefCableMode", `"wired"`},
		{Wireless, "kefCableMode", `"wireless"`},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			typeName, value := tt.mode.KEFTypeInfo()
			if typeName != tt.wantType {
				t.Errorf("KEFTypeInfo() type = %q, want %q", typeName, tt.wantType)
			}
			if value != tt.wantValue {
				t.Errorf("KEFTypeInfo() value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}
