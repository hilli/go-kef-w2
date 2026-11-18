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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/hilli/go-kef-w2/kefw2"
)

var eqProfileCmd = &cobra.Command{
	Use:   "eq_profile [PROFILE.json]",
	Short: "Get or restore the equalizer profile",
	Long:  "Read the current source's equalizer profile or restore one from a JSON file.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if len(args) == 1 {
			profile, err := readEQProfileFile(args[0])
			if err != nil {
				return err
			}
			source, err := currentSpeaker.Source(ctx)
			if err != nil {
				return fmt.Errorf("getting current source: %w", err)
			}
			if err := currentSpeaker.SetEQProfileV2(ctx, profile); err != nil {
				return fmt.Errorf("restoring EQ profile: %w", err)
			}
			taskConpletedPrinter.Printf("Restored EQ profile %q to %s source\n", profile.ProfileName, source)
			return nil
		}

		eqProfile, err := currentSpeaker.GetEQProfileV2(ctx)
		if err != nil {
			return fmt.Errorf("getting EQ profile: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), eqProfile)
		return err
	},
}

var requiredEQProfileFields = []string{
	"audioPolarity", "balance", "bassExtension", "deskMode", "deskModeSetting",
	"highPassMode", "highPassModeFreq", "isExpertMode", "isKW1", "phaseCorrection",
	"profileId", "profileName", "soundProfile", "subEnableStereo", "subOutLPFreq",
	"subwooferCount", "subwooferGain", "subwooferOut", "subwooferPolarity",
	"subwooferPreset", "trebleAmount", "wallMode", "wallModeSetting", "wirelessSub",
}

func readEQProfileFile(path string) (kefw2.EQProfileV2, error) {
	data, err := os.ReadFile(path) //nolint:gosec // The user explicitly supplies the profile file.
	if err != nil {
		return kefw2.EQProfileV2{}, fmt.Errorf("reading EQ profile: %w", err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: expected a JSON object")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: %w", err)
	}
	for _, field := range requiredEQProfileFields {
		_, ok := fields[field]
		if !ok {
			return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: missing required field %q", field)
		}
	}
	for field, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: field %q must not be null", field)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var profile kefw2.EQProfileV2
	if err := decoder.Decode(&profile); err != nil {
		return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: multiple JSON values")
		}
		return kefw2.EQProfileV2{}, fmt.Errorf("parsing EQ profile: %w", err)
	}
	return profile, nil
}

func init() {
	ConfigCmd.AddCommand(eqProfileCmd)
}
