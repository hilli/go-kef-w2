// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

// playerNoticeMsg is not session-scoped: it preserves uncertainty about a
// dispatched action after the user switches speakers.
type playerNoticeMsg struct {
	target playerSpeaker
	action string
	err    error
}
