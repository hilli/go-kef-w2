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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillSetupRemove(t *testing.T) {
	directory := filepath.Join(skillTestDirectory(t), "new", "skills")
	target := filepath.Join(directory, "kefw2", skillFilename)
	for range 2 {
		if err := setupSkill(directory); err != nil {
			t.Fatal(err)
		}
		assertSkillFile(t, target, skillBootstrap)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := setupSkill(directory); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Error("repeat setup rewrote the bootstrap")
	}
	for range 2 {
		if err := removeSkill(directory); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Dir(target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill directory still exists: %v", err)
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("skills root must be retained: %v", err)
	}
}

func TestSkillRefusesForeignAndModified(t *testing.T) {
	for _, content := range []string{
		"foreign content",
		skillBootstrap + "\nuser changes",
		strings.Replace(skillBootstrap, "KEF", "kef", 1), // Same length, not identical.
		skillBootstrapV1 + "\nuser changes",
		strings.Replace(skillBootstrapV1, "KEF", "kef", 1),
	} {
		t.Run(content[:7], func(t *testing.T) {
			directory := skillTestDirectory(t)
			if err := setupSkill(directory); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(directory, "kefw2", skillFilename)
			writeSkillTestFile(t, target, content)
			for _, operation := range []func(string) error{setupSkill, removeSkill} {
				if err := operation(directory); err == nil {
					t.Fatal("expected refusal")
				}
				assertSkillFile(t, target, content)
			}
		})
	}
}

func TestSkillPreviousBootstrapLifecycle(t *testing.T) {
	for _, action := range []string{"setup", skillRemoveAction} {
		t.Run(action, func(t *testing.T) {
			directory := skillTestDirectory(t)
			target := filepath.Join(directory, "kefw2", skillFilename)
			notes := filepath.Join(directory, "kefw2", "notes.md")
			writeSkillTestFile(t, target, skillBootstrapV1)
			writeSkillTestFile(t, notes, "keep")
			if action == "setup" {
				if err := setupSkill(directory); err != nil {
					t.Fatal(err)
				}
				assertSkillFile(t, target, skillBootstrap)
			} else {
				if err := removeSkill(directory); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("previous bootstrap remains after removal: %v", err)
				}
			}
			assertSkillFile(t, notes, "keep")
			entries, err := os.ReadDir(filepath.Dir(target))
			expected := 1
			if action == "setup" {
				expected++
			}
			if err != nil || len(entries) != expected {
				t.Fatalf("unexpected files after %s: %v, %v", action, entries, err)
			}
		})
	}
}

func TestSkillPreservesOtherContents(t *testing.T) {
	directory := skillTestDirectory(t)
	if err := setupSkill(directory); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(directory, "adjacent.md"),
		filepath.Join(directory, "other-skill", skillFilename),
		filepath.Join(directory, "kefw2", "notes.md"),
	} {
		writeSkillTestFile(t, path, "keep")
	}
	for range 2 {
		if err := removeSkill(directory); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(directory, "adjacent.md"),
		filepath.Join(directory, "other-skill", skillFilename),
		filepath.Join(directory, "kefw2", "notes.md"),
	} {
		assertSkillFile(t, path, "keep")
	}
}

func TestSkillRefusesSymlinks(t *testing.T) {
	for _, kind := range []string{"root", "ancestor", "directory", "file", "dangling"} {
		t.Run(kind, func(t *testing.T) {
			base := skillTestDirectory(t)
			outside := skillTestDirectory(t)
			sentinel := filepath.Join(outside, skillFilename)
			writeSkillTestFile(t, sentinel, skillBootstrap)
			directory := filepath.Join(base, "skills")
			link, destination := directory, outside
			switch kind {
			case "ancestor":
				link = filepath.Join(base, "ancestor")
				directory = filepath.Join(link, "skills")
			case "directory":
				link = filepath.Join(directory, "kefw2")
			case "file", "dangling":
				link = filepath.Join(directory, "kefw2", skillFilename)
				destination = sentinel
				if kind == "dangling" {
					destination = filepath.Join(outside, "missing")
				}
			}
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(destination, link); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []func(string) error{setupSkill, removeSkill} {
				if err := operation(directory); err == nil {
					t.Fatal("expected symlink refusal")
				}
				info, err := os.Lstat(link)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink was changed: %v", err)
				}
				assertSkillFile(t, sentinel, skillBootstrap)
			}
		})
	}
}

