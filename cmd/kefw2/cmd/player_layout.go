// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	playerPaneSources        = "sources"
	playerPaneDiscover       = "discover"
	playerPaneDetails        = "details"
	playerFocusSpeaker       = "speaker"
	playerLayoutKeyBackspace = "backspace"
	playerLayoutKeyDelete    = "delete"
	playerLayoutKeyMoveDown  = "ctrl+j"
	playerLayoutKeyMoveUp    = "ctrl+k"
	playerLayoutKeyTab       = "tab"
	playerLayoutMove         = "move"
)

type playerLayoutUI struct {
	root                *bloom.Root
	focus               bloom.Focus[string]
	layout              string
	path                string
	width               int
	arrangement         playerArrangement
	preview             playerArrangement
	pane                string
	zoom                string
	menu                bool
	help                bool
	mode                string
	drag                string
	dragBlocked         bool
	dragStart           int
	dragLeftWidth       int
	dragRightWidth      int
	dragBase            playerArrangement
	error               error
	invalid             bool
	reduced             bool
	notices             []playerNoticeMsg
	queue               *playerQueueUI
	discovery           *playerDiscoveryUI
	controls            *playerControlsUI
	controlGeneration   uint64
	hasControlSession   bool
	controlReason       string
	search              textinput.Model
	searchActive        bool
	sourceOpen          bool
	sourceIndex         int
	sourceEpoch         uint64
	sliderDragging      bool
	sliderEpoch         uint64
	sliderSuppressClick bool
	sliderBlocked       bool
	queueIndex          int
	queueClear          *playerQueueClearMsg
	queueDrag           *playerQueueDrag
	queueDragBlocked    bool
	outbound            []tea.Cmd
}

type playerQueueDrag struct {
	generation uint64
	snapshot   *playerQueueSnapshot
	from, gap  int
	count      int
}

func newPlayerLayoutUI(layout, configPath string) *playerLayoutUI {
	ui := &playerLayoutUI{
		layout: layout, path: filepath.Join(filepath.Dir(configPath), "player-layout.yaml"), width: 80,
		arrangement: playerDefaultArrangement(layout), pane: playerPaneDiscover,
	}
	ui.root = bloom.New(bloom.WithFocus(&ui.focus))
	ui.search = textinput.New()
	ui.search.Placeholder = "Search radio or podcasts"
	ui.focus.Set(playerPaneDiscover)
	file, err := playerReadLayouts(ui.path)
	if err != nil {
		ui.error, ui.invalid = err, true
	} else if arrangement, ok := file.Layouts[layout]; ok {
		ui.arrangement = playerCopyArrangement(arrangement)
	}
	return ui
}

func (ui *playerLayoutUI) extension() playerExtension {
	return playerExtension{update: ui.update, reset: func(*playerModel) {
		ui.closeQueueClear()
		ui.queueIndex = 0
		ui.search.Blur()
		ui.searchActive = false
		ui.sourceOpen = false
		ui.sliderDragging = false
		ui.controlReason = ""
		ui.hasControlSession = false
		if ui.queueDrag != nil {
			ui.queueDragBlocked = true
		}
		ui.queueDrag = nil
		ui.cancel()
	}}
}

