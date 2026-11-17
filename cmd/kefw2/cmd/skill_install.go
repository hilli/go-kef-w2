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
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const skillFilename = "SKILL.md"

func detectSkillDirectories(home string) ([]string, error) {
	directories := []string{filepath.Join(home, ".agents", "skills")}
	var failures []error
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(configHome) {
		return nil, fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
	}
	agentDirectories := []string{
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".copilot"),
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".cursor"),
		filepath.Join(home, ".gemini"),
		filepath.Join(configHome, "opencode"),
	}
	for _, directory := range agentDirectories {
		exists, err := skillDirectoryExists(directory)
		if err != nil {
			failures = append(failures, fmt.Errorf("inspect agent directory %s: %w", directory, err))
			continue
		}
		if !exists {
			continue
		}
		root, err := openSkillPath(directory, false)
		if err != nil {
			failures = append(failures, fmt.Errorf("detect agent directory %s: %w", directory, err))
			continue
		}
		if root != nil {
			_ = root.Close()
			directories = append(directories, filepath.Join(directory, "skills"))
		}
	}
	return directories, errors.Join(failures...)
}

func skillDirectoryExists(directory string) (bool, error) {
	parent, err := os.OpenRoot(filepath.Dir(directory))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer parent.Close()
	_, err = parent.Lstat(filepath.Base(directory))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func setupSkill(directory string) error {
	root, err := openSkillDirectory(directory, true)
	if err != nil {
		return err
	}
	defer root.Close()

	content, err := checkSkillBootstrap(root)
	if err != nil || content == skillBootstrap {
		return err
	}
	if content != "" {
		return upgradeSkillBootstrap(root)
	}
	return createSkillBootstrap(root, skillFilename)
}

func createSkillBootstrap(root *os.Root, name string) error {
	// Exclusive creation never follows a symlink or overwrites another writer.
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", skillFilename, err)
	}
	_, writeErr := io.WriteString(file, skillBootstrap)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write %s: %w", skillFilename, errors.Join(err, root.Remove(name)))
	}
	return nil
}

func upgradeSkillBootstrap(root *os.Root) (err error) {
	temporary := ".SKILL.md-" + rand.Text()
	if err := createSkillBootstrap(root, temporary); err != nil {
		return err
	}
	defer func() {
		if cleanupErr := root.Remove(temporary); cleanupErr != nil && !errors.Is(cleanupErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove staged %s: %w", skillFilename, cleanupErr))
		}
	}()
	// Recheck ownership after staging, then replace the entry without following it.
	content, err := checkSkillBootstrap(root)
	if err != nil {
		return err
	}
	if content == skillBootstrap {
		return nil
	}
	if content != skillBootstrapV1 {
		return fmt.Errorf("%s changed during upgrade; retry setup", skillFilename)
	}
	if err := root.Rename(temporary, skillFilename); err != nil {
		return fmt.Errorf("upgrade %s: %w", skillFilename, err)
	}
	return nil
}

func removeSkill(directory string) error {
	root, err := openSkillDirectory(directory, false)
	if err != nil || root == nil {
		return err
	}
	defer root.Close()
	content, err := checkSkillBootstrap(root)
	if err != nil || content == "" {
		return err
	}
	if err := root.Remove(skillFilename); err != nil {
		return fmt.Errorf("remove %s: %w", skillFilename, err)
	}
	// Remove only an empty directory; never recursively delete neighbors.
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return fmt.Errorf("inspect skill directory: %w", err)
	}
	if len(entries) != 0 {
		return nil
	}
	// Reopen the parent using the same no-symlink traversal.
	parent, err := openSkillPath(directory, false)
	if err != nil {
		return err
	}
	if parent == nil {
		return nil
	}
	defer parent.Close()
	if err := parent.Remove("kefw2"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove empty skill directory: %w", err)
	}
	return nil
}

func checkSkillBootstrap(root *os.Root) (string, error) {
	info, err := root.Lstat(skillFilename)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", skillFilename, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing non-regular file or symlink: %s", skillFilename)
	}
	if info.Size() != int64(len(skillBootstrap)) && info.Size() != int64(len(skillBootstrapV1)) {
		return "", fmt.Errorf("refusing foreign or modified %s; move it aside manually", skillFilename)
	}
	content, err := root.ReadFile(skillFilename)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", skillFilename, err)
	}
	if string(content) != skillBootstrap && string(content) != skillBootstrapV1 {
		return "", fmt.Errorf("refusing foreign or modified %s; move it aside manually", skillFilename)
	}
	return string(content), nil
}

func openSkillDirectory(directory string, create bool) (*os.Root, error) {
	return openSkillPath(filepath.Join(directory, "kefw2"), create)
}

// Walk from the filesystem root with directory handles. Reject symlinks in every
// component, including dangling links, rather than silently redirect installation.
// A missing path during removal is a successful no-op.
func openSkillPath(path string, create bool) (*os.Root, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve skills directory: %w", err)
	}
	volume := filepath.VolumeName(absolute) + string(filepath.Separator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	components := strings.Split(strings.TrimPrefix(absolute, volume), string(filepath.Separator))
	for _, component := range components {
		if component == "" {
			continue
		}
		next, openErr := openSkillPathComponent(root, component, create)
		_ = root.Close()
		if openErr != nil || next == nil {
			return nil, openErr
		}
		root = next
	}
	return root, nil
}

func openSkillPathComponent(root *os.Root, name string, create bool) (*os.Root, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return nil, nil
		}
		if err = root.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create skill directory %s: %w", name, err)
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect skill directory %s: %w", name, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing non-directory or symlink in skill path: %s", name)
	}
	next, err := root.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("open skill directory %s: %w", name, err)
	}
	return next, nil
}
