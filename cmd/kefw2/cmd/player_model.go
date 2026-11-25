// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

type playerSpeaker struct {
	IPAddress, Name, Model, FirmwareVersion, MacAddress, ID string
	Configured, Default                                     bool
}

func (s playerSpeaker) newSpeaker() *kefw2.KEFSpeaker {
	return &kefw2.KEFSpeaker{
		IPAddress: s.IPAddress, Name: s.Name, Model: s.Model,
		FirmwareVersion: s.FirmwareVersion, MacAddress: s.MacAddress, ID: s.ID,
	}
}

func playerSpeakerFromConfig(s kefw2.KEFSpeaker, defaultIP string) playerSpeaker {
	return playerSpeaker{
		IPAddress: s.IPAddress, Name: s.Name, Model: s.Model, FirmwareVersion: s.FirmwareVersion,
		MacAddress: s.MacAddress, ID: s.ID, Configured: true, Default: s.IPAddress == defaultIP && defaultIP != "",
	}
}

type playerSelection struct {
	configured []playerSpeaker
	selected   *playerSpeaker
	choose     bool
}

func initialPlayerSelection(
	configured []kefw2.KEFSpeaker, defaultIP, explicit, configuredSelector string,
) (playerSelection, error) {
	selection := playerSelection{configured: make([]playerSpeaker, 0, len(configured))}
	for _, speaker := range configured {
		if speaker.IPAddress != "" {
			selection.configured = append(selection.configured, playerSpeakerFromConfig(speaker, defaultIP))
		}
	}
	target := explicit
	if target == "" {
		target = configuredSelector
	}
	if target != "" {
		for _, speaker := range selection.configured {
			if speaker.IPAddress == target {
				selection.selected = &speaker
				return selection, nil
			}
		}
		for _, speaker := range selection.configured {
			if speaker.Name == target {
				if selection.selected != nil {
					return playerSelection{}, fmt.Errorf("speaker name %q is ambiguous; use an IP address", target)
				}
				selection.selected = &speaker
			}
		}
		if selection.selected == nil {
			selection.selected = &playerSpeaker{IPAddress: target}
		}
		return selection, nil
	}
	for _, speaker := range selection.configured {
		if speaker.Default {
			selection.selected = &speaker
			return selection, nil
		}
	}
	switch len(selection.configured) {
	case 0:
		return playerSelection{}, fmt.Errorf("no speakers configured; use 'kefw2 config speaker discover --save' " +
			"or 'kefw2 config speaker add <ip-address>', or specify --speaker")
	case 1:
		speaker := selection.configured[0]
		selection.selected = &speaker
	default:
		selection.choose = true
	}
	return selection, nil
}

type playerConnection string

const (
	playerConnecting  playerConnection = "Connecting"
	playerReady       playerConnection = "Ready"
	playerUnavailable playerConnection = "Unavailable"

	playerStatusUnavailableUpdate = "status unavailable"
	playerEventConnectionUpdate   = "event connection"

	// A second session can connect while a canceled one is draining. Further
	// switches coalesce until a slot opens.
	playerMaxLiveSessions = 2
	playerShutdownTimeout = 5 * time.Second
)

type playerModel struct {
	ctx        context.Context
	layout     string
	speakers   []playerSpeaker
	target     *playerSpeaker // Immutable until the next generation.
	generation uint64

	session      playerSession
	mutations    *playerMutationGate
	sessions     map[uint64]playerSession
	sessionMaker playerSessionFactory
	pendingStart bool

	statusRefreshSequence uint64
	connection            playerConnection
	snapshot              *statusSnapshot
	readErr               error
	eventErr              error
	lastSuccess           time.Time
	maxVolume             *int
	maxVolumeErr          error
	speakerInfo           *playerSpeaker
	speakerInfoErr        error

	chooserOpen   bool
	chooserIndex  int
	chooserFilter string
	width, height int
	extensions    []playerExtension
	ui            *playerLayoutUI
}

