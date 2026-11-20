# Implementation plan: dual-layout KEF player

Status: implementation in progress. This plan delivers the
[selected design](2026-11-18-player-listening-desk-design.md) in PR #44.
Discovery Browser's [visual reference is PR #45](https://github.com/hilli/go-kef-w2/pull/45).
The existing screenshots are design mockups; real terminal screenshots are a
release deliverable, not evidence that these features already work.

## Outcome and boundaries

Build one Bubble Tea v2 application using Bloom, launched as:

```sh
kefw2 player
kefw2 player --layout listening-desk
kefw2 player --layout discovery-browser
kefw2 player --speaker "Living Room" --layout discovery-browser
```

Listening Desk is always the default when `--layout` is omitted. Both arrangements
provide the same capabilities and keyboard bindings. Users can change the
controlled speaker while the player stays open, using the configured-speaker
chooser in either arrangement.

| Requirement | Delivered behavior | Main milestone |
|---|---|---|
| Launch and layouts | Strict layout flag, completion, Listening Desk default | M1, M6 |
| Configured speakers | Name/IP chooser; independent, cancellable speaker sessions | M2 |
| Inputs and transport | Actual input, supported playback/seek, clear external-audio state | M3 |
| Volume | Click/drag slider, exact keyboard steps, current speaker's limit | M3 |
| Radio and podcasts | Paged search, series/episode navigation, explicit play | M4 |
| Queue ordering | Drag insertion and keyboard moves of the speaker's active queue | M5 |
| Movable/resizable panes | Dock, resize, zoom, cancel, restore with keyboard parity | M6 |
| Responsive/accessibility | Compact panes, focus restoration, text-only and reduced motion | M6, M7 |
| Delivery | Tests, user/embedded guide, actual app screenshots | M7 |

Include UPnP browsing and adding supported tracks to the active queue because
the selected workspace exposes a library pane. Reuse existing favorites and
shuffle/repeat capabilities where those actions are shown. Do not invent
unverified enqueue semantics for radio or podcast resources.

Out of scope: a third Queue Studio layout, synchronized multiroom playback,
speaker discovery/config editing inside the player, saved-playlist editing,
multi-item transactional queue moves/undo, a layout plugin format, a live
layout-switcher, and a new player JSON API. Existing CLI commands and
`status watch --json` remain unchanged. The TUI controls one speaker at a time.

## Repository findings that shape the work

| Existing code | Reuse or integration requirement |
|---|---|
| `cmd/kefw2/cmd/root.go` | `initConfig` reads `speakers`, `defaultSpeaker`, environment and `--config`; `prepareCommand` normally resolves a speaker before RunE. The player needs a narrow preparation path that permits a chooser and validates local arguments before network access. |
| `cmd/kefw2/cmd/config_speaker.go` | Configured names/models/IPs and default indication already exist. Reuse that data; do not invoke discovery or rewrite config on selection. |
| `cmd/kefw2/cmd/status_watch.go` | Event invalidation, snapshot refresh, reconnect polling, and cancellation are useful foundations. The current watch is single-speaker; its workers must be owned by a player session. |
| `cmd/kefw2/cmd/status_json.go`, `status_watch_controls.go` | Reuse `readStatus` and capability checks rather than creating a second state parser or treating all Wi-Fi streams as seekable. |
| `kefw2/airable.go` | GET/POST requests currently use `context.Background`; clients retain service base URLs. Player-used operations need cancellation propagated through their full call chains. |
| `kefw2/kefw2.go` | `NewSpeaker` performs `UpdateInfo(context.Background())`. Selecting a configured speaker must not block Update/View in this constructor. |
| `kefw2/airable_radio.go`, `airable_podcast.go` | Bounded searches, exact playback, favorites and service navigation already exist. Radio playback also creates a background context; cancellation cannot stop at the low-level HTTP helper. |
| `kefw2/queue.go` | Queue moves use indices, not conditional revisions. Duplicate occurrences and concurrent-controller changes need explicit handling. Polls/sleeps and background-context calls on reused paths need cancellation review. |
| `kefw2/airable.go`, `cache.go`; `cmd/kefw2/cmd/cache.go` | Rows cache keys use service path/range without speaker identity, and CLI browse cache is global. Do not share these caches across player speaker sessions. |
| `content_picker.go`, `browser_styles.go` | Reuse service types/styles and useful selection helpers, not nested standalone Tea programs or synchronous picker callbacks in the new event loop. |
| `AGENTS.md`, `skillassets/guide.md`, `Taskfile.yaml` | Update embedded command documentation with CLI changes; use project formatting, lint, build and test commands. |

