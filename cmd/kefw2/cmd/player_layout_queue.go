// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
)

func playerMessage(message tea.Msg) tea.Cmd { return func() tea.Msg { return message } }

//nolint:gocyclo // Queue controls and rows have independent enablement.
func (ui *playerLayoutUI) queueView(m *playerModel) bloom.Child {
	state := ui.queue.state()
	uiIndex := max(0, min(ui.queueIndex, len(state.Items)-1))
	children := []bloom.Child{bloom.Fixed(1, ui.title(m, ModeQueue))}
	status := "Enter play · Ctrl+j/k move · Del remove · c clear · h shuffle · t repeat"
	switch {
	case state.Uncertain:
		status = "UNCERTAIN: review queue; r reload · a acknowledge"
		if state.Err != nil {
			status += ": " + state.Err.Error()
		}
	case state.Busy || state.Loading:
		status = "Loading / working..."
	case state.Err != nil:
		status = "Queue error: " + state.Err.Error() + " · r retry"
	}
	if state.ModeErr != nil {
		status += " | Mode error: " + state.ModeErr.Error()
	}
	children = append(children, bloom.Fixed(1, playerHint(watchText(status))))
	if !state.Ready {
		children = append(children, bloom.Fixed(1, bloom.Text("Writes disabled: awaiting fresh speaker status")))
	}
	bar := bloom.Row(
		bloom.Fixed(8, bloom.Text("[Play]").Style(playerButtonStyle(false, state.Ready && !state.Busy && !state.Uncertain)).
			ID("queue-play").On(on.Click(func() tea.Cmd {
			if !state.Ready || state.Busy || state.Uncertain || state.Snapshot == nil {
				return nil
			}
			return playerMessage(playerQueuePlayMsg{Generation: state.Generation, Snapshot: state.Snapshot, Index: uiIndex})
		}))),
		bloom.Fixed(9, bloom.Text("[Remove]").Style(playerButtonStyle(false, state.Ready && !state.Busy && !state.Uncertain)).
			ID("queue-remove").On(on.Click(func() tea.Cmd {
			if !state.Ready || state.Busy || state.Uncertain || state.Snapshot == nil {
				return nil
			}
			return playerMessage(playerQueueRemoveMsg{Generation: state.Generation, Snapshot: state.Snapshot, Index: uiIndex})
		}))),
		bloom.Fixed(8, bloom.Text("[Clear]").Style(playerButtonStyle(false, state.Ready && !state.Busy && !state.Uncertain)).
			ID("queue-clear-button").On(on.Click(func() tea.Cmd {
			if state.Ready && !state.Busy && !state.Uncertain {
				ui.openQueueClear(playerQueueClearMsg{Generation: state.Generation, Snapshot: state.Snapshot})
			}
			return nil
		}))),
	)
	modes := bloom.Row(
		bloom.Fixed(15, bloom.Text(fmt.Sprintf("[Shuffle %t]", state.Mode.Shuffle)).ID("queue-shuffle").On(on.Click(func() tea.Cmd {
			if !state.Ready || state.Busy || state.Uncertain || !state.ModeKnown {
				return nil
			}
			return playerMessage(playerQueueShuffleMsg{
				Generation: state.Generation, ReadyEpoch: state.ReadyEpoch, Enabled: !state.Mode.Shuffle,
			})
		}))),
		bloom.Fill(bloom.Text(fmt.Sprintf("[Repeat %s]", state.Mode.Repeat)).ID("queue-repeat").On(on.Click(func() tea.Cmd {
			if !state.Ready || state.Busy || state.Uncertain || !state.ModeKnown {
				return nil
			}
			next := PlayModeOff
			switch state.Mode.Repeat {
			case PlayModeOff:
				next = queueRepeatAll
			case queueRepeatAll:
				next = queueRepeatOne
			}
			return playerMessage(playerQueueRepeatMsg{Generation: state.Generation, ReadyEpoch: state.ReadyEpoch, Mode: next})
		}))),
	)
	children = append(children, bloom.Fixed(1, bar), bloom.Fixed(1, modes))
	if state.Err != nil || state.Uncertain {
		recovery := bloom.Row(
			bloom.Fixed(9, bloom.Text("[Reload]").ID("queue-reload").On(on.Click(func() tea.Cmd {
				return playerMessage(playerQueueLoadMsg{Generation: state.Generation})
			}))),
			bloom.Fill(bloom.Text("[Review OK]").ID("queue-review").On(on.Click(func() tea.Cmd {
				if !state.ReviewReady {
					return nil
				}
				return playerMessage(playerQueueAcknowledgeMsg{Generation: state.Generation, ReviewID: state.ReviewID})
			}))),
		)
		children = append(children, bloom.Fixed(1, recovery))
	}
	rows := max(1, m.height-max(3, m.height/3)-11)
	start := max(0, uiIndex-rows+1)
	end := min(len(state.Items), start+rows)
	for i := start; i < end; i++ {
		index := i
		item := state.Items[index]
		marker := "  "
		if index == uiIndex {
			marker = "> "
		}
		playing := " "
		if index == state.PlayingIndex {
			playing = "▶"
		}
		title := fmt.Sprintf("%s%s %s", marker, playing, watchText(item.Title))
		row := bloom.Row(
			bloom.Fixed(2, bloom.Text("::").ID(fmt.Sprintf("queue-grip-%d", index)).
				On(on.Drag(func(event on.Event) tea.Cmd {
					return ui.queueRowDrag(m, state, index, event)
				}))),
			bloom.Fill(bloom.Text(title).Style(playerSelectionStyle(index == uiIndex, ui.paneActive(ModeQueue))).
				ID(fmt.Sprintf("queue-row-%d", index)).Focusable().On(
				on.Click(func() tea.Cmd { ui.pane, ui.queueIndex = ModeQueue, index; return nil }),
				on.DoubleClick(func() tea.Cmd {
					if !state.Ready {
						return nil
					}
					return playerMessage(playerQueuePlayMsg{
						Generation: state.Generation, Snapshot: state.Snapshot, Index: index,
					})
				}),
			)),
		)
		children = append(children, bloom.Fixed(1, row))
	}
	if len(state.Items) == 0 && !state.Loading {
		children = append(children, bloom.Fixed(1, bloom.Text("Queue is empty")))
	}
	if ui.queueDrag != nil {
		children = append(children, bloom.Fixed(1, bloom.Text(fmt.Sprintf("Insert before row %d · Esc cancels", ui.queueDrag.gap+1))))
	}
	children = append(children, bloom.Fill(bloom.Text("")))
	return bloom.Column(children...).On(on.Scroll(func(_, dy int) tea.Cmd {
		ui.pane = ModeQueue
		ui.queueIndex = max(0, min(len(state.Items)-1, ui.queueIndex+dy))
		return nil
	}))
}