// Extensions run on the Tea loop and are registered before Run or from Update.
// Each receives session-start, session-stop, status, maximum, info and window
// messages after the corresponding model fields change. Other messages and
// keys go to the first extension that handles them; focused text inputs can
// consume S and q, otherwise leave S, q and unavailable-target r to the core.
//
// On session start capture *m.target, m.generation and m.session.context(). Make
// a separate target.newSpeaker() (and client/cache, if needed) per worker.
// Return asynchronous results through playerScopedCmd(generation, cmd), never
// perform network I/O in an extension callback, Update or View. Generation
// validation happens before any result is dispatched to extensions.
//
// Reserve m.mutations on the Tea loop before launching a mutation worker.
// The worker must defer ticket.release(), then wait on ticket.wait(ctx) before
// any preflight or write. Volume coalescers reserve only when a rate-limited
// write actually starts. Never reserve for reads or reuse a gate after switching.
//
// reset must clear speaker-scoped state and cancel workers without waiting.
// stop must cancel any remaining active/retired workers without waiting and
// return their completion channels; shutdown joins those channels with the
// core sessions. Neither callback should issue playback or configuration writes.
type playerExtension struct {
	update func(*playerModel, tea.Msg) (tea.Cmd, bool)
	reset  func(*playerModel)
	stop   func(*playerModel) []<-chan struct{}
}

func (m *playerModel) addExtension(extension playerExtension) {
	m.extensions = append(m.extensions, extension)
}

// playerMutationGate orders complete mutation transactions within one session.
// Reserving is nonblocking and safe for both the Tea loop and volume worker.
type playerMutationGate struct {
	mu      sync.Mutex
	tickets []*playerMutationTicket
}

type playerMutationTicket struct {
	gate     *playerMutationGate
	ready    chan struct{}
	released bool
}

func newPlayerMutationGate() *playerMutationGate {
	return &playerMutationGate{}
}

func (g *playerMutationGate) reserve() *playerMutationTicket {
	g.mu.Lock()
	defer g.mu.Unlock()
	ticket := &playerMutationTicket{gate: g, ready: make(chan struct{})}
	if len(g.tickets) == 0 {
		close(ticket.ready)
	}
	g.tickets = append(g.tickets, ticket)
	return ticket
}

func (t *playerMutationTicket) wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		t.release()
		return err
	}
	select {
	case <-ctx.Done():
		t.release()
		return ctx.Err()
	case <-t.ready:
	}
	if err := ctx.Err(); err != nil {
		t.release()
		return err
	}
	return nil
}

func (t *playerMutationTicket) release() {
	g := t.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.released {
		return
	}
	for i, ticket := range g.tickets {
		if ticket != t {
			continue
		}
		copy(g.tickets[i:], g.tickets[i+1:])
		g.tickets[len(g.tickets)-1] = nil
		g.tickets = g.tickets[:len(g.tickets)-1]
		t.released = true
		if i == 0 && len(g.tickets) > 0 {
			close(g.tickets[0].ready)
		}
		return
	}
}

func newPlayerModel(
	ctx context.Context, layout string, selection playerSelection, factory playerSessionFactory,
) *playerModel {
	model := &playerModel{
		ctx: ctx, layout: layout, speakers: append([]playerSpeaker(nil), selection.configured...),
		sessions: make(map[uint64]playerSession), sessionMaker: factory,
		chooserOpen: selection.choose, width: 80, height: 24,
	}
	if selection.selected != nil {
		target := *selection.selected
		model.target = &target
		model.generation = 1
		model.connection = playerConnecting
		model.pendingStart = true
	}
	return model
}

func (m *playerModel) Init() tea.Cmd {
	if m.ui != nil {
		if m.ui.sliderDragging {
			m.ui.sliderBlocked, m.ui.sliderDragging = true, false
		}
		return tea.Batch(m.ui.root.Init(), m.startCurrentSession())
	}
	return m.startCurrentSession()
}

