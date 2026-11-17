// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import "testing"

func TestAgentRadioCategoriesAreReadOnly(t *testing.T) {
	for _, category := range []string{"favorites", "popular", "local", "trending", "hq", "new"} {
		t.Run(category, func(t *testing.T) {
			f, speaker := newCLIFixture(t)
			f.rows["airable:https://radio.example/airable/radios/"+category] =
				`{"rowsCount":2,"rows":[{"title":"Station","path":"station:1","type":"audio"},` +
					`{"title":"Station","path":"station:2","type":"audio"}]}`
			home := t.TempDir()
			config := writeCLIConfig(t, home, speaker)
			stdout, stderr, err := runCLI(t, home, config, "radio", category, "--json")
			result := decodeCLI[struct {
				Data struct {
					Items []contentSummary `json:"items"`
				} `json:"data"`
			}](t, stdout, stderr, err)
			if len(result.Data.Items) != 2 {
				t.Fatalf("unexpected category: %+v", result)
			}
			stdout, stderr, err = runCLI(t, home, config, "radio", category, "Station", "--json")
			requireCLIFailure(t, stdout, stderr, err, "ambiguous")
			if len(f.postRequests()) != 0 {
				t.Fatal("listing or ambiguous selection changed playback")
			}
		})
	}
}

func TestAgentRadioBrowseDoesNotAutoPlay(t *testing.T) {
	f, speaker := newCLIFixture(t)
	f.rows["station:chosen"] = `{"rowsCount":1,"rows":[{"type":"audio","title":"Only station","path":"station:chosen"}]}`
	home := t.TempDir()
	config := writeCLIConfig(t, home, speaker)
	stdout, stderr, err := runCLI(t, home, config, "radio", "browse", "--path", "station:chosen", "--json")
	decodeCLI[commandResponse](t, stdout, stderr, err)
	if len(f.postRequests()) != 0 {
		t.Fatal("single-result browse changed playback")
	}
}
