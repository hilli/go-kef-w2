// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package kefw2_test

import (
	"os/exec"
	"testing"
)

func TestAirableClientLegacyUnkeyedLiteralCompiles(t *testing.T) {
	// Vet intentionally rejects unkeyed external literals, so compile this
	// compatibility fixture separately without that analyzer.
	cmd := exec.CommandContext(t.Context(), "go", "test", "-vet=off", "-tags=airable_legacy_literal", "-run=^$", ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("legacy AirableClient literal does not compile: %v\n%s", err, output)
	}
}