func (ui *playerLayoutUI) openQueueClear(request playerQueueClearMsg) {
	ui.cancelPointerInteractions()
	if ui.queueClear == nil {
		ui.focus.Push("queue-clear")
	}
	ui.queueClear = &request
	ui.queueConfirmAccept = false
}

func (ui *playerLayoutUI) closeQueueClear() {
	ui.queueClear = nil
	if ui.focus.Scope() == "queue-clear" {
		ui.focus.Pop()
	}
}

func (ui *playerLayoutUI) queueRowDrag(m *playerModel, state playerQueueUIState, index int, event on.Event) tea.Cmd {
	if event.Msg == nil || m.chooserOpen || ui.menu || ui.queueClear != nil || !state.Ready || state.Busy || state.Uncertain ||
		ui.queueDragBlocked {
		ui.queueDrag = nil
		return nil
	}
	if ui.queueDrag == nil {
		ui.queueDrag = &playerQueueDrag{generation: state.Generation, snapshot: state.Snapshot,
			from: index, gap: index, count: len(state.Items)}
	}
	drag := ui.queueDrag
	for i := range drag.count {
		rect, ok := ui.root.Frame().Rect(fmt.Sprintf("queue-row-%d", i))
		if !ok {
			continue
		}
		if event.Screen.Y == rect.Min.Y {
			drag.gap = i
			break
		}
		if event.Screen.Y >= rect.Max.Y {
			drag.gap = i + 1
		}
	}
	if !event.Done {
		return nil
	}
	ui.queueDrag = nil
	if _, ok := event.Msg.(tea.MouseReleaseMsg); !ok {
		return nil
	}
	to, err := playerQueueDropTarget(drag.from, drag.gap, drag.count)
	if err != nil || to == drag.from {
		return nil
	}
	return playerMessage(playerQueueMoveMsg{
		Generation: drag.generation, Snapshot: drag.snapshot, From: drag.from, To: to,
	})
}