func (m *playerModel) View() tea.View {
	return playerView(m)
}

type playerSessionMsg struct {
	generation uint64
	message    tea.Msg
}

type playerScopedMsg struct {
	generation uint64
	message    tea.Msg
}

type playerSessionDoneMsg struct{ generation uint64 }
type playerSessionStartedMsg struct {
	generation uint64
	target     playerSpeaker
}
type playerSessionStoppedMsg struct {
	generation uint64
	target     playerSpeaker
	err        error
}
type playerStatusRefreshRequest struct{ generation uint64 }
type playerStatusRefreshCompletedMsg struct {
	generation, sequence uint64
	err                  error
}
type playerOpenChooserMsg struct{}
type playerChooseSpeakerMsg struct{ index int }

func playerScopedCmd(generation uint64, command tea.Cmd) tea.Cmd {
	if command == nil {
		return nil
	}
	return func() tea.Msg {
		msg := command()
		if msg == nil {
			return nil
		}
		return playerScopedMsg{generation: generation, message: msg}
	}
}

func (m *playerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case playerSessionDoneMsg:
		session, exists := m.sessions[msg.generation]
		if !exists {
			return m, nil
		}
		delete(m.sessions, msg.generation)
		var stopped tea.Cmd
		if msg.generation == m.generation && m.session == session {
			session.cancel()
			m.session, m.mutations = nil, nil
			m.connection = playerUnavailable
			if m.readErr == nil {
				m.readErr = errors.New("speaker session stopped; press r to retry")
			}
			stopped = m.notifyExtensions(playerSessionStoppedMsg{
				generation: msg.generation, target: *m.target, err: m.readErr,
			})
		}
		return m, tea.Batch(stopped, m.startCurrentSession())
	case playerSessionMsg:
		if msg.generation != m.generation || m.session == nil {
			return m, nil
		}
		next := waitPlayerMessage(msg.generation, m.session)
		return m, tea.Batch(next, m.updateMessage(msg.message))
	case playerScopedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		return m, m.updateMessage(msg.message)
	case watchUpdate, playerMaxVolumeResult, playerInfoResult, playerSessionFailure, playerStatusRefreshedResult:
		// Speaker results must arrive through a generation-scoped envelope.
		return m, nil
	default:
		return m, m.updateMessage(msg)
	}
}

func (m *playerModel) updateMessage(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case watchUpdate:
		m.acceptWatchUpdate(msg)
		return m.notifyExtensions(msg)
	case playerMaxVolumeResult:
		m.acceptMaximum(msg)
		return m.notifyExtensions(msg)
	case playerInfoResult:
		m.speakerInfoErr = msg.err
		if msg.err == nil {
			info := msg.speaker
			m.speakerInfo = &info
		}
		return m.notifyExtensions(msg)
	case playerSessionFailure:
		m.readErr, m.connection = msg.err, playerUnavailable
		return m.notifyExtensions(msg)
	case playerStatusRefreshRequest:
		m.requestStatusRefresh(msg.generation)
		return nil
	case playerStatusRefreshedResult:
		m.acceptWatchUpdate(msg.update)
		updated := m.notifyExtensions(msg.update)
		completed := m.notifyExtensions(playerStatusRefreshCompletedMsg{
			generation: m.generation, sequence: msg.sequence, err: msg.update.err,
		})
		return tea.Batch(updated, completed)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.notifyExtensions(msg)
	case playerOpenChooserMsg:
		m.openSpeakerChooser()
		return nil
	case playerChooseSpeakerMsg:
		if m.chooserOpen && msg.index >= 0 && msg.index < len(m.speakers) {
			m.closeSpeakerChooser()
			return m.selectSpeaker(m.speakers[msg.index], false)
		}
		return nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	default:
		command, _ := m.dispatchExtensions(msg)
		return command
	}
}