//nolint:gocyclo // Routes mutually exclusive terminal and modal events.
func (ui *playerLayoutUI) update(m *playerModel, msg tea.Msg) (tea.Cmd, bool) {
	if ui.controls != nil && ui.controls.active != nil {
		ui.controlGeneration = ui.controls.active.generation
		ui.hasControlSession = true
	}
	if ui.controls != nil && ui.sourceOpen && ui.sourceEpoch != ui.controls.State().epoch {
		ui.sourceOpen = false
		ui.focus.Pop()
	}
	if ui.queueClear != nil && ui.queue != nil && !ui.queue.state().Ready {
		ui.closeQueueClear()
	}
	switch msg := msg.(type) {
	case playerNoticeMsg:
		ui.notices = append(ui.notices, msg)
		return nil, true
	case tea.WindowSizeMsg:
		ui.width = msg.Width
		if ui.sliderDragging {
			ui.sliderBlocked, ui.sliderDragging = true, false
		}
		if ui.queueDrag != nil {
			ui.queueDragBlocked = true
		}
		ui.queueDrag = nil
		if ui.drag != "" {
			ui.dragBlocked = true
		}
		ui.cancel()
		return ui.root.Update(msg), false
	case tea.BlurMsg:
		ui.cancelPointerInteractions()
		return ui.root.Update(msg), false
	case tea.MouseMsg:
		if m.chooserOpen || ui.menu || ui.help || ui.sourceOpen || ui.queueClear != nil ||
			(ui.discovery != nil && ui.discovery.presentation().confirmation != nil) {
			if ui.sliderDragging {
				ui.sliderBlocked, ui.sliderDragging = true, false
			}
			if ui.queueDrag != nil {
				ui.queueDragBlocked = true
			}
			ui.queueDrag = nil
			if ui.drag != "" {
				ui.dragBlocked = true
			}
			ui.cancel()
		}
		if motion, ok := msg.(tea.MouseMotionMsg); ok && motion.Button == tea.MouseNone && ui.drag != "" {
			ui.dragBlocked = true
			ui.cancel()
		}
		if motion, ok := msg.(tea.MouseMotionMsg); ok && motion.Button == tea.MouseNone && ui.queueDrag != nil {
			ui.queueDragBlocked = true
			ui.queueDrag = nil
		}
		cmd := ui.root.Update(msg)
		if _, ok := msg.(tea.MouseReleaseMsg); ok {
			ui.dragBlocked = false
			ui.queueDragBlocked = false
			ui.sliderBlocked = false
		}
		return cmd, true
	case tea.KeyPressMsg:
		cmd := ui.root.Update(msg)
		if ui.key(m, msg) {
			commands := append([]tea.Cmd{cmd}, ui.outbound...)
			ui.outbound = nil
			return tea.Batch(commands...), true
		}
		return cmd, false
	default:
		return ui.root.Update(msg), false
	}
}

func (ui *playerLayoutUI) save(resetInvalid bool) {
	if ui.invalid && !resetInvalid {
		return
	}
	if err := playerSaveLayouts(ui.path, ui.layout, ui.arrangement, resetInvalid); err != nil {
		ui.error = fmt.Errorf("save layout: %w", err)
		return
	}
	ui.error, ui.invalid = nil, false
}

func (ui *playerLayoutUI) cancel() {
	if ui.mode != "" || ui.drag != "" {
		ui.preview = playerArrangement{}
		ui.mode, ui.drag = "", ""
	}
}