Bloom is pre-1.0. Pin a verified compatible release rather than coding against an
unbounded dependency or assuming its showcase is a reusable pane manager.

## Architecture and invariants

Keep the initial implementation in `cmd/kefw2/cmd` as cohesive `player_*.go`
files, consistent with `status_watch_*`, so existing private helpers can be
reused. Split by responsibility only when needed: command, model/session,
controls, discovery, queue, layout, and persistence. No new framework/package
hierarchy is needed. Add library changes only for the cancellation/correctness
requirements of the player.

```text
Cobra command + validated options + configured speaker descriptors
  -> player model (one Update loop, Bloom Root/Focus, keymap, selected layout)
       -> presentation: sources / discovery / inspector / queue / transport
       -> active speaker session: target + generation + context/cancel
            -> state/event worker
            -> ordered mutation worker, coalescing volume targets
            -> discovery reader with latest-query generation
       <- typed results tagged with speaker generation and request identity
```

The Tea model owns UI state; workers return immutable results, never edit it.
Do not share a mutable `KEFSpeaker` or `AirableClient` across concurrent workers:
some methods update fields/client state. Keep worker-owned handles for the same
fixed endpoint, initialize them in their owner, and serialize access. Share
immutable descriptors and confirmed snapshots instead. Race-test the boundaries.

Use the smallest injected dependency surface needed by actual commands, following
`watchDependencies`. A fake clock/timer enables deterministic coalescing/reconnect
tests. Use `httptest` where endpoint/payload/cancellation behavior matters.

Invariants:

1. A command captures its speaker target and generation when created; it never
   consults global `currentSpeaker` later.
2. No retired-generation response may change current UI state or create another
   action. A generation increments even when returning to the same IP.
3. Switching controls is not transferring playback. No wake, stop, play, source,
   volume, queue, or configuration write is generated by selecting a speaker.
4. Confirmed state and pending intent are distinct. Read failures cannot look
   like success, and failed/uncertain mutations are never replayed on reconnect.
5. Layout affects presentation only; both builders use the same components,
   actions, capability checks, and keymap.

## Configured-speaker contract

### Selection and startup

The header shows `name (IP)` and opens a filterable chooser on click, Tab+Enter,
or uppercase `S` outside text input. Lowercase `s` selects the physical input.
Rows show configured name, model, IP, current selection and configured default.
Keyboard arrows/Enter choose, Esc cancels, and Tab stays within the modal.
Duplicate names remain separately selectable by IP. Unchecked speakers have
unknown reachability, not an invented online indicator.

Initial precedence: explicit `--speaker`, existing `KEFW2_SPEAKER`/`speaker`
configuration, configured default, then the sole configured speaker. Preserve
the existing ambiguous-name error for explicit names. Multiple configured
speakers without a default open the chooser instead of exiting. Zero configured
speakers without an explicit target produce guidance for
`kefw2 config speaker discover --save` or `kefw2 config speaker add <ip-address>`.
An explicit nonconfigured target retains the CLI's direct-target behavior but
is session-only and visibly distinguished from configured entries.

Configuration is read once at startup. Speaker selection is session-local and
does not change `defaultSpeaker`. Default selection and add/remove remain existing
configuration commands. Layout persistence never records a last-selected speaker.

### Switch state machine

`Ready(A) -> Connecting(B) -> Ready(B)` or `Unavailable(B)`.

On confirmed selection, advance the generation, mark B as the target, disable
mutations, clear A's displayed playback/queue/source/volume/capabilities and
close speaker-scoped menus/drags. Cancel A's session and discard its unsent
actions, debounced slider target and queue draft. Start B with fresh handles and
load state, maximum volume, capabilities and queue as independently reported
resources. Enable a control only when its required state is fresh; an unavailable
Airable service need not disable known-safe volume controls.

Keep the layout and optionally query text, but clear result paths/cursors and
re-fetch against B. Do not reuse A's Airable service base URLs, favorites, UPnP
paths, queue indices, capability assumptions or caches. The first implementation
should use session-local memory/no cache for browsing, and uncached queue/status
reads; do not retrofit global disk caching into this feature.

At `Unavailable(B)`, keep B visibly selected with Retry/Choose Speaker. Do not
silently keep controlling A. Switching back creates a fresh session. The chooser
remains usable during connection, service failure, or reconnect.

