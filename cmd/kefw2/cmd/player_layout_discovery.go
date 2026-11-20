// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"errors"
	"fmt"
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/hilli/go-kef-w2/kefw2"
)

//nolint:gocyclo // Routes independent discovery and modal actions.
func (ui *playerLayoutUI) discoveryKey(key tea.KeyPressMsg) bool {
	p := ui.discovery.presentation()
	if p.confirmation != nil {
		ui.syncDiscoveryConfirmation()
		if decided, accept := playerConfirmationKey(key, &ui.discoveryAccept); decided {
			ui.sendScoped(p.generation, playerDiscoveryConfirmMsg{
				generation: p.generation, request: p.confirmation.request, accept: accept,
			})
		}
		return true
	}
	request := p.state.request
	index := p.selection
	switch key.Code {
	case tea.KeyHome:
		index = 0
	case tea.KeyEnd:
		index = len(p.state.page.items) - 1
	case tea.KeyPgUp:
		index = max(0, index-10)
	case tea.KeyPgDown:
		index = min(len(p.state.page.items)-1, index+10)
	default:
		index = -2
	}
	if index >= 0 {
		ui.sendScoped(p.generation, playerDiscoverySelectMsg{generation: p.generation, request: request, index: index})
		return true
	}
	if index == -1 {
		return true
	}
	index = p.selection
	switch key.String() {
	case "1", "2", "3":
		service := []playerDiscoveryService{playerDiscoveryRadio, playerDiscoveryPodcast, playerDiscoveryUPnP}[int(key.Code-'1')]
		ui.sendScoped(p.generation, playerDiscoveryServiceMsg{generation: p.generation, service: service})
	case "up", "k":
		index = max(0, index-1)
		ui.sendScoped(p.generation, playerDiscoverySelectMsg{generation: p.generation, request: request, index: index})
	case playerKeyDown, "j":
		index = min(max(0, len(p.state.page.items)-1), index+1)
		ui.sendScoped(p.generation, playerDiscoverySelectMsg{generation: p.generation, request: request, index: index})
	case KeyEnter:
		if index >= 0 && index < len(p.state.page.items) {
			if playerDiscoveryIsBrowsable(p, p.state.page.items[index]) {
				ui.sendScoped(p.generation, playerDiscoveryBrowseMsg{generation: p.generation, request: request, index: index})
			} else if p.service != playerDiscoveryUPnP && p.writeReady {
				ui.sendScoped(p.generation, playerDiscoveryPlayMsg{generation: p.generation, request: request, index: index})
			}
		}
	case ">":
		if p.state.page.hasMore {
			ui.sendScoped(p.generation, playerDiscoveryPageMsg{
				generation: p.generation, request: request, offset: p.state.page.nextOffset,
			})
		}
	case "<":
		if p.hasPrevious {
			ui.sendScoped(p.generation, playerDiscoveryPageMsg{
				generation: p.generation, request: request, offset: p.previousOffset,
			})
		}
	case playerLayoutKeyBackspace:
		ui.sendScoped(p.generation, playerDiscoveryBackMsg{generation: p.generation, request: request})
	case "f":
		if p.service == playerDiscoveryUPnP || !p.writeReady {
			return true
		}
		ui.sendScoped(p.generation, playerDiscoveryFavoriteMsg{
			generation: p.generation, request: request, index: index, add: true,
		})
	case "F":
		if p.service == playerDiscoveryUPnP || !p.writeReady {
			return true
		}
		ui.sendScoped(p.generation, playerDiscoveryFavoriteMsg{
			generation: p.generation, request: request, index: index, add: false,
		})
	case "e":
		if p.service != playerDiscoveryUPnP {
			return true
		}
		if playerDiscoveryCanEnqueue(p, index) {
			ui.sendScoped(p.generation, playerDiscoveryEnqueueMsg{generation: p.generation, request: request, index: index})
		}
	case "v":
		if p.service == playerDiscoveryUPnP {
			return true
		}
		ui.sendScoped(p.generation, playerDiscoveryFavoritesMsg{generation: p.generation})
	case "r":
		if !p.canRetry {
			return false
		}
		ui.sendScoped(p.generation, playerDiscoveryRetryMsg{generation: p.generation, request: request})
	default:
		return false
	}
	return true
}