func (m *playerModel) acceptWatchUpdate(msg watchUpdate) {
	switch msg.kind {
	case watchSnapshotUpdate:
		snapshot := msg.snapshot
		m.snapshot, m.readErr, m.lastSuccess = &snapshot, nil, time.Now()
		m.connection = playerReady
	case playerStatusUnavailableUpdate:
		m.readErr = msg.err
		m.connection = playerUnavailable
	case playerEventConnectionUpdate:
		m.eventErr = msg.err
	}
}

func (m *playerModel) acceptMaximum(msg playerMaxVolumeResult) {
	m.maxVolumeErr = msg.err
	if msg.err == nil {
		value := msg.volume
		m.maxVolume = &value
	} else {
		m.maxVolume = nil
	}
}

// requestStatusRefresh only signals the current worker; completion follows a
// fresh watcher read and acknowledges every request through its sequence.
func (m *playerModel) requestStatusRefresh(generation uint64) uint64 {
	if m.session == nil || generation != m.generation || m.session.context().Err() != nil {
		return 0
	}
	m.statusRefreshSequence++
	if m.statusRefreshSequence == 0 {
		m.statusRefreshSequence++
	}
	m.session.refreshStatus(m.statusRefreshSequence)
	return m.statusRefreshSequence
}

func (m *playerModel) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == KeyCtrlC {
		return tea.Quit
	}
	if m.chooserOpen {
		return m.handleChooserKey(msg)
	}
	if command, handled := m.dispatchExtensions(msg); handled {
		return command
	}
	switch key {
	case "q":
		return tea.Quit
	case "S":
		m.openSpeakerChooser()
	case "r":
		if m.target != nil && m.connection == playerUnavailable {
			return m.selectSpeaker(*m.target, true)
		}
	}
	return nil
}

func (m *playerModel) dispatchExtensions(msg tea.Msg) (tea.Cmd, bool) {
	for _, extension := range m.extensions {
		if extension.update == nil {
			continue
		}
		if command, handled := extension.update(m, msg); handled {
			return command, true
		}
	}
	return nil, false
}

func (m *playerModel) notifyExtensions(msg tea.Msg) tea.Cmd {
	var commands []tea.Cmd
	for _, extension := range m.extensions {
		if extension.update != nil {
			command, _ := extension.update(m, msg)
			if command != nil {
				commands = append(commands, command)
			}
		}
	}
	return tea.Batch(commands...)
}

func (m *playerModel) openSpeakerChooser() {
	if m.chooserOpen {
		return
	}
	if m.ui != nil {
		m.ui.focus.Push("speaker-chooser")
		if m.ui.queueDrag != nil {
			m.ui.queueDragBlocked = true
			m.ui.queueDrag = nil
		}
		if m.ui.drag != "" {
			m.ui.dragBlocked = true
		}
		m.ui.cancel()
	}
	m.chooserOpen = true
	m.chooserFilter = ""
	m.chooserIndex = 0
	if m.target != nil {
		for i, speaker := range m.speakers {
			if speaker.IPAddress == m.target.IPAddress {
				m.chooserIndex = i
				break
			}
		}
	}
}

func (m *playerModel) closeSpeakerChooser() {
	m.chooserOpen = false
	if m.ui != nil && m.ui.focus.Scope() == "speaker-chooser" {
		m.ui.focus.Pop()
	}
}

func (m *playerModel) handleChooserKey(key tea.KeyPressMsg) tea.Cmd {
	switch key.Code {
	case tea.KeyEsc:
		m.closeSpeakerChooser()
	case tea.KeyUp:
		m.moveChooser(-1)
	case tea.KeyDown:
		m.moveChooser(1)
	case tea.KeyTab:
		step := 1
		if key.Mod&tea.ModShift != 0 {
			step = -1
		}
		m.moveChooser(step)
	case tea.KeyEnter:
		if len(m.chooserMatches()) > 0 {
			m.closeSpeakerChooser()
			return m.selectSpeaker(m.speakers[m.chooserIndex], false)
		}
	case tea.KeyBackspace:
		runes := []rune(m.chooserFilter)
		if len(runes) > 0 {
			m.chooserFilter = string(runes[:len(runes)-1])
			m.resetChooserMatch()
		}
	default:
		text := key.Text
		if text == "" && key.Mod == 0 && key.Code > 0 && unicode.IsPrint(key.Code) {
			text = string(key.Code)
		}
		if key.Mod & ^tea.ModShift == 0 && text != "" && strings.IndexFunc(text, func(r rune) bool {
			return !unicode.IsPrint(r)
		}) == -1 {
			m.chooserFilter += text
			m.resetChooserMatch()
		}
	}
	return nil
}