//nolint:gocyclo // Keeps modal precedence and the shared keymap in one place.
func (ui *playerLayoutUI) key(m *playerModel, key tea.KeyPressMsg) bool {
	if ui.discovery != nil && ui.discovery.presentation().confirmation != nil {
		return ui.discoveryKey(key)
	}
	if ui.sourceOpen {
		return ui.sourceKey(m, key)
	}
	if ui.help {
		if key.Code == tea.KeyEsc || key.String() == "?" {
			ui.help = false
			ui.focus.Pop()
		}
		return true
	}
	if ui.searchActive && ui.discovery != nil && ui.discovery.presentation().service == playerDiscoveryUPnP {
		ui.search.Blur()
		ui.searchActive = false
	}
	if ui.searchActive {
		switch key.Code {
		case tea.KeyEsc:
			ui.search.Blur()
			ui.searchActive = false
			ui.focus.Set(playerPaneDiscover)
		case tea.KeyTab:
			ui.search.Blur()
			ui.searchActive = false
			if key.Mod&tea.ModShift != 0 {
				ui.focus.Set(playerPaneDiscover)
			} else {
				ui.focus.Set("service-radio")
			}
		case tea.KeyEnter:
			if ui.discovery != nil {
				ui.sendScoped(m.generation, playerDiscoverySearchMsg{generation: m.generation, query: ui.search.Value()})
			}
			ui.search.Blur()
			ui.searchActive = false
			ui.focus.Set(playerPaneDiscover)
		default:
			var command tea.Cmd
			ui.search, command = ui.search.Update(key)
			ui.outbound = append(ui.outbound, command)
		}
		return true
	}
	if ui.queueClear != nil {
		switch key.String() {
		case "y":
			request := *ui.queueClear
			request.Confirmed = true
			ui.closeQueueClear()
			ui.send(request)
		case "n", KeyEsc:
			ui.closeQueueClear()
		}
		return true
	}
	if key.Code == tea.KeyEsc {
		if ui.sliderDragging {
			ui.sliderBlocked, ui.sliderDragging = true, false
			return true
		}
		if ui.queueDrag != nil {
			ui.queueDragBlocked = true
			ui.queueDrag = nil
			return true
		}
		if ui.mode != "" || ui.drag != "" {
			if ui.drag != "" {
				ui.dragBlocked = true
			}
			ui.cancel()
			return true
		}
		if ui.menu {
			ui.menu = false
			ui.focus.Pop()
			return true
		}
		if ui.zoom != "" {
			ui.zoom = ""
			return true
		}
	}
	if ui.mode != "" {
		switch key.Code {
		case tea.KeyEnter:
			ui.arrangement = playerCopyArrangement(ui.preview)
			ui.cancel()
			ui.save(false)
		case tea.KeyLeft, tea.KeyRight:
			delta := -1
			if key.Code == tea.KeyRight {
				delta = 1
			}
			ui.adjust(delta)
		}
		return true
	}
	if ui.menu {
		switch key.String() {
		case "m":
			ui.startMode(playerLayoutMove)
		case "r":
			ui.startMode("resize")
		case "z":
			ui.zoomPane()
			ui.menu = false
			ui.focus.Pop()
		case "R":
			ui.arrangement = playerDefaultArrangement(ui.layout)
			ui.menu = false
			ui.focus.Pop()
			ui.save(true)
		case "a":
			ui.reduced = !ui.reduced
		case "d":
			ui.toggleQueueDock()
			ui.menu = false
			ui.focus.Pop()
			ui.focus.Set(ui.pane)
		case "1", "2", "3", "4", "5":
			choices := ui.paneChoices()
			index := int(key.Code - '1')
			if index < len(choices) {
				ui.pane = choices[index]
				ui.menu = false
				ui.focus.Pop()
				ui.focus.Set(ui.pane)
			}
		}
		return true
	}
	if key.Code == tea.KeyEnter && ui.focus.Is(playerFocusSpeaker) {
		m.openSpeakerChooser()
		return true
	}
	if key.Code == tea.KeyEnter && ui.focus.Is("queue-open") {
		ui.toggleQueue()
		return true
	}
	if key.Code == tea.KeyEnter && ui.focus.Is("details-open") {
		ui.pane = playerPaneDetails
		ui.focus.Set(ui.pane)
		return true
	}
	if key.Code == tea.KeyEnter && ui.focus.Is("panes-open") {
		ui.cancelPointerInteractions()
		ui.menu = true
		ui.focus.Push("pane-menu")
		return true
	}
	if key.Code == tea.KeyEnter && ui.discovery != nil {
		focus, _ := ui.focus.Current()
		switch focus {
		case "service-radio", "service-podcast", "service-upnp":
			p := ui.discovery.presentation()
			service := playerDiscoveryService(strings.TrimPrefix(focus, "service-"))
			ui.sendScoped(p.generation, playerDiscoveryServiceMsg{generation: p.generation, service: service})
			return true
		case "search":
			if ui.discovery.presentation().service != playerDiscoveryUPnP {
				ui.searchActive = true
				ui.outbound = append(ui.outbound, ui.search.Focus())
			}
			return true
		}
	}
	if ui.controls != nil && ui.controlsKey(m, key) {
		return true
	}
	focusID, focused := ui.focus.Current()
	paneFocused := !focused || focusID == ui.pane || strings.HasPrefix(focusID, "queue-row-") ||
		strings.HasPrefix(focusID, "discovery-row-")
	if paneFocused && ui.queue != nil && ui.pane == ModeQueue && ui.queueKey(key) {
		return true
	}
	if paneFocused && ui.discovery != nil && ui.pane == playerPaneDiscover && ui.discoveryKey(key) {
		return true
	}
	switch key.String() {
	case "?":
		ui.cancelPointerInteractions()
		ui.help = true
		ui.focus.Push("help")
		return true
	case "/":
		if ui.discovery != nil && ui.discovery.presentation().service != playerDiscoveryUPnP {
			ui.pane, ui.searchActive = playerPaneDiscover, true
			ui.focus.Set("search")
			ui.outbound = append(ui.outbound, ui.search.Focus())
			return true
		}
	case "p":
		if ui.drag != "" {
			ui.dragBlocked = true
		}
		ui.cancel()
		ui.menu = true
		ui.focus.Push("pane-menu")
		return true
	case "z":
		ui.zoomPane()
		return true
	case playerLayoutKeyTab, "shift+tab":
		step := 1
		if key.Mod&tea.ModShift != 0 {
			step = -1
		}
		ids := append([]string{playerFocusSpeaker, "queue-open", "details-open", "panes-open"}, ui.arrangement.Order...)
		if ui.discovery != nil {
			if ui.discovery.presentation().service != playerDiscoveryUPnP {
				ids = append(ids, "search")
			}
			ids = append(ids, "service-radio", "service-podcast", "service-upnp")
		}
		if ui.controls != nil {
			ids = append(ids, "previous", "play-pause", "transport-next", "mute", "seek-back", "seek-next", "volume")
		}
		current, ok := ui.focus.Current()
		if !ok {
			current = ui.pane
		}
		i := slices.Index(ids, current)
		next := ids[(max(i, 0)+len(ids)+step)%len(ids)]
		if slices.Contains(ui.arrangement.Order, next) {
			ui.pane = next
		}
		ui.focus.Set(next)
		if next == "search" {
			ui.pane = playerPaneDiscover
			ui.searchActive = true
			ui.outbound = append(ui.outbound, ui.search.Focus())
		} else if strings.HasPrefix(next, "service-") {
			ui.pane = playerPaneDiscover
		}
		return true
	case "alt+1", "alt+2", "alt+3":
		ui.pane = ui.arrangement.Order[int(key.Code-'1')]
		ui.focus.Set(ui.pane)
		return true
	case "Q":
		ui.toggleQueue()
		return true
	case "i":
		ui.pane = playerPaneDetails
		ui.focus.Set(ui.pane)
		return true
	case KeyEsc:
		if len(ui.notices) > 0 {
			ui.notices = ui.notices[1:]
			return true
		}
	}
	return false
}

