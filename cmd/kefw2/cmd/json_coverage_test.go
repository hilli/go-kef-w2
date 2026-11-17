// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestOperationalCommandsSupportJSON(t *testing.T) {
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		switch cmd.Name() {
		case "skill", "completion", "help":
			return
		}
		if cmd != rootCmd && (cmd.Run != nil || cmd.RunE != nil) &&
			(!cmd.HasSubCommands() || cmd == queueCmd || cmd == radioCmd || cmd == podcastCmd || cmd == upnpCmd) {
			if cmd.Flags().Lookup("json") == nil {
				t.Errorf("operational command lacks --json: %s", cmd.CommandPath())
			}
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(rootCmd)
}
