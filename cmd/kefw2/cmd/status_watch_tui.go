// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
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
	art     watchArtworkRendering
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
	terminal      *watchArtworkWriter
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
	artWriter := &watchArtworkWriter{output: output, cancel: cancel}
	writer := &watchTerminalWriter{File: output, watchArtworkWriter: artWriter}
	model := &watchModel{
		ctx: ctx, updates: updates, actions: actions, done: done,
		identity: identifySpeaker(currentSpeaker), minimal: minimal,
		backend: detectWatchArtworkBackend(forceASCII),
		width:   80, height: 24,
		terminal: artWriter,
	}
	program := tea.NewProgram(model, tea.WithContext(ctx),
		tea.WithInput(cmd.InOrStdin()), tea.WithOutput(writer), tea.WithoutSignalHandler())
	_, err := program.Run()
	if model.artCancel != nil {
		model.artCancel()
	}
	artWriter.invalidate()
	if model.backend == watchArtworkKitty && !model.minimal {
		_, _ = artWriter.Write([]byte(clearWatchImage))
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
	*watchArtworkWriter
}

func (w *watchTerminalWriter) Write(p []byte) (int, error) {
	return w.watchArtworkWriter.Write(p)
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
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "space" {
			key = " "
		}
		return m, m.handleKey(key)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.backend != watchArtworkASCII && m.terminal != nil {
			m.terminal.invalidate()
		}
		return m, m.renderArtwork()
	case watchUpdate:
		return m, tea.Batch(m.nextUpdate, m.acceptUpdate(msg))
	case watchArtworkMsg:
		return m, m.acceptArtwork(msg)
	case watchArtworkRetryMsg:
		if msg.key == m.artKey && m.artErr != nil {
			m.artKey = ""
			return m, m.loadArtwork()
		}
	case watchArtworkRenderMsg:
		return m, m.acceptArtworkRender(msg)
	}
	return m, nil
}

func (m *watchModel) acceptUpdate(msg watchUpdate) tea.Cmd {
	switch msg.kind {
	case watchSnapshotUpdate:
		m.snapshot, m.readErr, m.lastSuccess = &msg.snapshot, nil, time.Now()
		if strings.HasPrefix(m.actionNote, "Action accepted;") {
			m.actionNote = "Status refreshed after action"
		}
		return m.loadArtwork()
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
	return nil
}

func (m *watchModel) acceptArtworkRender(msg watchArtworkRenderMsg) tea.Cmd {
	if msg.version != m.renderVersion {
		return nil
	}
	hadArtwork := len(m.artLines) > 0
	m.artLines, m.artErr = msg.art.lines, msg.err
	if msg.err == nil && msg.art.native != "" {
		marker := m.terminal.queue(msg.art.native)
		return tea.Sequence(tea.Raw(marker), tea.ClearScreen)
	}
	if hadArtwork && m.backend != watchArtworkASCII && len(m.artLines) == 0 {
		return m.clearArtwork()
	}
	return nil
}

func (m *watchModel) acceptArtwork(msg watchArtworkMsg) tea.Cmd {
	if msg.key != m.artKey {
		return nil
	}
	m.artErr = msg.err
	if msg.err != nil {
		hadArtwork := len(m.artLines) > 0
		m.art, m.artLines = watchArtwork{}, nil
		m.renderVersion++
		retry := tea.Tick(watchRecoveryDelay, func(time.Time) tea.Msg {
			return watchArtworkRetryMsg{key: msg.key}
		})
		if hadArtwork {
			return tea.Batch(m.clearArtwork(), retry)
		}
		return retry
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
		hadArtwork := len(m.artLines) > 0
		m.art, m.artLines = watchArtwork{}, nil
		m.renderVersion++
		if hadArtwork {
			return m.clearArtwork()
		}
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
		rendered, err := renderWatchArtwork(art, width, height, backend)
		return watchArtworkRenderMsg{version: version, art: rendered, err: err}
	}
}

func (m *watchModel) clearArtwork() tea.Cmd {
	if m.backend == watchArtworkASCII {
		return nil
	}
	m.terminal.invalidate()
	if m.backend == watchArtworkKitty {
		return tea.Sequence(tea.Raw(clearWatchImage), tea.ClearScreen)
	}
	return tea.ClearScreen
}

func (m *watchModel) View() tea.View {
	if m.width < 1 || m.height < 1 {
		return tea.View{AltScreen: true}
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
	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true
	return view
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
