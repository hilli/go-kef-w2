// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"fmt"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
		status = "Queue edits paused pending review"
	case state.Busy || state.Loading:
		status = "Loading / working..."
	case state.Err != nil:
		status = "Queue error: " + state.Err.Error() + " · r retry"
	}
	if state.ModeErr != nil {
		status += " | Mode error: " + state.ModeErr.Error()
	}
	children = append(children, bloom.Fixed(1, playerHint(watchText(status))))
	recoveryHeight := 0
	if state.Err != nil || state.Uncertain {
		recoveryHeight = 6
		children = append(children, bloom.Fixed(recoveryHeight, ui.queueRecoveryView(state)))
	}
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
		bloom.Fixed(15, bloom.Text(fmt.Sprintf("[Shuffle %t]", state.Mode.Shuffle)).
			Style(playerButtonStyle(false, state.Ready && !state.Busy && !state.Uncertain && state.ModeKnown)).
			ID("queue-shuffle").On(on.Click(func() tea.Cmd {
			if !state.Ready || state.Busy || state.Uncertain || !state.ModeKnown {
				return nil
			}
			return playerMessage(playerQueueShuffleMsg{
				Generation: state.Generation, ReadyEpoch: state.ReadyEpoch, Enabled: !state.Mode.Shuffle,
			})
		}))),
		bloom.Fill(bloom.Text(fmt.Sprintf("[Repeat %s]", state.Mode.Repeat)).
			Style(playerButtonStyle(false, state.Ready && !state.Busy && !state.Uncertain && state.ModeKnown)).
			ID("queue-repeat").On(on.Click(func() tea.Cmd {
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
	controls := bloom.Box(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(playerBorderColor),
		bloom.Column(bloom.Fixed(1, bar), bloom.Fixed(1, modes))).ID("queue-controls")
	children = append(children, bloom.Fixed(4, controls))
	rows := max(1, m.height-max(3, m.height/3)-13-recoveryHeight)
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

func (ui *playerLayoutUI) queueRecoveryView(state playerQueueUIState) bloom.Child {
	title, instruction := "QUEUE READ FAILED", "Reload to try again."
	if state.Uncertain {
		title, instruction = "QUEUE NEEDS REVIEW", "Edits paused.\nReload and review order."
	}
	reloadStyle, reviewStyle := playerHintStyle, playerHintStyle
	if !state.Busy && !state.Loading {
		reloadStyle = lipgloss.NewStyle().Foreground(playerSurfaceColor).Background(playerWarningColor).Bold(true)
		if state.Uncertain && state.ReviewReady {
			reviewStyle = lipgloss.NewStyle().Foreground(playerSurfaceColor).Background(playerSuccessColor).Bold(true)
		}
	}
	actions := []bloom.Child{
		bloom.Fixed(13, bloom.Text("[r Reload]").Style(reloadStyle).ID("queue-reload").On(on.Click(func() tea.Cmd {
			if state.Busy || state.Loading {
				return nil
			}
			return playerMessage(playerQueueLoadMsg{Generation: state.Generation})
		}))),
	}
	if state.Uncertain {
		actions = append(actions, bloom.Fill(bloom.Text("[a Review OK]").Style(reviewStyle).
			ID("queue-review").On(on.Click(func() tea.Cmd {
			if state.Busy || state.Loading || !state.ReviewReady {
				return nil
			}
			return playerMessage(playerQueueAcknowledgeMsg{Generation: state.Generation, ReviewID: state.ReviewID})
		}))))
	}
	content := bloom.Column(
		bloom.Fixed(1, bloom.Text(title).Style(lipgloss.NewStyle().Foreground(playerWarningColor).Bold(true))),
		bloom.Fixed(2, bloom.Text(instruction)),
		bloom.Fixed(1, bloom.Row(actions...)),
	)
	return bloom.Box(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(playerWarningColor),
		content).ID("queue-recovery")
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
