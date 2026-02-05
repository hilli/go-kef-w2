/*
Copyright © 2023-2026 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"testing"
)

func TestParseTimePosition(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		// Valid seconds only
		{"zero seconds", "0", 0, false},
		{"90 seconds", "90", 90000, false},
		{"large seconds", "3600", 3600000, false},

		// Valid mm:ss
		{"5:30", "5:30", 330000, false},
		{"0:45", "0:45", 45000, false},
		{"10:00", "10:00", 600000, false},
		{"99:59", "99:59", 5999000, false},

		// Valid hh:mm:ss
		{"1:00:00", "1:00:00", 3600000, false},
		{"1:23:45", "1:23:45", 5025000, false},
		{"0:05:30", "0:05:30", 330000, false},
		{"10:30:15", "10:30:15", 37815000, false},

		// Invalid formats
		{"empty", "", 0, true},
		{"negative seconds", "-5", 0, true},
		{"seconds out of range mm:ss", "5:60", 0, true},
		{"seconds out of range hh:mm:ss", "1:30:60", 0, true},
		{"minutes out of range hh:mm:ss", "1:60:30", 0, true},
		{"too many colons", "1:2:3:4", 0, true},
		{"non-numeric", "abc", 0, true},
		{"mixed non-numeric", "5:abc", 0, true},

		// Edge cases
		{"whitespace", "  90  ", 90000, false},
		{"negative in mm:ss", "-5:30", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimePosition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimePosition(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseTimePosition(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		ms   int
		want string
	}{
		{"zero", 0, "0:00"},
		{"45 seconds", 45000, "0:45"},
		{"5 minutes 30 seconds", 330000, "5:30"},
		{"1 hour", 3600000, "1:00:00"},
		{"1:23:45", 5025000, "1:23:45"},
		{"10:30:15", 37815000, "10:30:15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.ms); got != tt.want {
				t.Errorf("formatDuration(%v) = %v, want %v", tt.ms, got, tt.want)
			}
		})
	}
}
