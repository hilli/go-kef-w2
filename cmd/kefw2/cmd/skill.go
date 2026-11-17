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
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Keep the bootstrap stable when possible, and retain exact previous versions
// for safe upgrades. Exact bytes, not a user-editable ownership marker or
// self-reported hash, establish ownership.
//
//go:embed skillassets/SKILL.md
var skillBootstrap string

//go:embed skillassets/bootstrap-v1.md
var skillBootstrapV1 string

//go:embed skillassets/guide.md
var skillGuide string

const skillRemoveAction = "remove"

func newSkillCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "skill",
		Short: "Install or display the embedded agent skill (offline)",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the guide bundled with this binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), skillGuide)
			return err
		},
	})
	for _, action := range []string{"setup", skillRemoveAction} {
		child := &cobra.Command{
			Use:   action,
			Short: action + " the managed kefw2 skill bootstrap",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runSkillAction(cmd, action)
			},
		}
		child.Long = "Manage ~/.agents/skills and skills directories for detected agents: " +
			"Claude, Copilot, Codex (legacy), Cursor, Gemini, and OpenCode.\n" +
			"Only existing agent configuration directories are selected. " +
			"--skills-dir overrides automatic detection; kefw2/SKILL.md is appended."
		child.Flags().String("skills-dir", "", "Use only this skills root instead of automatic detection")
		child.Flags().BoolP("verbose", "v", false, "Report skill paths and setup/removal progress")
		command.AddCommand(child)
	}
	return command
}

func runSkillAction(cmd *cobra.Command, action string) error {
	directories, err := skillDirectories(cmd)
	var failures []error
	if err != nil {
		failures = append(failures, err)
	}
	verbose, _ := cmd.Flags().GetBool("verbose")
	for _, directory := range directories {
		target := filepath.Join(directory, "kefw2", skillFilename)
		if verbose {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Skill %s: %s\n", action, target); err != nil {
				return errors.Join(append(failures, err)...)
			}
		}
		if action == "setup" {
			err = setupSkill(directory)
		} else {
			err = removeSkill(directory)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("skill %s %s: %w", action, directory, err))
			continue
		}
		if verbose {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "OK: %s\n", target); err != nil {
				return errors.Join(append(failures, err)...)
			}
		}
	}
	return errors.Join(failures...)
}

func skillDirectories(cmd *cobra.Command) ([]string, error) {
	directory, err := cmd.Flags().GetString("skills-dir")
	if err != nil {
		return nil, err
	}
	if cmd.Flags().Changed("skills-dir") {
		if directory == "" {
			return nil, fmt.Errorf("--skills-dir must not be empty")
		}
		return []string{directory}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home directory: %w", err)
	}
	return detectSkillDirectories(home)
}

func init() {
	rootCmd.AddCommand(newSkillCommand())
}