Stop/drain old workers asynchronously, retaining a bounded shutdown path; never
wait synchronously in Update. Coalesce rapid switch intents to the latest target,
cancel superseded connection attempts, and prevent unbounded retired workers.
On quit, cancel and join all workers/event subscriptions without leaving the
terminal in mouse/alternate-screen mode.

Cancellation cannot retract an HTTP write already accepted by A. If its result
is uncertain, retain an explicit notice naming A; never convert it into a B
error or retry it on B. Newly selected speakers do not inherit pending actions.

## Milestones and dependencies

Each milestone is a reviewable implementation slice, not permission to ship an
incomplete UI. Only documentation changes are made by this planning PR.

| Milestone | Depends on | Completion signal |
|---|---|---|
| M0: Bloom feasibility and dependency pin | None | A small actual Tea/Bloom frame demonstrates the required primitives |
| M1: Command shell and cancellation foundation | M0 | Correct launch contract and cancellable player-used I/O |
| M2: Speaker sessions and chooser | M1 | Switch between two configured targets without state/action leakage |
| M3: Now playing, sources and volume | M2 | Safe, capability-aware controls on the selected speaker |
| M4: Discovery and library | M2, M3 | Search/browse/play/favorite with bounded async requests |
| M5: Active queue editing | M2, M3 | Correct confirmed reorder via mouse and keyboard |
| M6: Both layouts and pane customization | M3, M4, M5 | Both complete workspaces adapt and persist independent arrangements |
| M7: Integration, documentation and release evidence | M6 | Full acceptance matrix passes and screenshots show the running TUI |

M4 and M5 can be developed independently after their prerequisites, but should
not require concurrent edits to the same model file. Start with a simple
noncustomizable layout in M1-M5; finish pane mechanics once components exist.

### M0 - Validate Bloom before committing to its API

Targets: `go.mod`, `go.sum`, initial `player_layout.go` and focused tests.

Verify a currently released Bloom version against the repository's Bubble Tea,
Bubbles and Lip Gloss v2 versions. Pin it and use its actual API. Demonstrate
focus scopes, stable-ID drag tracking, one-cell splitter hit testing, modal
input blocking, and layout response to window-size messages. Keep the useful
frame/tests, not a second demo app. Do not upgrade unrelated dependencies.

Exit gate: real terminal mouse/focus/resize smoke check plus a deterministic
frame/handler test. Document incompatibilities before writing around them.
Bloom supplies primitives; docking order and volume/queue behavior remain ours.

### M1 - Add the command shell and cancellable I/O

Targets: `player.go`, `player_model.go`, `root.go`, `kefw2/airable*.go`,
player-used portions of `queue.go`, associated tests, embedded skill guide.

Register `player` with `--layout`, `cobra.NoArgs`, static layout completion and
existing `--speaker` completion. Validate arguments and TTY input/output before
speaker I/O. Put pure validation in the appropriate Cobra Args/preparation path:
a RunE-only check is too late if the inherited PersistentPreRunE contacts a
speaker first. Add a narrow player preparation path; preserve other commands'
selection, completion and JSON behavior. Load configuration but let player
sessions resolve/connect the target asynchronously.

Show an initial responsive shell with persistent header, help, speaker chooser
entry point, and placeholder loading states for unfinished components. Honor
command context and SIGINT/SIGTERM. Return explicit non-TTY errors without
emitting escape sequences. Do not auto-play at startup.

Add context-taking variants for the Airable operations actually used by the
player, retaining existing public methods as background-context wrappers.
Thread the supplied context through service discovery, exact playback, source
switching, GET/POST requests, pagination and queue waits; replace relevant
`time.Sleep` polling with cancellable timer selects. Do not stop at adding a
context parameter to the top-level adapter while inner calls use Background.
Avoid an all-library rewrite: inventory and cover the selected call chains.
For configured speakers use session-owned descriptors and `UpdateInfo(ctx)`;
if direct-target construction needs a new context-aware constructor, preserve
the existing constructor as a compatibility wrapper.

Tests: default/explicit/invalid layouts, extra args, help/completion without
network or TUI, no target vs multiple targets, alternate `--config` and selector
precedence, non-TTY rejection before network; HTTP cancellation including a
radio activation chain and a queue wait. Existing callers retain behavior.

### M2 - Make configured-speaker switching real

Targets: `player_session.go`, `player_model.go`, `player_speakers.go`,
root preparation integration and session/chooser tests.

