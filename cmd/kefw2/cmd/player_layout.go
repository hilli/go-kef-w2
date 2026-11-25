// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"
	"image"
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
	playerFocusPlayPause     = "play-pause"
	playerLayoutKeyBackspace = "backspace"
	playerLayoutKeyDelete    = "delete"
	playerLayoutKeyMoveDown  = "ctrl+j"
	playerLayoutKeyMoveUp    = "ctrl+k"
	playerLayoutKeyTab       = "tab"
	playerLayoutKeyShiftTab  = "shift+tab"
	playerLayoutMove         = "move"
	playerLayoutResize       = "resize"
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
	settings            bool
	settingsLibrary     bool
	settingsIndex       int
	settingsServer      int
	palette             bool
	paletteSearch       textinput.Model
	paletteIndex        int
	mode                string
	drag                string
	dragBlocked         bool
	dragStart           int
	dragLeftWidth       int
	dragRightWidth      int
	dragRightPane       string
	dragBase            playerArrangement
	dragPane            string
	dragTargets         []playerPaneDragTarget
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
	sourceNavIndex      int
	sliderDragging      bool
	sliderEpoch         uint64
	sliderSuppressClick bool
	sliderBlocked       bool
	queueIndex          int
	queueClear          *playerQueueClearMsg
	queueConfirmAccept  bool
	discoveryConfirm    *playerDiscoveryConfirmation
	discoveryAccept     bool
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

type playerPaneDragTarget struct {
	pane   string
	bounds image.Rectangle
}

func (ui *playerLayoutUI) pointerModal(m *playerModel) bool {
	return m.chooserOpen || ui.menu || ui.help || ui.settings || ui.palette || ui.sourceOpen || ui.queueClear != nil ||
		(ui.discovery != nil && ui.discovery.presentation().confirmation != nil)
}

