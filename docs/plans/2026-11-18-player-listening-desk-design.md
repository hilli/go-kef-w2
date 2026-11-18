# Player design: Listening Desk and Discovery Browser

**Selected direction:** one `kefw2 player` with two user-selectable layouts.
Listening Desk is the default; Discovery Browser is the search-first alternative.
Both use the same player state, components, and controls. This is a design
proposal, not an implemented command.

Delivery is broken into dependency-ordered milestones in the
[implementation plan](2026-11-18-player-implementation-plan.md), including
switching between configured speakers without restarting the player.

![Listening Desk, wide](../designs/player-listening-desk/wide.png)
![Listening Desk, compact](../designs/player-listening-desk/compact.png)

## Experience and trade-offs

A quiet, cyan-accented workspace: sources on the left, a large discovery pane in
the middle, and a queue on the right. A persistent transport dock shows the
speaker, actual input, playback metadata, connection health, and volume. Browsing
Radio does not imply the speaker has switched inputs: the dock continues to say,
for example, `INPUT Optical / external audio` until a successful source change.
Radio and Podcasts are content services within Wi-Fi, not physical inputs.

Listening Desk balances the main workflows for lower learning cost and fewer
mode switches. Discovery Browser gives more space to search results and episode
details, with the queue available on demand. Queue Studio is not included in the
selected implementation scope.

## Layout selection

Proposed command interface:

```sh
kefw2 player                            # Listening Desk (default)
kefw2 player --layout listening-desk     # Explicit default
kefw2 player --layout discovery-browser # Search-first arrangement
```

Use a named `--layout` flag rather than a positional argument so its purpose is
clear and other player options can coexist. Accept exactly `listening-desk` and
`discovery-browser`, with shell completion for both. Reject an unknown value
with a nonzero exit and an error listing the allowed values, before initializing
the terminal UI or contacting a speaker. Reject positional arguments.

The flag selects a starting pane arrangement, not a feature set or a separate
player. Both layouts support source selection, radio/podcast search, queue
ordering, now-playing status, volume, mouse, and full keyboard operation.
Launching either layout must not itself change playback or speaker input.
An in-app layout switcher is outside the initial scope; select at launch.

Omitting the flag always selects Listening Desk, even if the previous session
used Discovery Browser. Remember pane customizations separately under each
layout ID; never let one layout's widths or order overwrite the other's.
Reset restores only the active layout's built-in arrangement.