func TestSkillRefusesNonRegularPaths(t *testing.T) {
	for _, filename := range []string{"kefw2", filepath.Join("kefw2", skillFilename)} {
		t.Run(filename, func(t *testing.T) {
			directory := skillTestDirectory(t)
			path := filepath.Join(directory, filename)
			if filename == "kefw2" {
				writeSkillTestFile(t, path, "not a directory")
			} else if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []func(string) error{setupSkill, removeSkill} {
				if err := operation(directory); err == nil {
					t.Fatal("expected non-regular path refusal")
				}
			}
		})
	}
}

func TestSkillMissingRemove(t *testing.T) {
	directory := filepath.Join(skillTestDirectory(t), "does", "not", "exist")
	if err := removeSkill(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove created directories: %v", err)
	}
}

func TestSkillPreservesUnownedEmptyDirectory(t *testing.T) {
	directory := skillTestDirectory(t)
	target := filepath.Join(directory, "kefw2")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeSkill(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("removed directory without an owned bootstrap: %v", err)
	}
}

func TestSkillCommands(t *testing.T) {
	home := skillTestDirectory(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	override := filepath.Join(home, "custom")
	for _, directory := range []string{"", override} {
		flags := []string{}
		targetRoot := filepath.Join(home, ".agents", "skills")
		if directory != "" {
			flags = []string{"--skills-dir", directory}
			targetRoot = directory
		}
		for _, action := range []string{"setup", "setup", "remove", "remove"} {
			command := newSkillCommand()
			command.SetArgs(append([]string{action}, flags...))
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(targetRoot, "kefw2", skillFilename)
			if action == "setup" {
				assertSkillFile(t, target, skillBootstrap)
			} else if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("remove left bootstrap: %v", err)
			}
		}
	}
}

func TestSkillVerboseProgress(t *testing.T) {
	for _, flag := range []string{"", "-v", "--verbose"} {
		t.Run("flag="+flag, func(t *testing.T) {
			home := skillTestDirectory(t)
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			if err := os.Mkdir(filepath.Join(home, ".claude"), 0o700); err != nil {
				t.Fatal(err)
			}
			targets := []string{
				filepath.Join(home, ".agents", "skills", "kefw2", skillFilename),
				filepath.Join(home, ".claude", "skills", "kefw2", skillFilename),
			}
			for _, action := range []string{"setup", "setup", skillRemoveAction, skillRemoveAction} {
				command := newSkillCommand()
				var output bytes.Buffer
				command.SetOut(&output)
				args := []string{action}
				if flag != "" {
					args = append(args, flag)
				}
				command.SetArgs(args)
				if err := command.Execute(); err != nil {
					t.Fatal(err)
				}
				if flag == "" {
					if output.Len() != 0 {
						t.Fatalf("quiet command printed progress: %s", output.String())
					}
					continue
				}
				for _, target := range targets {
					if !strings.Contains(output.String(), "Skill "+action+": "+target) ||
						!strings.Contains(output.String(), "OK: "+target) {
						t.Fatalf("progress omitted action or target: %s", output.String())
					}
				}
			}
		})
	}
}

func TestSkillVerboseWriterFailure(t *testing.T) {
	directory := filepath.Join(skillTestDirectory(t), "skills")
	command := newSkillCommand()
	command.SilenceErrors, command.SilenceUsage = true, true
	command.SetOut(skillFailWriter{})
	command.SetArgs([]string{"setup", "-v", "--skills-dir", directory})
	if err := command.Execute(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("verbose output error was not propagated: %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installed after progress writer failure: %v", err)
	}
}