func (ui *playerLayoutUI) paneChoices() []string {
	choices := append([]string(nil), ui.arrangement.Order...)
	for _, id := range []string{ModeQueue, playerPaneDetails} {
		if !slices.Contains(choices, id) {
			choices = append(choices, id)
		}
	}
	return choices
}

func (ui *playerLayoutUI) toggleQueue() {
	if ui.layout == playerDiscoveryBrowser && ui.pane == ModeQueue {
		ui.pane = playerPaneDiscover
	} else {
		ui.pane = ModeQueue
	}
	ui.focus.Set(ui.pane)
}

func (ui *playerLayoutUI) toggleQueueDock() {
	if ui.layout != playerDiscoveryBrowser {
		return
	}
	from, to := playerPaneDetails, ModeQueue
	if slices.Contains(ui.arrangement.Order, ModeQueue) {
		from, to = ModeQueue, playerPaneDetails
	}
	index := slices.Index(ui.arrangement.Order, from)
	if index < 0 {
		return
	}
	ui.arrangement.Order[index] = to
	ui.arrangement.Width[to] = ui.arrangement.Width[from]
	delete(ui.arrangement.Width, from)
	ui.pane = to
	ui.save(false)
}

func (ui *playerLayoutUI) sourceKey(m *playerModel, key tea.KeyPressMsg) bool {
	sources := ui.sources()
	switch key.Code {
	case tea.KeyEsc:
		ui.sourceOpen = false
		ui.focus.Pop()
	case tea.KeyUp:
		ui.sourceIndex = max(0, ui.sourceIndex-1)
	case tea.KeyDown:
		ui.sourceIndex = min(len(sources)-1, ui.sourceIndex+1)
	case tea.KeyEnter:
		if ui.controls.State().ready && len(sources) > 0 && sources[ui.sourceIndex].enabled {
			ui.sendScoped(m.generation, playerSourceRequest{
				generation: m.generation, epoch: ui.sourceEpoch, source: sources[ui.sourceIndex].source,
			})
			ui.sourceOpen = false
			ui.focus.Pop()
		}
	}
	return true
}