func newPlayerLayoutUI(layout, configPath string) *playerLayoutUI {
	ui := &playerLayoutUI{
		layout: layout, path: filepath.Join(filepath.Dir(configPath), "player-layout.yaml"), width: 80,
		arrangement: playerDefaultArrangement(layout), pane: playerPaneDiscover,
	}
	ui.root = bloom.New(bloom.WithFocus(&ui.focus))
	ui.search = textinput.New()
	ui.search.Prompt = ""
	ui.search.Placeholder = "Search radio or podcasts"
	styles := ui.search.Styles()
	styles.Focused.Text = playerSelectionStyle(false, true)
	styles.Blurred.Text = playerSelectionStyle(false, true)
	styles.Focused.Placeholder = playerHintStyle
	styles.Blurred.Placeholder = playerHintStyle
	ui.search.SetStyles(styles)
	ui.paletteSearch = textinput.New()
	ui.paletteSearch.Prompt = ""
	ui.paletteSearch.Placeholder = "Type a command…"
	ui.paletteSearch.SetStyles(styles)
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
		ui.closeSettings()
		ui.closePalette()
		ui.sourceNavIndex = 0
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
		if ui.pointerModal(m) {
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
		if ui.palette {
			ui.updatePaletteInput(msg)
			return tea.Batch(ui.root.Update(msg), ui.takeOutbound()), false
		}
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
		ui.dragPane, ui.dragRightPane, ui.dragTargets = "", "", nil
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
	if ui.queueClear != nil {
		if decided, accept := playerConfirmationKey(key, &ui.queueConfirmAccept); decided {
			request := *ui.queueClear
			ui.closeQueueClear()
			if accept {
				request.Confirmed = true
				ui.send(request)
			}
		}
		return true
	}
	if ui.palette {
		return ui.paletteKey(m, key)
	}
	if playerPaletteShortcut(key) {
		ui.openPalette()
		return true
	}
	if ui.settings {
		return ui.settingsKey(m, key)
	}
	if ui.searchActive && ui.discovery != nil && !ui.discovery.presentation().searchable() {
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
			ui.startMode(playerLayoutResize)
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
		case "b":
			ui.swapBottomPanes()
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
	if key.Code == tea.KeyEnter && ui.focus.Is("settings-open") {
		ui.openSettings()
		return true
	}
	if key.Code == tea.KeyEnter && ui.focus.Is("commands-open") {
		ui.openPalette()
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
			if ui.discovery.presentation().searchable() {
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
		strings.HasPrefix(focusID, "discovery-row-") || strings.HasPrefix(focusID, "source-nav-")
	if paneFocused && ui.pane == playerPaneSources && ui.sourceNavKey(m, key) {
		return true
	}
	if paneFocused && ui.queue != nil && ui.pane == ModeQueue && ui.queueKey(key) {
		return true
	}
	if paneFocused && ui.discovery != nil && ui.pane == playerPaneDiscover && ui.discoveryKey(key) {
		return true
	}
	switch key.String() {
	case "ctrl+,":
		ui.openSettings()
		return true
	case "?":
		ui.cancelPointerInteractions()
		ui.help = true
		ui.focus.Push("help")
		return true
	case "/":
		if ui.discovery != nil && ui.discovery.presentation().searchable() {
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
	case playerLayoutKeyTab, playerLayoutKeyShiftTab:
		step := 1
		if key.Mod&tea.ModShift != 0 {
			step = -1
		}
		ids := append([]string{playerFocusSpeaker, "queue-open", "settings-open", "commands-open"}, ui.arrangement.Order...)
		if ui.discovery != nil {
			if ui.discovery.presentation().searchable() {
				ids = append(ids, "search")
			}
			ids = append(ids, "service-radio", "service-podcast", "service-upnp")
		}
		if ui.controls != nil {
			ids = append(ids, string(playerControlPrevious), playerFocusPlayPause, "transport-next", "seek-back", "seek-next")
			state := ui.controls.State()
			if state.err != nil {
				ids = append(ids, "controls-dismiss")
			}
			ids = append(ids, "mute", "volume")
			if state.volumeCanRetry {
				ids = append(ids, "volume-retry")
			}
			if state.volumeErr != nil {
				ids = append(ids, "volume-dismiss")
			}
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

func playerConfirmationKey(key tea.KeyPressMsg, accept *bool) (bool, bool) {
	switch key.String() {
	case playerLayoutKeyTab, playerLayoutKeyShiftTab:
		*accept = !*accept
	case watchKeyLeft:
		*accept = true
	case playerKeyRight:
		*accept = false
	case KeyEnter:
		return true, *accept
	case "y":
		return true, true
	case "n", KeyEsc:
		return true, false
	}
	return false, false
}

func playerConfirmationLabel(label string, selected bool) string {
	if selected {
		return ">[" + label + "]"
	}
	return " [" + label + "]"
}

func (ui *playerLayoutUI) syncDiscoveryConfirmation() {
	if ui.discoveryConfirm != ui.discovery.confirmation {
		ui.discoveryConfirm = ui.discovery.confirmation
		ui.discoveryAccept = false
	}
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
		case string(playerControlPrevious):
			ui.sendTransport(generation, playerControlPrevious)
			return true
		case playerFocusPlayPause:
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
		case "controls-dismiss":
			if ui.controls.State().err != nil {
				ui.sendScoped(generation, playerControlDismissErrorRequest{generation: generation, epoch: epoch})
			}
			return true
		case "volume-retry":
			if ui.controls.State().volumeCanRetry {
				ui.sendScoped(generation, playerVolumeRetryRequest{generation: generation, epoch: epoch})
			}
			return true
		case "volume-dismiss":
			if ui.controls.State().volumeErr != nil {
				ui.sendScoped(generation, playerVolumeDismissRequest{generation: generation, epoch: epoch})
			}
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
		kefw2.SourceAux, kefw2.SourceUSB, kefw2.SourceCoaxial, kefw2.SourceStandby,
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
	if (state.Busy || state.Loading) && (key.String() == "r" || key.String() == "a") {
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
	if (!state.Ready || state.Busy || state.Loading || state.Uncertain) &&
		slices.Contains([]string{KeyEnter, playerLayoutKeyMoveUp, playerLayoutKeyMoveDown, playerLayoutKeyDelete,
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

func (ui *playerLayoutUI) paneActive(pane string) bool {
	return ui.pane == pane && ui.bottomFocus() == ""
}

func (ui *playerLayoutUI) title(m *playerModel, pane string) bloom.Child {
	title := ":: " + strings.ToUpper(pane)
	shortcut := "i"
	if index := slices.Index(ui.arrangement.Order, pane); index >= 0 {
		shortcut = fmt.Sprintf("Alt+%d", index+1)
	} else if pane == ModeQueue {
		shortcut = "Q"
	}
	title += " " + shortcut
	if ui.drag == playerLayoutMove && ui.dragPane == ui.layoutPane(pane) {
		title = ":: MOVE " + strings.ToUpper(pane)
	}
	style := playerTitleStyle(ui.paneActive(pane))
	node := bloom.Text(title).Style(style).Truncate().ID("title-"+pane).Focusable().On(
		on.Click(func() tea.Cmd { ui.pane = pane; ui.focus.Set(pane); return nil }),
		on.DoubleClick(func() tea.Cmd { ui.pane = pane; ui.zoomPane(); return nil }),
	)
	if slices.Contains(ui.arrangement.Order, ui.layoutPane(pane)) {
		node = node.On(on.Drag(func(event on.Event) tea.Cmd { return ui.dragTitle(m, pane, event) }))
	}
	return node
}

func (ui *playerLayoutUI) dragTitle(m *playerModel, pane string, e on.Event) tea.Cmd {
	if e.Msg == nil || ui.pointerModal(m) || ui.dragBlocked {
		ui.cancel()
		return nil
	}
	if ui.drag == "" && !ui.beginTitleDrag(pane) {
		return nil
	}
	if ui.drag != playerLayoutMove || ui.dragPane != ui.layoutPane(pane) {
		return nil
	}
	ui.preview = playerCopyArrangement(ui.dragBase)
	for _, target := range ui.dragTargets {
		if !e.Screen.In(target.bounds) {
			continue
		}
		order := ui.preview.Order
		if pane == playerBottomNowPlaying || pane == playerBottomVolume {
			order = ui.preview.BottomOrder
		}
		from, to := slices.Index(order, ui.dragPane), slices.Index(order, target.pane)
		if from < to {
			copy(order[from:to], order[from+1:to+1])
		} else {
			copy(order[to+1:from+1], order[to:from])
		}
		order[to] = ui.dragPane
		break
	}
	if e.Done {
		if _, ok := e.Msg.(tea.MouseReleaseMsg); !ok {
			ui.cancel()
			return nil
		}
		changed := !slices.Equal(ui.arrangement.Order, ui.preview.Order) ||
			!slices.Equal(ui.arrangement.BottomOrder, ui.preview.BottomOrder)
		ui.arrangement = playerCopyArrangement(ui.preview)
		ui.cancel()
		if changed {
			ui.save(false)
		}
		return nil
	}
	return nil
}

func (ui *playerLayoutUI) layoutPane(pane string) string {
	if pane == playerPaneDetails && slices.Contains(ui.arrangement.Order, ModeQueue) {
		return ModeQueue
	}
	if pane == ModeQueue && slices.Contains(ui.arrangement.Order, playerPaneDetails) {
		return playerPaneDetails
	}
	return pane
}

func (ui *playerLayoutUI) paneBounds(pane string) (image.Rectangle, bool) {
	if pane == playerBottomNowPlaying || pane == playerBottomVolume {
		return ui.root.Frame().Rect(pane + "-pane")
	}
	if rect, ok := ui.root.Frame().Rect("pane-" + pane); ok {
		return rect, true
	}
	if pane == ModeQueue {
		return ui.root.Frame().Rect("pane-" + playerPaneDetails)
	}
	if pane == playerPaneDetails {
		return ui.root.Frame().Rect("pane-" + ModeQueue)
	}
	return image.Rectangle{}, false
}

func (ui *playerLayoutUI) beginTitleDrag(pane string) bool {
	order := ui.arrangement.Order
	bottom := pane == playerBottomNowPlaying || pane == playerBottomVolume
	if bottom {
		order = ui.arrangement.BottomOrder
	}
	var targets []playerPaneDragTarget
	for _, id := range order {
		if rect, ok := ui.paneBounds(id); ok && !rect.Empty() {
			targets = append(targets, playerPaneDragTarget{pane: id, bounds: rect})
		}
	}
	if len(targets) < 2 {
		return false
	}
	// Preview reflow must not move the drop targets under the pointer.
	ui.dragTargets = targets
	ui.drag, ui.dragPane = playerLayoutMove, ui.layoutPane(pane)
	ui.dragBase = playerCopyArrangement(ui.arrangement)
	ui.preview = playerCopyArrangement(ui.arrangement)
	if bottom {
		focus := playerBottomVolume
		if pane == playerBottomNowPlaying {
			focus = playerFocusPlayPause
		}
		ui.focusBottom(focus)
	} else {
		ui.search.Blur()
		ui.searchActive = false
		ui.pane = pane
		ui.focus.Set(pane)
	}
	return true
}

func (ui *playerLayoutUI) divider(m *playerModel, pane, right string) bloom.Child {
	return bloom.Text("│").Style(playerHintStyle).ID("divider-" + pane).On(on.Drag(func(e on.Event) tea.Cmd {
		if e.Msg == nil || ui.pointerModal(m) || ui.dragBlocked {
			ui.cancel()
			return nil
		}
		if ui.drag == "" {
			ui.drag, ui.dragStart = playerLayoutResize, e.Start.X
			ui.dragRightPane = right
			ui.dragBase = playerCopyArrangement(ui.arrangement)
			ui.preview = playerCopyArrangement(ui.arrangement)
			leftRect, leftOK := ui.paneBounds(pane)
			rightRect, rightOK := ui.paneBounds(right)
			if !leftOK || !rightOK {
				ui.cancel()
				return nil
			}
			ui.dragLeftWidth, ui.dragRightWidth = leftRect.Dx()-2, rightRect.Dx()-2
		}
		if ui.drag != playerLayoutResize {
			return nil
		}
		right := ui.dragRightPane
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
