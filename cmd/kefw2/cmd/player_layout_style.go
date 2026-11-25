// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"image/color"
	"strings"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/canvas"
	"charm.land/lipgloss/v2"
)

var (
	playerTextColor     = lipgloss.Color("#e8f1f3")
	playerSurfaceColor  = lipgloss.Color("#101820")
	playerRaisedColor   = lipgloss.Color("#1b2a35")
	playerMutedColor    = lipgloss.Color("#94a3b8")
	playerAccentColor   = lipgloss.Color("#67e8f9")
	playerBorderColor   = lipgloss.Color("#425369")
	playerSelectionFill = lipgloss.Color("#164e63")
	playerWarningColor  = lipgloss.Color("#fbbf24")
	playerSuccessColor  = lipgloss.Color("#86efac")
	playerKeyStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")).Bold(true)
	playerHintStyle     = lipgloss.NewStyle().Foreground(playerMutedColor)
)

type playerSurfaceColors struct {
	foreground, background color.Color
}

func (s playerSurfaceColors) Apply(surface *canvas.Surface, _ float64) {
	for y := 0; y < surface.Height(); y++ {
		for x := 0; x < surface.Width(); x++ {
			if cell := surface.Cell(x, y); cell != nil {
				if cell.Style.Fg == nil {
					cell.Style.Fg = s.foreground
				}
				if cell.Style.Bg == nil {
					cell.Style.Bg = s.background
				}
			}
		}
	}
}

func playerSelectionStyle(selected, active bool) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(playerTextColor)
	if !selected {
		return style
	}
	if active {
		return style.Background(playerSelectionFill).Bold(true)
	}
	return style.Background(lipgloss.Color("#263747"))
}

func playerButtonStyle(focused, enabled bool) lipgloss.Style {
	if !enabled {
		if focused {
			return playerHintStyle.Underline(true)
		}
		return playerHintStyle
	}
	if focused {
		return lipgloss.NewStyle().Foreground(playerSurfaceColor).Background(playerAccentColor).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(playerAccentColor)
}

func playerFrameStyle(active bool) lipgloss.Style {
	border := playerBorderColor
	if active {
		border = playerAccentColor
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border)
}

func playerTitleStyle(active bool) lipgloss.Style {
	style := lipgloss.NewStyle().Foreground(playerMutedColor).Background(playerRaisedColor).Bold(true)
	if active {
		style = style.Foreground(playerAccentColor)
	}
	return style
}

func playerProblemLine(line string) bool {
	for _, prefix := range []string{
		"UNCERTAIN", "STALE", "Events disconnected", "LAYOUT ERROR", "Control error", "Control unavailable",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func playerStatusText(lines []string) bloom.TextNode {
	styled := make([]string, len(lines))
	for i, line := range lines {
		style := playerHintStyle
		switch {
		case playerProblemLine(line):
			style = style.Foreground(playerWarningColor)
		case strings.HasPrefix(line, "Track: "):
			style = style.Foreground(playerTextColor).Bold(true)
		case strings.HasPrefix(line, "Playback: "):
			style = style.Foreground(playerTextColor)
		}
		styled[i] = style.Render(line)
	}
	return bloom.Text(strings.Join(styled, "\n"))
}

func playerHint(text string) bloom.TextNode {
	parts := strings.Split(text, " · ")
	for i, part := range parts {
		key, rest, found := strings.Cut(part, " ")
		if found {
			parts[i] = playerKeyStyle.Render(key) + " " + playerHintStyle.Render(rest)
		} else {
			parts[i] = playerKeyStyle.Render(key)
		}
	}
	return bloom.Text(strings.Join(parts, playerHintStyle.Render(" · "))).Style(playerHintStyle)
}