//nolint:gocyclo // Maps focused controls and global shortcuts independently.
func (ui *playerLayoutUI) controlsKey(m *playerModel, key tea.KeyPressMsg) bool {
	generation := m.generation
	epoch := ui.controls.State().epoch
	if key.Code == tea.KeyEnter {
		id, _ := ui.focus.Current()
		switch id {
		case "source-open":
			ui.openSource()
			return true
		case "previous":
			ui.sendTransport(generation, playerControlPrevious)
			return true
		case "play-pause":
			ui.sendTransport(generation, playerControlPlayPause)
			return true
		case "transport-next":
			ui.sendTransport(generation, playerControlNext)
			return true
		case "mute":
			if ui.transportAllowed(playerControlMute) {
				ui.sendScoped(generation, playerMuteRequest{generation: generation, epoch: epoch})
			}
			return true
		case "seek-back":
			ui.sendTransport(generation, playerControlSeekBackward)
			return true
		case "seek-next":
			ui.sendTransport(generation, playerControlSeekForward)
			return true
		}
	}
	if ui.focus.Is("volume") {
		switch key.String() {
		case watchKeyLeft, playerKeyRight, "up", playerKeyDown, "home", "end":
			ui.sendScoped(generation, playerVolumeKeyRequest{generation: generation, epoch: epoch, key: key.String()})
			return true
		}
	}
	switch key.String() {
	case "s":
		ui.openSource()
	case "m":
		if ui.transportAllowed(playerControlMute) {
			ui.sendScoped(generation, playerMuteRequest{generation: generation, epoch: epoch})
		}
	case "+", "-":
		ui.sendScoped(generation, playerVolumeKeyRequest{generation: generation, epoch: epoch, key: key.String()})
	case " ", "space":
		ui.sendTransport(generation, playerControlPlayPause)
	case "[":
		ui.sendTransport(generation, playerControlPrevious)
	case "]":
		ui.sendTransport(generation, playerControlNext)
	case ",":
		ui.sendTransport(generation, playerControlSeekBackward)
	case ".":
		ui.sendTransport(generation, playerControlSeekForward)
	case "R":
		if !ui.controls.State().volumeCanRetry {
			return false
		}
		ui.sendScoped(generation, playerVolumeRetryRequest{generation: generation, epoch: epoch})
	case "D":
		state := ui.controls.State()
		if state.volumeErr == nil && state.err == nil {
			return false
		}
		ui.sendScoped(generation, playerVolumeDismissRequest{generation: generation, epoch: epoch})
		ui.sendScoped(generation, playerControlDismissErrorRequest{generation: generation, epoch: epoch})
	default:
		return false
	}
	return true
}

func (ui *playerLayoutUI) transportAllowed(action playerControlAction) bool {
	state := ui.controls.State()
	if !state.ready || state.confirmed == nil {
		ui.controlReason = "Speaker status unavailable; control disabled"
		return false
	}
	if err := playerControlUnavailable(*state.confirmed, action); err != nil {
		ui.controlReason = err.Error()
		return false
	}
	ui.controlReason = ""
	return true
}

func (ui *playerLayoutUI) sendTransport(generation uint64, action playerControlAction) {
	if ui.transportAllowed(action) {
		ui.sendScoped(generation, playerTransportRequest{generation: generation, epoch: ui.controls.State().epoch, action: action})
	}
}