func TestSkillCommandsDetectAgents(t *testing.T) {
	for _, customConfig := range []bool{false, true} {
		t.Run(fmt.Sprint(customConfig), func(t *testing.T) {
			home := skillTestDirectory(t)
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			configHome := filepath.Join(home, ".config")
			t.Setenv("XDG_CONFIG_HOME", "")
			if customConfig {
				configHome = filepath.Join(home, "custom-config")
				t.Setenv("XDG_CONFIG_HOME", configHome)
			}
			agents := []string{
				filepath.Join(home, ".claude"),
				filepath.Join(home, ".copilot"),
				filepath.Join(home, ".codex"),
				filepath.Join(home, ".cursor"),
				filepath.Join(home, ".gemini"),
				filepath.Join(configHome, "opencode"),
			}
			roots := make([]string, 0, len(agents)+1)
			roots = append(roots, filepath.Join(home, ".agents", "skills"))
			for _, agent := range agents {
				if err := os.MkdirAll(agent, 0o700); err != nil {
					t.Fatal(err)
				}
				roots = append(roots, filepath.Join(agent, "skills"))
			}
			unknown := filepath.Join(home, ".unknown", "skills")
			writeSkillTestFile(t, filepath.Join(unknown, "other", skillFilename), "keep")
			run := func(args ...string) {
				t.Helper()
				command := newSkillCommand()
				command.SetArgs(args)
				if err := command.Execute(); err != nil {
					t.Fatal(err)
				}
			}

			override := filepath.Join(home, "explicit-skills")
			run("setup", "--skills-dir", override)
			assertSkillFile(t, filepath.Join(override, "kefw2", skillFilename), skillBootstrap)
			run("remove", "--skills-dir", override)
			for _, root := range roots {
				if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("explicit override touched detected root %s: %v", root, err)
				}
			}
			for range 2 {
				run("setup")
				for _, root := range roots {
					assertSkillFile(t, filepath.Join(root, "kefw2", skillFilename), skillBootstrap)
				}
			}
			for range 2 {
				run("remove")
				for _, root := range roots {
					if _, err := os.Lstat(filepath.Join(root, "kefw2")); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("managed skill remains in %s: %v", root, err)
					}
				}
			}
			for _, agent := range agents {
				if _, err := os.Stat(agent); err != nil {
					t.Fatalf("removed agent configuration directory: %v", err)
				}
			}
			if _, err := os.Lstat(filepath.Join(unknown, "kefw2")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("installed into an unknown agent directory: %v", err)
			}
			assertSkillFile(t, filepath.Join(unknown, "other", skillFilename), "keep")
		})
	}
}

func TestSkillCommandsDoNotCreateAbsentAgents(t *testing.T) {
	home := skillTestDirectory(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	command := newSkillCommand()
	command.SetArgs([]string{"setup"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".agents" {
		t.Fatalf("setup created directories for absent agents: %v", entries)
	}
}

func TestSkillCommandsContinueAfterConflicts(t *testing.T) {
	home := skillTestDirectory(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	foreignRoots := []string{
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".cursor", "skills"),
	}
	for _, root := range foreignRoots {
		writeSkillTestFile(t, filepath.Join(root, "kefw2", skillFilename), "user-owned")
	}
	if err := os.Mkdir(filepath.Join(home, ".copilot"), 0o700); err != nil {
		t.Fatal(err)
	}
	managedRoots := []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".copilot", "skills"),
	}
	for _, action := range []string{"setup", skillRemoveAction} {
		command := newSkillCommand()
		command.SilenceErrors, command.SilenceUsage = true, true
		command.SetArgs([]string{action})
		err := command.Execute()
		if err == nil {
			t.Fatal("expected conflicts to be reported")
		}
		for _, root := range foreignRoots {
			if !strings.Contains(err.Error(), root) {
				t.Fatalf("error omitted conflict at %s: %v", root, err)
			}
			assertSkillFile(t, filepath.Join(root, "kefw2", skillFilename), "user-owned")
		}
		for _, root := range managedRoots {
			target := filepath.Join(root, "kefw2", skillFilename)
			if action == "setup" {
				assertSkillFile(t, target, skillBootstrap)
			} else if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("conflict prevented removal from %s: %v", root, err)
			}
		}
	}
}