The Discovery Browser [design and screenshots in PR #45](https://github.com/hilli/go-kef-w2/pull/45)
provide its visual reference. The shared controls, state handling, and launch
contract in this document take precedence over differences in the original
standalone proposals.

## Listening Desk arrangement

At 120+ columns and 32+ rows, use three columns: Sources (24 cells), Discover
(remaining width, minimum 42), Queue (32). Keep the transport and one-line
context-sensitive help outside the pane tree. These are terminal-cell targets;
the accompanying HTML is an illustrative mockup, not a cell-exact renderer.

Drag a pane's title grip to another dock position; a placeholder previews the
drop. Drag a one-cell divider to resize adjacent panes, respecting minimums.
Double-click a title or press `z` with pane focus to zoom/unzoom. A normal title
click only focuses, so double-click does not also perform a destructive action.
Save pane order and widths, not absolute screen coordinates.

At 90-119 columns, turn Queue into a tab beside Discover. Below 90 columns, also
replace Sources with a source chooser. `Tab` and numbered pane shortcuts still
reach collapsed panes as overlays. Below 60x18, show an explicit size warning,
but retain quit and mute. Short terminals reduce visible list rows before
collapsing detail content. Enlarging restores the user's layout and selection.

## Discovery Browser arrangement

At 120+ columns and 32+ rows, show service/source navigation (22 cells), results
(fill, minimum 38), and an inspector (40). Search stays above the pane tree;
the same transport and volume dock remains below it. Selecting a result previews
details without playing it. Enter on a podcast series opens its episodes; Enter
on a playable episode or station activates it.

Queue opens as a drawer with `Q` or its visible button and can dock in place of
the inspector. It uses the same queue component, reorder controls, and conflict
handling as Listening Desk. The inspector is not a second search engine: it
reads the selected result from the shared discovery state.

At 90-119 columns collapse navigation into a chooser. Below 90 columns, replace
the inspector with a Details overlay (`i`) and give results the available width.
The compact queue drawer retains a small input/volume status line. Apply the same
60x18 minimum, keyboard-accessible collapsed panes, drag/resize/zoom semantics,
and layout restoration rules as Listening Desk.

## Interaction contract

| Action | Mouse | Keyboard |
|---|---|---|
| Focus | Click pane/control | Tab / Shift+Tab; Alt+1/2/3 for panes |
| Navigate | Wheel; click row | Arrows or j/k; PageUp/PageDown; Home/End |
| Activate | Click Play button; double-click result | Enter; podcast series opens episodes first |
| Search | Click search field; choose Radio/Podcasts | `/`; Tab to service selector; Enter submits |
| Queue / details | Click Queue / Details | `Q` focuses queue or toggles its drawer; `i` opens details |
| Speaker | Click speaker name in header | `S` opens configured-speaker chooser; arrows, Enter; Esc cancels |
| Source | Select source, activate Switch | `s`, choose, Enter; Esc cancels |
| Volume | Click/drag slider; wheel only over slider | `+`/`-` outside text input; slider Left/Right |
| Mute | Click Mute | `m` outside text input |
| Playback | Click transport | Space toggles; `[`/`]` previous/next outside inputs |
| Queue order | Drag row grip; insertion line previews destination | Ctrl+k / Ctrl+j moves selected row |
| Pane placement | Drag title grip | `p` opens pane menu: Move, Resize, Zoom, Reset |
| Pane resizing | Drag divider | Pane menu > Resize, arrows, Enter commit, Esc restore |
| Help / quit | Click help / Quit | `?`; `q` outside inputs; Ctrl+c always |

The pane menu's Move action uses arrows between dock targets, Enter commits and
Esc restores. It also exposes named pane choices, so Alt-key terminal conflicts
never make a pane inaccessible. Text inputs receive printable keys, spaces, and
cursor keys before application shortcuts. Modal focus is trapped and restored.
When a slider has focus, Home/End select zero/the allowed maximum; Up/Down
change by five points. No shortcut raises the speaker's configured maximum.
Use these bindings in both layouts; queue reorder shortcuts apply only with
queue focus. Layout selection must not introduce a second keymap.

## Configured-speaker switching

The persistent header identifies the selected speaker by name and IP. Clicking
it, tabbing to it and pressing Enter, or pressing uppercase `S` opens a
filterable list of configured speakers, showing name, model, IP, configured
default, and the current selection. Lowercase `s` remains the input/source
chooser. Use the IP to disambiguate duplicate names; only a completed probe
justifies an online/offline indicator. Keep the chooser reachable in both layouts,
including compact mode and when the current speaker is offline.

Initial selection honors `--speaker NAME_OR_IP`, then `KEFW2_SPEAKER`/the existing
`speaker` configuration value, then the configured default (or the sole configured
speaker). If several speakers exist without a default, open the chooser instead
of failing before the TUI starts. Preserve explicit name/IP selection semantics,
including errors for ambiguous names. An explicit unconfigured address is
session-only; it does not add a configuration entry. With no configured or
explicit target, return actionable setup instructions.

Selecting a speaker is not an audio handoff: do not stop the old speaker, start
the new one, copy volume/source/queue, wake either speaker, or change the saved
default. Layout customizations remain selected while speaker-specific state is
reloaded. The initial configured speaker list is a session snapshot; adding,
removing, grouping, or changing the default stays in `config speaker`.

On confirmation, immediately identify the requested target and show Connecting.
Disable its mutations, cancel and retire the previous speaker session, discard
unsent actions/slider targets/queue drafts, and load fresh target state. Never
show the old speaker's metadata, volume limit, or queue under the new name.
Every asynchronous result, event, and action carries the originating session
generation; ignore results from retired generations, including A -> B -> A.
Network operations capture a fixed target, never a mutable global selection.

Recreate Airable clients and speaker-scoped caches for the new session. Clear
search results and paths; retaining query text is fine, but resubmission must
resolve it against the new speaker. Selecting an unreachable speaker leaves an
explicit error for that target with Retry and Choose Speaker; never silently
fall back to controlling the previous speaker. Cancellation cannot recall a
write already accepted by the old speaker: report an uncertain old-target action
with its speaker identity, never replay it against the newly selected speaker.

## Volume slider: yes

Build a horizontal track using Bloom `on.ClickAt` and `on.Drag`; it is a custom
control, not an assumed built-in slider. For a track of width `w > 1`, map
`clamp(x, 0, w-1) / (w-1)` to `0..min(100, maxVolume)`, rounding to an integer.
Handle width 0/1 explicitly. Show both requested and confirmed values while a
write is pending. A cell-based slider has finite precision; keys provide exact
single-point adjustments.

Coalesce drag updates to at most ten writes per second, with one volume request
in flight and the latest target replacing intermediate targets. Always flush
the final target after release. Read back the confirmed value; on failure revert
to the last confirmed value with a persistent error and Retry. Do not retry
volume changes automatically after reconnect. If the maximum cannot be loaded,
disable volume increases and explain why; mute and decreases remain available
when their required state is known.

## Data flow and implementation boundary

Propose one new Cobra subcommand, `kefw2 player`, with the `--layout` contract
above, retaining existing speaker selection/configuration. Do not change
`status watch` or its JSON contracts.
Production implementation must update the embedded
`cmd/kefw2/cmd/skillassets/guide.md` with the command, layout values, default,
and examples when the command is actually added.

Use the existing Bubble Tea v2 stack. A pointer-owned player model owns
`bloom.Root`, `bloom.Focus`, pane state and child Bubbles inputs/viewports.
Bloom supplies rows/columns, slots with minimums and collapse priorities,
focus scopes, modal layers and hit testing. **Docking, splitter state, slider,
and queue drag/drop are application logic**, following Bloom's showcase; they
are not a ready-made window manager. Stable pane/row IDs survive reordering.
Pass messages to `ui.Update` and handle mouse actions only once.

Keep one model/update loop, speaker adapter, event subscription, search state,
queue state, transport, slider, and keymap. The two layout builders compose the
same components with different slots, collapse priorities, and drawer/inspector
placement. Do not duplicate network workers, playback logic, or queue mutation
handlers per layout. Keep the two built-in arrangements explicit rather than
introducing a plugin system or arbitrary layout-definition language.

Reuse `KEFSpeaker.Source`, `SetSource`, `GetVolume`, `GetMaxVolume`, `SetVolume`,
`Mute`/`Unmute`, and supported transport operations. Reuse the event invalidation
and snapshot approach from `status_watch.go`: events trigger refreshed state,
not blind assumptions about success. Run speaker/network I/O in commands or a
serialized worker, never in View or mouse callbacks; cancel on exit.

Discovery uses `SearchRadioPage`, bounded `SearchPodcasts` results and service
containers to reach episodes. Fetch additional pages explicitly through
`GetRows` using service paths/ranges; never download an entire catalogue merely
to draw a list. A query generation number discards late results; show loading,
empty results, service errors and Retry separately.

The queue uses `GetPlayQueue`, `MoveQueueItem`, `PlayQueueIndex` and
`RemoveFromQueue`. "Playlist ordering" means the active speaker queue, not
silently rewriting a saved playlist. Before a move, refresh and compare the
ordered snapshot, resolve the selected occurrence (including duplicate paths),
serialize the mutation, then refetch. If another controller changed the queue,
cancel and ask the user to retry; the index-based API cannot guarantee an atomic
compare-and-swap. No claim of race-free edits against concurrent controllers.

Wi-Fi services and queue playback must be explicitly activated. A Radio station
uses `ResolveAndPlayRadioStation`; episodes use the exact episode API where
applicable. These may replace current playback, so show the target speaker/input
in the action. Never claim radio is seekable or universally enqueueable. TV,
Optical, Coaxial, Aux and USB show external audio when metadata is unavailable;
disable unsupported transport with a reason. Bluetooth/Wi-Fi capability is not
proof that a particular stream can seek. Show seek only with known duration and
supported media. Unsupported physical inputs are omitted/disabled per model.

Persist layout only in a proposed versioned `player-layout.yaml` alongside the
selected `kefw2.yaml` (derive the directory from `viper.ConfigFileUsed()`, respecting
the existing OS-specific configuration path and `--config`), with atomic writes
and visible save errors.
Key saved arrangements by `listening-desk` and `discovery-browser`; load only the
selected layout's pane state, without storing a last-used/default-layout override.
Validate IDs and widths; offer Reset for invalid files instead of silently
discarding them. Never persist search history or playback changes implicitly.

## Failure states and acceptance

Keep the last confirmed snapshot marked `STALE` with its age during disconnect;
disable writes until reconnect and require explicit retry of failed mutations.
Keep errors in the affected pane rather than only in a disappearing toast.
Render a text-only fallback without artwork, truecolor, mouse, or animation.
Reduced-motion disables transitions; focus uses a marker and border as well as
color. Escape always cancels an uncommitted drag/menu, never stops playback.

Implementation tests should cover keyboard/mouse parity for source selection,
search, volume endpoints/clamping/coalescing, drag final flush and write failure;
duplicate queue entries and concurrent-change rejection; query response ordering;
modal focus restoration; pane move/resize/cancel; reconnect without replayed
mutations; and resize at 140x40, 100x30, 80x24 and below minimum. Use fake speaker
adapters and Bloom's frame/click helpers; no live speaker required. Prove each
behavioral test fails when that behavior is removed before accepting it.

Run shared interaction and responsive-layout tests against both layout builders,
including search and queue access when their panes are collapsed. Add command
tests for omitted/explicit layout values, invalid values before terminal/network
initialization, positional-argument rejection, and completion. Verify saved pane
arrangements and Reset are isolated by layout ID, and that launching either
layout issues no playback/source mutations. The default must remain Listening
Desk after a prior Discovery Browser session.

Exercise speaker switching in both layouts with two fake servers, different
volume limits, identical service paths, delayed reads/writes, and A -> B -> A
selection. Assert no cross-speaker state or queued commands, no default-config
write, no playback mutation caused by selection, and chooser recovery after an
offline target. Verify old session workers/event subscriptions terminate and
stale artwork/search/queue replies cannot populate the new target.

## Visuals and references

The screenshots are **static browser-rendered terminal design mockups**, using
fictional metadata, not screenshots of a running Go/Bloom app. The standalone
[mockup](../designs/player-listening-desk/index.html) has no remote dependencies.
Reproduce with a local HTTP server and a browser at 1440x1000 and 880x850.

Verified against [Bloom mouse](https://bloomfx.dev/input/mouse),
[focus](https://bloomfx.dev/input/focus), [responsive
layouts](https://bloomfx.dev/layout/responsive), and the [showcase
application](https://github.com/lrstanley/bloom/tree/master/_examples/showcase).
Repository anchors: `kefw2/queue.go`, `kefw2/airable_radio.go`,
`kefw2/airable_podcast.go`, `kefw2/kefw2.go`,
`cmd/kefw2/cmd/status_watch.go` and `status_watch_controls.go`.