func (ui *playerLayoutUI) openSource() {
	ui.cancelPointerInteractions()
	ui.sourceOpen = true
	ui.sourceIndex = 0
	ui.sourceEpoch = ui.controls.State().epoch
	ui.focus.Push("source-chooser")
}

func (ui *playerLayoutUI) cancelPointerInteractions() {
	if ui.drag != "" {
		ui.dragBlocked = true
	}
	ui.cancel()
	if ui.queueDrag != nil {
		ui.queueDragBlocked = true
		ui.queueDrag = nil
	}
	if ui.sliderDragging {
		ui.sliderBlocked, ui.sliderDragging = true, false
	}
}

type playerSourceChoice struct {
	source  kefw2.Source
	enabled bool
}

func (ui *playerLayoutUI) sources() []playerSourceChoice {
	names := []kefw2.Source{
		kefw2.SourceWiFi, kefw2.SourceBluetooth, kefw2.SourceOptical, kefw2.SourceTV,
		kefw2.SourceAux, kefw2.SourceUSB, kefw2.SourceCoaxial,
	}
	choices := make([]playerSourceChoice, 0, len(names))
	for _, source := range names {
		choices = append(choices, playerSourceChoice{source: source, enabled: ui.controls.SupportsSource(source)})
	}
	return choices
}

func (ui *playerLayoutUI) send(message tea.Msg) {
	ui.outbound = append(ui.outbound, func() tea.Msg { return message })
}

func (ui *playerLayoutUI) sendScoped(generation uint64, message tea.Msg) {
	ui.outbound = append(ui.outbound, playerScopedCmd(generation, playerMessage(message)))
}

//nolint:gocyclo // Each key is a distinct queue action.
func (ui *playerLayoutUI) queueKey(key tea.KeyPressMsg) bool {
	state := ui.queue.state()
	ui.queueIndex = max(0, min(ui.queueIndex, len(state.Items)-1))
	if state.Busy || state.Uncertain {
		switch key.String() {
		case "r":
			ui.send(playerQueueLoadMsg{Generation: state.Generation})
		case "a":
			if state.ReviewReady {
				ui.send(playerQueueAcknowledgeMsg{Generation: state.Generation, ReviewID: state.ReviewID})
			}
		default:
			return true
		}
		return true
	}
	switch key.Code {
	case tea.KeyHome:
		ui.queueIndex = 0
		return true
	case tea.KeyEnd:
		ui.queueIndex = max(0, len(state.Items)-1)
		return true
	case tea.KeyPgUp:
		ui.queueIndex = max(0, ui.queueIndex-10)
		return true
	case tea.KeyPgDown:
		ui.queueIndex = min(max(0, len(state.Items)-1), ui.queueIndex+10)
		return true
	}
	if !state.Ready && slices.Contains([]string{KeyEnter, playerLayoutKeyMoveUp, playerLayoutKeyMoveDown, playerLayoutKeyDelete,
		playerLayoutKeyBackspace, "c", "h", "t"}, key.String()) {
		return true
	}
	switch key.String() {
	case "up", "k":
		ui.queueIndex = max(0, ui.queueIndex-1)
	case playerKeyDown, "j":
		ui.queueIndex = min(max(0, len(state.Items)-1), ui.queueIndex+1)
	case KeyEnter:
		ui.send(playerQueuePlayMsg{Generation: state.Generation, Snapshot: state.Snapshot, Index: ui.queueIndex})
	case playerLayoutKeyMoveUp, playerLayoutKeyMoveDown:
		to := ui.queueIndex - 1
		if key.String() == playerLayoutKeyMoveDown {
			to = ui.queueIndex + 1
		}
		if to >= 0 && to < len(state.Items) {
			ui.send(playerQueueMoveMsg{Generation: state.Generation, Snapshot: state.Snapshot,
				From: ui.queueIndex, To: to})
			ui.queueIndex = to
		}
	case playerLayoutKeyDelete, playerLayoutKeyBackspace:
		ui.send(playerQueueRemoveMsg{Generation: state.Generation, Snapshot: state.Snapshot, Index: ui.queueIndex})
	case "c":
		request := playerQueueClearMsg{Generation: state.Generation, Snapshot: state.Snapshot}
		ui.openQueueClear(request)
	case "h":
		ui.send(playerQueueShuffleMsg{Generation: state.Generation, ReadyEpoch: state.ReadyEpoch, Enabled: !state.Mode.Shuffle})
	case "t":
		next := PlayModeOff
		switch state.Mode.Repeat {
		case PlayModeOff:
			next = queueRepeatAll
		case queueRepeatAll:
			next = queueRepeatOne
		}
		ui.send(playerQueueRepeatMsg{Generation: state.Generation, ReadyEpoch: state.ReadyEpoch, Mode: next})
	case "r":
		ui.send(playerQueueLoadMsg{Generation: state.Generation})
	case "a":
		if state.ReviewReady {
			ui.send(playerQueueAcknowledgeMsg{Generation: state.Generation, ReviewID: state.ReviewID})
		}
	default:
		return false
	}
	return true
}