func (ui *playerLayoutUI) discoveryView(m *playerModel) bloom.Child {
	p := ui.discovery.presentation()
	generation, request := p.generation, p.state.request
	services := bloom.Row(
		ui.discoveryServiceButton("1 Radio", playerDiscoveryRadio, generation),
		ui.discoveryServiceButton("2 Podcasts", playerDiscoveryPodcast, generation),
		ui.discoveryServiceButton("3 UPnP", playerDiscoveryUPnP, generation),
	)
	children := []bloom.Child{
		bloom.Fixed(1, ui.title(m, playerPaneDiscover)), bloom.Fixed(1, services),
	}
	if ui.layout == playerListeningDesk {
		children = append(children, bloom.Fixed(1, ui.discoverySearch()))
	}
	children = append(children, playerDiscoveryStatusChildren(p)...)
	if !p.writeReady {
		children = append(children, bloom.Fixed(1, bloom.Text("Writes disabled: awaiting fresh speaker status")))
	}
	children = append(children, bloom.Fixed(2, ui.discoveryActions(p)))
	rows := max(1, m.height-max(3, m.height/3)-13)
	start := max(0, p.selection-rows+1)
	for index := start; index < len(p.state.page.items) && index < start+rows; index++ {
		item := p.state.page.items[index]
		rowIndex := index
		marker := "  "
		if index == p.selection {
			marker = "> "
		}
		node := bloom.Text(marker+watchText(item.Title)).
			Style(playerSelectionStyle(index == p.selection, ui.paneActive(playerPaneDiscover))).
			ID(fmt.Sprintf("discovery-row-%d", index)).Focusable().On(
			on.Click(func() tea.Cmd {
				ui.pane = playerPaneDiscover
				return playerScopedCmd(generation, playerMessage(playerDiscoverySelectMsg{
					generation: generation, request: request, index: rowIndex,
				}))
			}),
			on.DoubleClick(func() tea.Cmd {
				if playerDiscoveryIsBrowsable(p, item) {
					return playerScopedCmd(generation, playerMessage(playerDiscoveryBrowseMsg{
						generation: generation, request: request, index: rowIndex,
					}))
				}
				if p.service == playerDiscoveryUPnP || !p.writeReady {
					return nil
				}
				return playerScopedCmd(generation, playerMessage(playerDiscoveryPlayMsg{
					generation: generation, request: request, index: rowIndex,
				}))
			}),
		)
		children = append(children, bloom.Fixed(1, node))
	}
	children = append(children, bloom.Fill(bloom.Text("")))
	help := "Enter play/open · f/F favorite · < > pages · v favorites"
	if p.service == playerDiscoveryUPnP {
		help = "Enter browse · e enqueue · play from Queue · < > pages"
	}
	children = append(children, bloom.Fixed(1, playerHint(help)))
	return bloom.Column(children...).On(on.Scroll(func(_, dy int) tea.Cmd {
		if len(p.state.page.items) == 0 {
			return nil
		}
		index := max(0, min(len(p.state.page.items)-1, p.selection+dy))
		ui.pane = playerPaneDiscover
		return playerScopedCmd(generation, playerMessage(playerDiscoverySelectMsg{
			generation: generation, request: request, index: index,
		}))
	}))
}

func playerDiscoveryStatusChildren(p playerDiscoveryPresentation) []bloom.Child {
	if p.action != nil && p.action.err != nil {
		label := "FAILED"
		if p.action.uncertain {
			label = "UNCERTAIN"
		}
		status := fmt.Sprintf("%s: %s (%s)", label, p.action.action, p.action.target.Name)
		cause := p.action.err
		for errors.Unwrap(cause) != nil {
			cause = errors.Unwrap(cause)
		}
		lines := strings.Split(ansi.Hardwrap(watchText(cause.Error()), 34, true), "\n")
		if len(lines) > 3 {
			lines = append(lines[:2], ansi.Truncate(lines[2], 33, "")+"…")
		}
		return []bloom.Child{bloom.Fixed(1, bloom.Text(watchText(status))),
			bloom.Fixed(len(lines), bloom.Text(strings.Join(lines, "\n")))}
	}
	status := string(p.state.status)
	if p.state.stale {
		status += " (previous results)"
	}
	if p.state.err != nil {
		status += ": " + p.state.err.Error()
	}
	if p.canRetry {
		status += " | r retry read"
	}
	if p.service == playerDiscoveryUPnP && p.state.err == nil && p.action == nil {
		status += " | no UPnP search, favorites or direct play"
	}
	if p.action != nil {
		label := "ACTION"
		if p.action.pending {
			label = "PENDING"
		} else if p.action.accepted {
			label = "REQUEST ACCEPTED (unconfirmed)"
		}
		status = fmt.Sprintf("%s: %s (%s)", label, p.action.action, p.action.target.Name)
	}
	return []bloom.Child{bloom.Fixed(1, bloom.Text(watchText(status)))}
}

func (ui *playerLayoutUI) discoverySearch() bloom.Child {
	if ui.discovery.presentation().service == playerDiscoveryUPnP {
		return bloom.Text("UPnP search unavailable · browse servers instead").ID("search")
	}
	return bloom.Text("Search: " + ui.search.View()).Style(playerSelectionStyle(ui.searchActive || ui.focus.Is("search"), true)).
		ID("search").Focusable().On(on.Click(func() tea.Cmd {
		ui.pane, ui.searchActive = playerPaneDiscover, true
		return ui.search.Focus()
	}))
}

