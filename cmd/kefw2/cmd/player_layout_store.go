// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const playerLayoutVersion = 1

type playerArrangement struct {
	Order []string       `yaml:"order"`
	Width map[string]int `yaml:"width"`
}

type playerLayoutFile struct {
	Version int                          `yaml:"version"`
	Layouts map[string]playerArrangement `yaml:"layouts"`
}

func playerDefaultArrangement(layout string) playerArrangement {
	if layout == playerDiscoveryBrowser {
		return playerArrangement{Order: []string{playerPaneSources, playerPaneDiscover, playerPaneDetails}, Width: map[string]int{
			playerPaneSources: 22, playerPaneDiscover: 38, playerPaneDetails: 40,
		}}
	}
	return playerArrangement{Order: []string{playerPaneSources, playerPaneDiscover, ModeQueue}, Width: map[string]int{
		playerPaneSources: 24, playerPaneDiscover: 42, ModeQueue: 32,
	}}
}

func playerCopyArrangement(a playerArrangement) playerArrangement {
	b := playerArrangement{Order: append([]string(nil), a.Order...), Width: make(map[string]int, len(a.Width))}
	for name, width := range a.Width {
		b.Width[name] = width
	}
	return b
}

func playerValidateArrangement(layout string, a playerArrangement) error {
	expected := playerDefaultArrangement(layout)
	if layout == playerDiscoveryBrowser {
		for _, id := range a.Order {
			if id == ModeQueue {
				expected.Width[ModeQueue] = expected.Width[playerPaneDetails]
				delete(expected.Width, playerPaneDetails)
				break
			}
		}
	}
	if len(a.Order) != len(expected.Order) || len(a.Width) != len(expected.Order) {
		return fmt.Errorf("invalid pane count for %s", layout)
	}
	seen := make(map[string]bool)
	for _, id := range a.Order {
		if _, ok := expected.Width[id]; !ok || seen[id] {
			return fmt.Errorf("invalid or duplicate pane %q", id)
		}
		seen[id] = true
		if a.Width[id] < playerPaneMinimum(layout, id) || a.Width[id] > 1000 {
			return fmt.Errorf("invalid width for pane %q", id)
		}
	}
	return nil
}

func playerPaneMinimum(layout, id string) int {
	if id == playerPaneDiscover {
		return playerDefaultArrangement(layout).Width[id]
	}
	return 8
}

func playerReadLayouts(path string) (playerLayoutFile, error) {
	return playerReadLayoutsExcept(path, "")
}

func playerReadLayoutsExcept(path, ignoredLayout string) (playerLayoutFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path derives from the user-selected config file.
	if errors.Is(err, os.ErrNotExist) {
		return playerLayoutFile{Version: playerLayoutVersion, Layouts: make(map[string]playerArrangement)}, nil
	}
	if err != nil {
		return playerLayoutFile{}, err
	}
	var file playerLayoutFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return file, fmt.Errorf("invalid player layout: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return file, fmt.Errorf("invalid player layout: multiple documents")
	}
	if file.Version != playerLayoutVersion {
		return file, fmt.Errorf("unsupported player layout version %d", file.Version)
	}
	if file.Layouts == nil {
		return file, errors.New("invalid player layout: missing layouts")
	}
	for layout, arrangement := range file.Layouts {
		if layout != playerListeningDesk && layout != playerDiscoveryBrowser {
			return file, fmt.Errorf("unknown player layout %q", layout)
		}
		if layout == ignoredLayout {
			continue
		}
		if err := playerValidateArrangement(layout, arrangement); err != nil {
			return file, err
		}
	}
	return file, nil
}

// playerSaveLayouts replaces only a fully validated file. A bad or future file
// stays untouched until the user explicitly chooses Reset.
func playerSaveLayouts(path, layout string, arrangement playerArrangement, resetInvalid bool) error {
	if err := playerValidateArrangement(layout, arrangement); err != nil {
		return err
	}
	file, err := playerReadLayouts(path)
	if err != nil {
		if !resetInvalid {
			return err
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return err
		}
		file, err = playerReadLayoutsExcept(path, layout)
		if err != nil {
			if errors.As(err, &pathErr) {
				return err
			}
			file = playerLayoutFile{Version: playerLayoutVersion, Layouts: make(map[string]playerArrangement)}
		}
	}
	file.Layouts[layout] = playerCopyArrangement(arrangement)
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".player-layout-*.yaml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