func (ui *playerLayoutUI) zoomPane() {
	if ui.zoom == ui.pane {
		ui.zoom = ""
	} else {
		ui.zoom = ui.pane
	}
}

func (ui *playerLayoutUI) startMode(mode string) {
	ui.mode = mode
	ui.preview = playerCopyArrangement(ui.arrangement)
	ui.menu = false
	ui.focus.Pop()
}

func (ui *playerLayoutUI) adjust(delta int) {
	index := slices.Index(ui.preview.Order, ui.pane)
	if index < 0 {
		return
	}
	if ui.mode == playerLayoutMove {
		next := max(0, min(len(ui.preview.Order)-1, index+delta))
		ui.preview.Order[index], ui.preview.Order[next] = ui.preview.Order[next], ui.preview.Order[index]
	} else {
		if ui.pane == playerPaneDiscover {
			neighbor := index + 1
			if neighbor >= len(ui.preview.Order) {
				neighbor = index - 1
			}
			delta = -delta
			if neighbor >= 0 {
				id := ui.preview.Order[neighbor]
				ui.preview.Width[id] = max(playerPaneMinimum(ui.layout, id),
					min(ui.maximumPaneWidth(id), ui.preview.Width[id]+delta))
			}
		} else {
			ui.preview.Width[ui.pane] = max(playerPaneMinimum(ui.layout, ui.pane),
				min(ui.maximumPaneWidth(ui.pane), ui.preview.Width[ui.pane]+delta))
		}
	}
}

func (ui *playerLayoutUI) maximumPaneWidth(pane string) int {
	available := ui.width
	if ui.width >= 120 {
		available -= len(ui.preview.Order) - 1
		for _, other := range ui.preview.Order {
			if other == pane {
				continue
			}
			if other == playerPaneDiscover {
				available -= playerPaneMinimum(ui.layout, other)
			} else {
				available -= ui.preview.Width[other]
			}
		}
	} else if ui.width >= 90 {
		available -= 1 + playerPaneMinimum(ui.layout, playerPaneDiscover)
	}
	return max(playerPaneMinimum(ui.layout, pane), min(1000, available))
}

func (ui *playerLayoutUI) arrangementView() playerArrangement {
	if ui.mode != "" || ui.drag != "" {
		return ui.preview
	}
	return ui.arrangement
}

func (ui *playerLayoutUI) title(m *playerModel, pane string) bloom.Child {
	marker := "  "
	if ui.pane == pane {
		marker = "> "
	}
	title := marker + strings.ToUpper(pane) + " ::"
	if ui.drag == playerLayoutMove && ui.pane == pane {
		title = marker + "[DROP] " + strings.ToUpper(pane) + " ::"
	}
	node := bloom.Text(title).ID("title-"+pane).Focusable().On(
		on.Click(func() tea.Cmd { ui.pane = pane; ui.focus.Set(pane); return nil }),
		on.DoubleClick(func() tea.Cmd { ui.pane = pane; ui.zoomPane(); return nil }),
	)
	if slices.Contains(ui.arrangement.Order, pane) {
		node = node.On(on.Drag(func(event on.Event) tea.Cmd { return ui.dragTitle(m, pane, event) }))
	}
	return node
}