func TestSkillDetectionIgnoresAbsentAgentUnderSymlink(t *testing.T) {
	home := skillTestDirectory(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	outside := skillTestDirectory(t)
	if err := os.Symlink(outside, filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	command := newSkillCommand()
	command.SetArgs([]string{"setup"})
	if err := command.Execute(); err != nil {
		t.Fatalf("absent OpenCode must not block other installs: %v", err)
	}
	assertSkillFile(t, filepath.Join(home, ".agents", "skills", "kefw2", skillFilename), skillBootstrap)
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("modified an unrelated symlink target: %v, %v", entries, err)
	}
}

func TestSkillDetectionRefusesSymlinks(t *testing.T) {
	for _, component := range []string{"agent", "skills"} {
		t.Run(component, func(t *testing.T) {
			home := skillTestDirectory(t)
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			outside := skillTestDirectory(t)
			link := filepath.Join(home, ".claude")
			sentinel := filepath.Join(outside, "skills", "kefw2", skillFilename)
			if component == "skills" {
				if err := os.Mkdir(link, 0o700); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(link, "skills")
				sentinel = filepath.Join(outside, "kefw2", skillFilename)
			}
			writeSkillTestFile(t, sentinel, skillBootstrap)
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(home, ".copilot"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"setup", skillRemoveAction} {
				command := newSkillCommand()
				command.SilenceErrors, command.SilenceUsage = true, true
				command.SetArgs([]string{action})
				if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "symlink") {
					t.Fatalf("expected symlink refusal: %v", err)
				}
				assertSkillFile(t, sentinel, skillBootstrap)
				for _, agent := range []string{".agents", ".copilot"} {
					path := filepath.Join(home, agent, "skills", "kefw2", skillFilename)
					if action == "setup" {
						assertSkillFile(t, path, skillBootstrap)
					} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("valid skill was not removed: %s: %v", path, err)
					}
				}
			}
			t.Setenv("XDG_CONFIG_HOME", "relative-invalid-path")
			override := filepath.Join(home, "explicit-skills")
			command := newSkillCommand()
			command.SetArgs([]string{"setup", "--skills-dir", override})
			if err := command.Execute(); err != nil {
				t.Fatalf("explicit override must bypass detection: %v", err)
			}
			assertSkillFile(t, filepath.Join(override, "kefw2", skillFilename), skillBootstrap)
		})
	}
}

func TestSkillShow(t *testing.T) {
	var output bytes.Buffer
	command := newSkillCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"show"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.String() != skillGuide || !strings.Contains(output.String(), "radio play --path") {
		t.Fatal("show did not emit exactly the embedded guide")
	}
	command = newSkillCommand()
	command.SetOut(skillFailWriter{})
	command.SetArgs([]string{"show"})
	command.SilenceErrors, command.SilenceUsage = true, true
	if err := command.Execute(); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("show did not propagate writer error: %v", err)
	}
}

func TestSkillArguments(t *testing.T) {
	for _, args := range [][]string{
		{"setup", "--skills-dir", ""},
		{"setup", "extra"},
		{"remove", "extra"},
		{"show", "extra"},
		{"show", "--verbose"},
	} {
		command := newSkillCommand()
		command.SetArgs(args)
		command.SilenceErrors, command.SilenceUsage = true, true
		if err := command.Execute(); err == nil {
			t.Fatalf("accepted invalid arguments: %v", args)
		}
	}
}

func TestSkillRegistered(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"skill", "show"})
	if err != nil || command.Name() != "show" || command.Parent().Name() != "skill" {
		t.Fatalf("skill show not registered: %v", err)
	}
}

type skillFailWriter struct{}

func (skillFailWriter) Write([]byte) (int, error) {
	return 0, os.ErrPermission
}

func skillTestDirectory(t *testing.T) string {
	t.Helper()
	// macOS /var is a system symlink. Tests pass the actual directory, since
	// the installer deliberately refuses even symlinks in path ancestors.
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func writeSkillTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertSkillFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected content in %s", path)
	}
}
