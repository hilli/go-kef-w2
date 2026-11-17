// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentSkillVerbose(t *testing.T) {
	home := skillTestDirectory(t)
	target := filepath.Join(home, ".agents", "skills", "kefw2", skillFilename)
	config := filepath.Join(home, "missing.yaml")
	for _, action := range []string{"setup", skillRemoveAction} {
		stdout, stderr, err := runCLI(t, home, config, "skill", action, "-v")
		if err != nil || stderr != "" || !strings.Contains(stdout, target) {
			t.Fatalf("verbose %s: err=%v stderr=%q stdout=%q", action, err, stderr, stdout)
		}
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verbose removal left managed bootstrap: %v", err)
	}
}

func TestAgentSkillLifecycle(t *testing.T) {
	for _, mode := range []string{"default", "override", "detected"} {
		t.Run(mode, func(t *testing.T) {
			// Resolve macOS's system temp symlinks before passing paths to
			// the installer, which deliberately rejects symlink ancestors.
			home := skillTestDirectory(t)
			config := filepath.Join(home, "malformed.yaml")
			const malformed = "speakers: [\n"
			writeSkillTestFile(t, config, malformed)

			skillsRoot := filepath.Join(home, ".agents", "skills")
			allowedDirectory := ".agents"
			var flags []string
			if mode == "override" {
				allowedDirectory = "custom-skills"
				skillsRoot = filepath.Join(home, allowedDirectory)
				flags = []string{"--skills-dir", skillsRoot}
			}
			allowed := map[string]bool{filepath.Base(config): true, allowedDirectory: true}
			var detectedStub string
			if mode == "detected" {
				directory := filepath.Join(home, ".claude")
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				allowed[".claude"] = true
				detectedStub = filepath.Join(directory, "skills", "kefw2", skillFilename)
				writeSkillTestFile(t, detectedStub, skillBootstrapV1)
			}
			stub := filepath.Join(skillsRoot, "kefw2", skillFilename)
			adjacent := filepath.Join(skillsRoot, "kefw2", "notes.md")

			run := func(action string) string {
				t.Helper()
				args := []string{"skill", action}
				if action != "show" {
					args = append(args, flags...)
				}
				stdout, stderr, err := runCLI(t, home, config, args...)
				if err != nil || stderr != "" {
					t.Fatalf("skill %s: err=%v stderr=%q stdout=%q", action, err, stderr, stdout)
				}
				assertSkillFile(t, config, malformed)
				entries, err := os.ReadDir(home)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if !allowed[entry.Name()] {
						t.Fatalf("skill %s created unexpected home artifact %q", action, entry.Name())
					}
				}
				return stdout
			}

			for range 2 {
				run("setup")
				assertSkillFile(t, stub, skillBootstrap)
				if detectedStub != "" {
					assertSkillFile(t, detectedStub, skillBootstrap)
				}
			}
			if output := run("show"); output != skillGuide {
				t.Fatalf("skill show did not emit exactly the embedded guide: %q", output)
			}
			writeSkillTestFile(t, adjacent, "foreign notes: preserve me\n")
			for range 2 {
				run("remove")
				if _, err := os.Lstat(stub); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("managed bootstrap remains after removal: %v", err)
				}

				if detectedStub != "" {
					if _, err := os.Lstat(detectedStub); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("detected bootstrap remains after removal: %v", err)
					}
				}
				assertSkillFile(t, adjacent, "foreign notes: preserve me\n")
			}
		})
	}
}