Implement the startup and switch contracts above before adding mutations.
Adapt status-watch refresh/event handling to session ownership; preserve watch
behavior and avoid copying its state parser. Carry generation/request IDs in
all messages, including failures, reconnect timers and future artwork replies.
Provide Retry, Cancel and Choose Speaker paths; canceling an unconfirmed chooser
does not retire the active session.

Tests use two fake servers with the same service paths but different state.
Hold A's response, select B, release A and confirm only B appears. Repeat
A -> B -> A to catch IP-only checks. Switch while an event request, a pending
connection, and an artificially queued mutation are outstanding. Assert requests
remain bound to their original target, queued writes are dropped, old workers
terminate, and the saved config bytes are unchanged. Test duplicate names, no
default, offline B, retry, repeated selection of the active speaker (no-op),
and keyboard/click equivalence.

Exit gate: the selected speaker is unambiguous in every state, and switching
does not send a playback/source/volume write to either server.

### M3 - Wire actual input, playback and volume

Targets: `player_controls.go`, shared status/capability helpers,
transport/source/slider views and focused tests.

Render the target's confirmed input, power, playback metadata, position/duration,
mute, volume and health. Mark stale values and their age after disconnect. Gate
pause/next/previous/seek using actual capability/state; show explicit external
audio and live-radio states. Reuse useful status-watch helpers without inheriting
its n/p keymap into the shared player keymap.

Implement source selection as an explicit action. Ordinary transport/volume
must not wake standby speakers; an explicit source/Play action may activate
Wi-Fi according to existing service behavior and must identify its target.
Refresh after writes; never infer confirmed source from the currently browsed
service.

Implement the slider mapping/zero-width guards, configured maximum, focus,
keyboard steps and coalescing specified in the design. Keep one volume write
in flight and retain only the latest target; flush release unless that session
was retired. A failed maximum read blocks increases, not a guessed ceiling.
Changing speakers discards the old target and loads the new limit before enabling
increases. Rate limiting must not block mute or freeze the UI.

Tests: two different speaker limits, drag endpoints and beyond-track positions,
keyboard precision, final flush, write failure/readback, event races, standby and
unsupported controls, and switch during a queued/accepted volume request.

### M4 - Add radio, podcasts and library discovery

Targets: `player_discovery.go`, inspector/navigation views and tests, reused
Airable radio/podcast/UPnP helpers.

Use a real query field and Radio/Podcasts selector. Submit explicitly; fetch
bounded pages and follow service-provided paths for next pages/episodes.
Maintain a query generation in addition to the speaker generation. Show loading,
no matches, end of results, old-query results and errors distinctly. Selecting a
row previews; only explicit Play/Enter on a playable leaf changes playback.

Resolve exact radio paths and podcast episodes with the existing helpers.
Implement favorites through the existing service APIs. Browse configured UPnP
servers/containers, with explicit supported-track enqueue; do not depend on
global `currentSpeaker` or disk-cache paths in reused CLI callbacks. Do not launch
the old picker as a nested Tea application.

Tests: late search/episode replies, speaker changes mid-query, query text with
spaces and shortcut letters, page limits/boundaries, series vs episode behavior,
back/selection restoration, exact duplicate-title activation, favorite errors,
service unavailability, and UPnP enqueue not changing another speaker.

### M5 - Edit the active speaker queue

Targets: `player_queue.go`, queue view/drag handlers, adapter tests.

Load the actual queue, keep playing and selected markers distinct, expose
play/remove/confirmed clear plus shuffle/repeat. Implement Ctrl+k/Ctrl+j and
row-grip drag to the same move action. Preview insertion without mutating;
Esc cancels. No automatic playback after a move. Keep all queue writes serialized
and disable additional queue edits while a mutation is unresolved.

Before moving, refresh and compare the ordered snapshot. Resolve duplicate-path
occurrences against that snapshot and convert visible indices at the boundary.
Refetch after the mutation before declaring confirmed success. If the queue
changed, reject with a review/retry prompt. An index-based API still has a race
between preflight and write; do not claim atomic protection. If a timeout may
have followed an accepted write, refetch and report uncertainty rather than
blindly retrying.

Tests: first/last positions, duplicate titles/paths, out-of-range/stale selection,
mouse and keyboard equivalence, canceled/no-op drag, detected concurrent changes,
ambiguous write outcomes and refetch, remove/clear confirmation, and speaker
switch during preflight/write/refresh. Assert reorder never calls Play.

### M6 - Complete the two layouts and pane behavior

Targets: `player_layout.go`, pane state/persistence, layout/focus tests.

