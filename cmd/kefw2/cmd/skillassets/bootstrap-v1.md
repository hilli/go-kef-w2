---
name: kefw2
description: Use when asked to inspect or control KEF W2 speakers, adjust volume, mute or source, control playback, or find and play internet radio with the kefw2 CLI.
---

# KEF W2 speaker control

Run `kefw2 skill show` to read the complete guide bundled with the installed
binary before controlling speakers. If that command is unavailable, use
`kefw2 --help` and the relevant subcommand's `--help`; do not guess flags.

Select the intended speaker explicitly, read its state before acting, and
distinguish an accepted command from an observed state change. Prefer the
documented non-interactive JSON commands. Never retry queue mutations blindly.