func (ui *playerLayoutUI) discoveryActions(p playerDiscoveryPresentation) bloom.Child {
	gen, request, index := p.generation, p.state.request, p.selection
	button := func(label string, message tea.Msg) bloom.Child {
		return bloom.Fixed(len(label), bloom.Text(label).Style(playerButtonStyle(false, message != nil)).
			ID("discovery-action-"+label).On(on.Click(func() tea.Cmd {
			if message == nil {
				return nil
			}
			return playerScopedCmd(gen, playerMessage(message))
		})))
	}
	var activate tea.Msg = playerDiscoveryPlayMsg{generation: gen, request: request, index: index}
	if index >= 0 && index < len(p.state.page.items) && playerDiscoveryIsBrowsable(p, p.state.page.items[index]) {
		activate = playerDiscoveryBrowseMsg{generation: gen, request: request, index: index}
	} else if p.service == playerDiscoveryUPnP || !p.writeReady {
		activate = nil
	}
	var addFavorite, removeFavorite, favorites tea.Msg
	if p.service != playerDiscoveryUPnP {
		favorites = playerDiscoveryFavoritesMsg{generation: gen}
	}
	if p.service != playerDiscoveryUPnP && p.writeReady {
		addFavorite = playerDiscoveryFavoriteMsg{generation: gen, request: request, index: index, add: true}
		removeFavorite = playerDiscoveryFavoriteMsg{generation: gen, request: request, index: index}
	}
	var previous, next tea.Msg
	if p.hasPrevious {
		previous = playerDiscoveryPageMsg{generation: gen, request: request, offset: p.previousOffset}
	}
	if p.state.page.hasMore {
		next = playerDiscoveryPageMsg{generation: gen, request: request, offset: p.state.page.nextOffset}
	}
	var retry tea.Msg
	if p.canRetry {
		retry = playerDiscoveryRetryMsg{generation: gen, request: request}
	}
	queueLabel := "[Queue -]"
	var enqueue tea.Msg
	if playerDiscoveryCanEnqueue(p, index) {
		queueLabel = "[Queue]"
		enqueue = playerDiscoveryEnqueueMsg{generation: gen, request: request, index: index}
	}
	return bloom.Column(
		bloom.Fixed(1, bloom.Row(
			button("[Open/Play]", activate),
			button("[+Fav]", addFavorite),
			button("[-Fav]", removeFavorite),
			button(queueLabel, enqueue),
		)),
		bloom.Fixed(1, bloom.Row(
			button("[<]", previous), button("[>]", next),
			button("[Back]", playerDiscoveryBackMsg{generation: gen, request: request}),
			button("[Favs]", favorites),
			button("[Retry read]", retry),
		)),
	)
}

func playerDiscoveryCanEnqueue(p playerDiscoveryPresentation, index int) bool {
	if !p.writeReady || p.service != playerDiscoveryUPnP || index < 0 || index >= len(p.state.page.items) {
		return false
	}
	item := p.state.page.items[index]
	return item.Type == kefw2.ContentTypeAudio && playerDiscoveryUPnPPath(item.Path)
}

func playerDiscoveryIsBrowsable(p playerDiscoveryPresentation, item kefw2.ContentItem) bool {
	return item.Type == kefw2.ContentTypeContainer &&
		(p.service != playerDiscoveryRadio || !playerDiscoveryPlayableRadio(item))
}

func (ui *playerLayoutUI) discoveryServiceButton(label string, service playerDiscoveryService, generation uint64) bloom.Child {
	p := ui.discovery.presentation()
	if p.service == service {
		label = "[" + label + "]"
	} else {
		label = " " + label + " "
	}
	if ui.focus.Is("service-" + string(service)) {
		label = ">" + label
	}
	return bloom.Fixed(len(label), bloom.Text(label).
		Style(playerButtonStyle(ui.focus.Is("service-"+string(service)) || p.service == service, true)).
		ID("service-"+string(service)).Focusable().On(on.Click(func() tea.Cmd {
		ui.pane = playerPaneDiscover
		ui.search.Blur()
		ui.searchActive = false
		return playerScopedCmd(generation, playerMessage(playerDiscoveryServiceMsg{generation: generation, service: service}))
	})))
}

func (ui *playerLayoutUI) discoveryDetails() string {
	p := ui.discovery.presentation()
	var details string
	if p.selection < 0 || p.selection >= len(p.state.page.items) {
		details = "Select a result to inspect details"
	} else {
		item := p.state.page.items[p.selection]
		details = strings.Join([]string{watchText(item.Title), watchText(item.Type), watchText(item.LongDescription)}, "\n")
	}
	if p.action != nil && p.action.err != nil {
		details += "\n" + watchText(p.action.err.Error())
	}
	return details
}