Compose shared components into Listening Desk and Discovery Browser according
to the selected design's minimums and collapse rules. Add stable-ID title drag
to dock, splitter resize, title double-click/keyboard zoom, and pane menu
Move/Resize/Reset. Keyboard previews and cancel/commit behavior must match the
mouse paths. Modal layers trap focus and restore it to a reachable control;
when a pane collapses, preserve selection and expose it via a tab/chooser.

Keep speaker chooser, current target, input and transport discoverable in both
layouts. Test resize during a drag, modal opening during drag, lost mouse release,
and returning from zoom. No backend command is sent by a layout operation.
Use text markers/borders as well as color, plain-text artwork fallback, and a
reduced-motion preference in the pane/settings menu. Fancy animation is not
required for correctness.

Persist validated versioned pane arrangements keyed by layout ID in
`player-layout.yaml` beside the selected `kefw2.yaml`, using atomic replacement.
Respect OS-specific config location and `--config`. Missing file means factory
layout; malformed data or write failure is a visible recoverable error with
Reset. Do not persist speaker selection, search history, or queued mutations.
Corrupt/future-version files must not be silently overwritten.

Tests: both layouts at 140x40, 100x30, 80x24 and below 60x18; narrow and short
dimensions independently; reachable collapsed queue/details/speaker controls;
long Unicode names by display width; move/resize/cancel/zoom; saved layout
isolation, validation, atomic-write failure and Reset. Starting without --layout
must still pick Listening Desk after using Discovery Browser.

### M7 - Prove end-to-end behavior and document delivery

Targets: focused integration tests, `README.md`, relevant CLI docs,
`cmd/kefw2/cmd/skillassets/guide.md`, PR screenshots.

Run the end-to-end matrix below using fake endpoints first. Perform actual
terminal smoke checks for keyboard-only navigation, mouse dragging, resize,
terminal restoration, and both layouts; do not substitute HTML render checks.
Optional hardware validation must name the intended speaker and obtain approval
before audible playback/volume/source changes. If hardware is unavailable,
record that limitation rather than claiming hardware validation.

Document launch examples, layout default, initial speaker precedence, `S` vs `s`,
non-persistent speaker switching, pane menu, supported queue operations, error
recovery and external-source limitations. Update the embedded guide in the same
implementation slices that add/change commands or flags, not only at the end.
Keep existing help, completions, JSON contracts and skill output consistent.

Capture real running-app wide/compact screenshots for both layouts plus the
speaker chooser and a disconnected/reconnecting state, using deterministic demo
data/fake adapters clearly labeled as such. Replace or distinguish design
mockups in the implementation PR. Do not require a live speaker for screenshots.

## Verification and release gates

| Scenario | Required evidence |
|---|---|
| Both launch layouts | Same features/keymap; invalid layout fails before I/O |
| A -> B -> A | Old replies/events/search/artwork cannot update the new generation |
| Different volume limits | B never inherits A's limit, queued target or mute intent |
| Offline selected speaker | Clear target/error; chooser works; no hidden fallback to A |
| Queue edit during switch | Draft/unsent move discarded; accepted write never replayed on B |
| Duplicate queue entries | Selected occurrence, not first matching title/path, moves |
| Stale/disconnected services | Errors remain visible; reads recover without replayed writes |
| Keyboard-only path | Switch speaker/input, search/play, order queue, resize/move panes |
| Resize and modal focus | Controls remain reachable; canceled drags send no backend action |
| Existing CLI regression | Other speaker resolution, status watch/JSON and skill output unchanged |

For each slice run the smallest relevant `go test` selectors in `./kefw2` and/or
`./cmd/kefw2/cmd`, expanding when a shared helper changes. Run race tests on the
session/cancellation tests as soon as M2 lands and before release. Validate
behavioral tests by a controlled mutation: remove the generation check, target
capture, volume clamp, or queue conflict check and confirm the corresponding
test fails, then restore. Verify a mutation applied and compiled before counting
the result; a compile failure is not useful behavioral evidence.

Before each implementation commit, format touched Go files and run:

```sh
task lint
task build
task test
```

At the final gate also run `go test -race ./cmd/kefw2/cmd ./kefw2`. Record actual
results and unresolved environment limitations, not assumed passes.

Release only when M0-M7 and the acceptance matrix are complete. No silent
speaker fallback, stale-generation UI updates, cross-speaker mutation, duplicate
backend per layout, or mouse-only essential operation is an acceptable shortcut.