func (ui *playerLayoutUI) dragTitle(m *playerModel, pane string, e on.Event) tea.Cmd {
	if e.Msg == nil || m.chooserOpen || ui.menu || ui.dragBlocked {
		ui.cancel()
		return nil
	}
	if ui.drag == "" {
		ui.drag, ui.pane, ui.dragStart = playerLayoutMove, pane, e.Start.X
		ui.dragBase = playerCopyArrangement(ui.arrangement)
		ui.preview = playerCopyArrangement(ui.arrangement)
	}
	if ui.drag != playerLayoutMove {
		return nil
	}
	if e.Done {
		if _, ok := e.Msg.(tea.MouseReleaseMsg); !ok {
			ui.cancel()
			return nil
		}
		changed := !slices.Equal(ui.arrangement.Order, ui.preview.Order)
		ui.arrangement = playerCopyArrangement(ui.preview)
		ui.cancel()
		if changed {
			ui.save(false)
		}
		return nil
	}
	for _, other := range ui.preview.Order {
		rect, ok := ui.root.Frame().Rect("title-" + other)
		if ok && other != pane && e.Screen.X >= rect.Min.X && e.Screen.X < rect.Max.X {
			a, b := slices.Index(ui.preview.Order, pane), slices.Index(ui.preview.Order, other)
			ui.preview.Order[a], ui.preview.Order[b] = ui.preview.Order[b], ui.preview.Order[a]
			break
		}
	}
	return nil
}

func (ui *playerLayoutUI) divider(m *playerModel, pane string) bloom.Child {
	return bloom.Text("│").ID("divider-" + pane).On(on.Drag(func(e on.Event) tea.Cmd {
		if e.Msg == nil || m.chooserOpen || ui.menu || ui.dragBlocked {
			ui.cancel()
			return nil
		}
		if ui.drag == "" {
			ui.drag, ui.pane, ui.dragStart = "resize", pane, e.Start.X
			ui.dragBase = playerCopyArrangement(ui.arrangement)
			ui.preview = playerCopyArrangement(ui.arrangement)
			index := slices.Index(ui.dragBase.Order, pane)
			if index < 0 || index+1 >= len(ui.dragBase.Order) {
				ui.cancel()
				return nil
			}
			right := ui.dragBase.Order[index+1]
			leftRect, leftOK := ui.root.Frame().Rect("title-" + pane)
			rightRect, rightOK := ui.root.Frame().Rect("title-" + right)
			if !leftOK || !rightOK {
				ui.cancel()
				return nil
			}
			ui.dragLeftWidth, ui.dragRightWidth = leftRect.Dx(), rightRect.Dx()
		}
		if ui.drag != "resize" {
			return nil
		}
		index := slices.Index(ui.dragBase.Order, pane)
		if index < 0 || index+1 >= len(ui.dragBase.Order) {
			ui.cancel()
			return nil
		}
		right := ui.dragBase.Order[index+1]
		delta := max(playerPaneMinimum(ui.layout, pane)-ui.dragLeftWidth,
			min(ui.dragRightWidth-playerPaneMinimum(ui.layout, right), e.Screen.X-ui.dragStart))
		if pane != playerPaneDiscover {
			ui.preview.Width[pane] = max(playerPaneMinimum(ui.layout, pane), min(1000, ui.dragBase.Width[pane]+delta))
		}
		if right != playerPaneDiscover {
			ui.preview.Width[right] = max(playerPaneMinimum(ui.layout, right), min(1000, ui.dragBase.Width[right]-delta))
		}
		if e.Done {
			if _, ok := e.Msg.(tea.MouseReleaseMsg); !ok {
				ui.cancel()
				return nil
			}
			changed := ui.arrangement.Width[pane] != ui.preview.Width[pane] ||
				ui.arrangement.Width[right] != ui.preview.Width[right]
			ui.arrangement = playerCopyArrangement(ui.preview)
			ui.cancel()
			if changed {
				ui.save(false)
			}
		}
		return nil
	}))
}
