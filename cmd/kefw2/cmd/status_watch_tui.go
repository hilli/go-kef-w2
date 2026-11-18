// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type watchStoppedMsg struct{ err error }
type watchArtworkMsg struct {
	key string
	art watchArtwork
	err error
}
type watchArtworkRetryMsg struct{ key string }
type watchArtworkRenderMsg struct {
	version int
	lines   []string
	err     error
}

type watchModel struct {
	ctx           context.Context
	updates       <-chan watchUpdate
	done          <-chan error
	actions       chan<- string
	identity      speakerIdentity
	width, height int
	minimal       bool
	backend       watchArtworkBackend
	snapshot      *statusSnapshot
	readErr       error
	eventErr      error
	lastSuccess   time.Time
	pending       bool
	actionNote    string
	err           error

	artKey        string
	art           watchArtwork
	artLines      []string
	artErr        error
	artCancel     context.CancelFunc
	renderVersion int
}

func runWatchTUI(ctx context.Context, cmd *cobra.Command, deps watchDependencies) error {
	output, ok := cmd.OutOrStdout().(*os.File)
	if !ok || !term.IsTerminal(int(output.Fd())) {
		return fmt.Errorf("status watch requires a terminal; use --json for pipes or files")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := make(chan watchUpdate)
	actions := make(chan string, 1)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- watchStatus(ctx, deps, actions, func(update watchUpdate) error {
			select {
			case updates <- update:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	defer func() { cancel(); <-finished }()
	minimal, _ := cmd.Flags().GetBool("minimal")
	forceASCII, _ := cmd.Flags().GetBool("ascii")
	writer := &watchTerminalWriter{File: output, cancel: cancel}
	model := &watchModel{
		ctx: ctx, updates: updates, actions: actions, done: done,
		identity: identifySpeaker(currentSpeaker), minimal: minimal,
		backend: detectWatchArtworkBackend(forceASCII),
		width:   80, height: 24,
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx),
		tea.WithInput(cmd.InOrStdin()), tea.WithOutput(writer), tea.WithoutSignalHandler())
	_, err := program.Run()
	if model.artCancel != nil {
		model.artCancel()
	}
	if writeErr := writer.failure(); writeErr != nil {
		return fmt.Errorf("writing terminal status: %w", writeErr)
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	return model.err
}

type watchTerminalWriter struct {
	*os.File
	mu     sync.Mutex
	err    error
	cancel context.CancelFunc
}

func (w *watchTerminalWriter) Write(p []byte) (int, error) {
	n, err := w.File.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.mu.Lock()
		if w.err == nil {
			w.err = err
		}
		w.mu.Unlock()
		w.cancel()
	}
	return n, err
}

func (w *watchTerminalWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (m *watchModel) nextUpdate() tea.Msg {
	select {
	case update := <-m.updates:
		return update
	case err := <-m.done:
		return watchStoppedMsg{err}
	case <-m.ctx.Done():
		return watchStoppedMsg{}
	}
}

func (m *watchModel) Init() tea.Cmd { return m.nextUpdate }

func (m *watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case watchStoppedMsg:
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		return m, m.handleKey(msg.String())
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.renderArtwork()
	case watchUpdate:
		var artwork tea.Cmd
		switch msg.kind {
		case watchSnapshotUpdate:
			m.snapshot, m.readErr, m.lastSuccess = &msg.snapshot, nil, time.Now()
			if strings.HasPrefix(m.actionNote, "Action accepted;") {
				m.actionNote = "Status refreshed after action"
			}
			artwork = m.loadArtwork()
		case "status unavailable":
			m.readErr = msg.err
		case "event connection":
			m.eventErr = msg.err
		case watchActionUpdate:
			m.pending = false
			m.actionNote = "Action accepted; waiting for observed state"
			if msg.err != nil {
				m.actionNote = "Action failed: " + msg.err.Error()
			}
		}
		return m, tea.Batch(m.nextUpdate, artwork)
	case watchArtworkMsg:
		return m, m.acceptArtwork(msg)
	case watchArtworkRetryMsg:
		if msg.key == m.artKey && m.artErr != nil {
			m.artKey = ""
			return m, m.loadArtwork()
		}
	case watchArtworkRenderMsg:
		if msg.version == m.renderVersion {
			m.artLines, m.artErr = msg.lines, msg.err
		}
	}
	return m, nil
}

func (m *watchModel) acceptArtwork(msg watchArtworkMsg) tea.Cmd {
	if msg.key != m.artKey {
		return nil
	}
	m.artErr = msg.err
	if msg.err != nil {
		m.art, m.artLines = watchArtwork{}, nil
		m.renderVersion++
		return tea.Tick(watchRecoveryDelay, func(time.Time) tea.Msg {
			return watchArtworkRetryMsg{key: msg.key}
		})
	}
	if msg.art.digest == m.art.digest && m.art.image != nil {
		return nil
	}
	m.art = msg.art
	return m.renderArtwork()
}

func (m *watchModel) handleKey(key string) tea.Cmd {
	if key == "q" || key == KeyCtrlC {
		return tea.Quit
	}
	switch key {
	case " ", "n", "p", "+", "=", "-", "m", watchKeyLeft, "right":
		if m.pending {
			return nil
		}
		if m.snapshot == nil || m.readErr != nil {
			m.actionNote = "Controls unavailable until fresh status is available"
			return nil
		}
		select {
		case m.actions <- key:
			m.pending = true
			m.actionNote = "Sending action..."
		default:
			m.actionNote = "Another action is pending"
		}
	}
	return nil
}

func (m *watchModel) loadArtwork() tea.Cmd {
	if m.minimal || m.snapshot == nil {
		return nil
	}
	p := m.snapshot.Data.Playback
	key, url := "", ""
	if p != nil && p.Track != nil && p.Track.ImageURL != "" {
		url = p.Track.ImageURL
		key = url + "\x00" + m.snapshot.Player.TrackRoles.ID + "\x00" +
			p.Track.Title + "\x00" + p.Track.Artist + "\x00" + p.Track.Album
	}
	if key == m.artKey {
		return nil
	}
	m.artKey, m.artErr = key, nil
	if m.artCancel != nil {
		m.artCancel()
	}
	if url == "" {
		m.art, m.artLines = watchArtwork{}, nil
		m.renderVersion++
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.artCancel = cancel
	return func() tea.Msg {
		art, err := fetchWatchArtwork(ctx, url)
		return watchArtworkMsg{key: key, art: art, err: err}
	}
}

func (m *watchModel) renderArtwork() tea.Cmd {
	m.renderVersion++
	version := m.renderVersion
	width, height := min(m.width, 48), min(12, max(0, m.height-14))
	art, backend := m.art, m.backend
	return func() tea.Msg {
		lines, err := renderWatchArtwork(art, width, height, backend)
		return watchArtworkRenderMsg{version: version, lines: lines, err: err}
	}
}

func (m *watchModel) View() string {
	if m.width < 1 || m.height < 1 {
		return ""
	}
	lines := append([]string(nil), m.artLines...)
	text := []string{fmt.Sprintf("Speaker: %s (%s)", m.identity.Name, m.identity.IPAddress)}
	if m.snapshot == nil {
		text = append(text, "Waiting for speaker status...")
	} else {
		s := m.snapshot.Data
		text = append(text, fmt.Sprintf("Source: %s  Power: %s", s.Source, s.Power),
			fmt.Sprintf("Volume: %d  Muted: %t", s.Volume, s.Muted))
		text = appendWatchPlayback(text, m.snapshot)
	}
	if m.readErr != nil {
		text = append(text, "STALE: "+m.readErr.Error())
		if !m.lastSuccess.IsZero() {
			text = append(text, "Last successful read: "+m.lastSuccess.Format(time.RFC3339))
		}
	}
	if m.eventErr != nil {
		text = append(text, "Events disconnected; polling while reconnecting: "+m.eventErr.Error())
	}
	if m.artErr != nil {
		text = append(text, "Artwork unavailable: "+m.artErr.Error())
	}
	if m.actionNote != "" {
		text = append(text, m.actionNote)
	}
	text = append(text, "Space play/pause | n/p track | +/- volume | m mute | arrows seek | q quit")
	for _, line := range text {
		lines = append(lines, ansi.Truncate(watchText(line), m.width, ""))
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	if m.backend == watchArtworkKitty && len(m.artLines) == 0 {
		lines[0] = clearWatchImage + lines[0]
	}
	return strings.Join(lines, "\n")
}

func appendWatchPlayback(lines []string, snapshot *statusSnapshot) []string {
	p := snapshot.Data.Playback
	if p == nil {
		return lines
	}
	lines = append(lines, "Playback: "+p.State)
	if snapshot.Player.MediaRoles.Title != "" {
		lines = append(lines, "Transport: "+snapshot.Player.MediaRoles.Title)
	}
	if p.Track != nil {
		lines = append(lines, "Artist: "+p.Track.Artist, "Album: "+p.Track.Album, "Track: "+p.Track.Title)
	}
	if p.PositionMS != nil {
		progress := formatDuration(*p.PositionMS)
		if p.DurationMS != nil {
			progress += "/" + formatDuration(*p.DurationMS)
		}
		lines = append(lines, "Progress: "+progress)
	}
	return lines
}

func watchText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