func (m *playerModel) moveChooser(step int) {
	matches := m.chooserMatches()
	if len(matches) == 0 {
		return
	}
	for i, index := range matches {
		if index == m.chooserIndex {
			m.chooserIndex = matches[(i+len(matches)+step)%len(matches)]
			return
		}
	}
	m.chooserIndex = matches[0]
}

func (m *playerModel) chooserMatches() []int {
	filter := strings.ToLower(m.chooserFilter)
	var matches []int
	for i, speaker := range m.speakers {
		if strings.Contains(strings.ToLower(speaker.Name+" "+speaker.Model+" "+speaker.IPAddress), filter) {
			matches = append(matches, i)
		}
	}
	return matches
}

func (m *playerModel) resetChooserMatch() {
	matches := m.chooserMatches()
	if len(matches) > 0 {
		m.chooserIndex = matches[0]
	}
}

func (m *playerModel) selectSpeaker(target playerSpeaker, force bool) tea.Cmd {
	if !force && m.target != nil && m.target.IPAddress == target.IPAddress {
		return nil
	}
	if m.session != nil {
		m.session.cancel()
		m.session = nil
	}
	m.mutations = nil
	m.generation++
	m.target = &target
	m.connection = playerConnecting
	m.snapshot, m.readErr, m.eventErr = nil, nil, nil
	m.lastSuccess = time.Time{}
	m.maxVolume, m.maxVolumeErr = nil, nil
	m.speakerInfo, m.speakerInfoErr = nil, nil
	for _, extension := range m.extensions {
		if extension.reset != nil {
			extension.reset(m)
		}
	}
	m.pendingStart = true
	return m.startCurrentSession()
}

func (m *playerModel) startCurrentSession() tea.Cmd {
	if !m.pendingStart || m.target == nil || len(m.sessions) >= playerMaxLiveSessions || m.ctx.Err() != nil {
		return nil
	}
	generation := m.generation
	target := *m.target
	session := m.sessionMaker(m.ctx, target, generation)
	m.session, m.sessions[generation], m.pendingStart = session, session, false
	m.mutations = newPlayerMutationGate()
	started := m.notifyExtensions(playerSessionStartedMsg{generation: generation, target: target})
	return tea.Batch(waitPlayerMessage(generation, session), waitPlayerDone(generation, session), started)
}

func waitPlayerMessage(generation uint64, session playerSession) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-session.messages():
			if !ok {
				return nil
			}
			return playerSessionMsg{generation: generation, message: msg}
		case <-session.context().Done():
			return nil
		}
	}
}

func waitPlayerDone(generation uint64, session playerSession) tea.Cmd {
	return func() tea.Msg {
		<-session.done()
		return playerSessionDoneMsg{generation: generation}
	}
}

func (m *playerModel) shutdown() error {
	var done []<-chan struct{}
	for _, session := range m.sessions {
		session.cancel()
		done = append(done, session.done())
	}
	for _, extension := range m.extensions {
		if extension.stop != nil {
			done = append(done, extension.stop(m)...)
		}
	}
	timer := time.NewTimer(playerShutdownTimeout)
	defer timer.Stop()
	for _, finished := range done {
		if finished == nil {
			continue
		}
		select {
		case <-finished:
		case <-timer.C:
			return fmt.Errorf("timed out waiting for player sessions to stop")
		}
	}
	return nil
}
