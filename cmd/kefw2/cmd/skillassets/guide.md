# kefw2 agent guide

This guide is bundled with, and version-matched to, the running `kefw2`
binary. Use `kefw2 COMMAND --help` for accepted values and flags. Do not invent
capabilities when help or the speaker rejects an operation.

## Select, read, then act

1. List configured targets: `kefw2 config speaker list --json`.
   To discover reachable speakers: `kefw2 config speaker discover --json`.
   Discovery saves configuration only with explicit `--save`.
   Saving discovery preserves the existing effective default speaker.
   Do not silently change configuration or guess which speaker the user means.
2. Select a target explicitly with `--speaker NAME_OR_IP` on every
   speaker command. Ask if the target or intended action is ambiguous.
3. Read `kefw2 --speaker NAME_OR_IP status --json` before changing state.
   Check the source, volume and playback state. Avoid surprising loud playback.
4. Perform the smallest requested action, then read status or the relevant
   getter again. Speaker state is eventually consistent: allow a short bounded
   delay and bounded read retries, not an unbounded loop.

## Non-interactive commands

Pass `--json` on each supported command; it is not a global output mode.
Operational commands support JSON, including the content, queue, configuration
and utility workflows below. The interactive `player`, command groups that
only print help, shell completion, and skill installation/display are not JSON
APIs. Existing human output and TUIs remain the default when `--json` is omitted.

```text
kefw2 config speaker list --json
kefw2 config speaker discover --json
kefw2 --speaker NAME_OR_IP status --json
kefw2 --speaker NAME_OR_IP status watch --json
kefw2 --speaker NAME_OR_IP volume --json
kefw2 --speaker NAME_OR_IP volume 25 --json
kefw2 --speaker NAME_OR_IP mute --json
kefw2 --speaker NAME_OR_IP mute on --json
kefw2 --speaker NAME_OR_IP mute off --json
kefw2 --speaker NAME_OR_IP source --json
kefw2 --speaker NAME_OR_IP source SOURCE --json
kefw2 --speaker NAME_OR_IP play --json
kefw2 --speaker NAME_OR_IP pause --json
kefw2 --speaker NAME_OR_IP stop --json
kefw2 --speaker NAME_OR_IP off --json
kefw2 --speaker NAME_OR_IP radio search "QUERY" --json
kefw2 --speaker NAME_OR_IP radio play --path "PATH" --json
kefw2 --speaker NAME_OR_IP config eq_profile --json
kefw2 --speaker NAME_OR_IP config eq_profile PROFILE.json --json
kefw2 --speaker NAME_OR_IP config eq_profile save "Night listening" --json
kefw2 config eq_profile list --json
kefw2 config eq_profile show "Night listening" --json
kefw2 --speaker NAME_OR_IP config eq_profile load "Night listening" --json
kefw2 config eq_profile import "Night listening" PROFILE.json --json
kefw2 config eq_profile export "Night listening" BACKUP.json --json
kefw2 config eq_profile delete "Night listening" --json
```

Volume accepts an integer from 0 through 100. Omit the argument to read volume,
mute or source; provide it to write. Source values are `analog`, `bluetooth`,
`coaxial`, `optical`, `standby`, `tv`, `usb`, and `wifi`.
Basic `play` may wake the WiFi source and start an existing queue, not merely
resume the last audible content. Confirm that behavior matches the request.

## JSON results

Each finite successful invocation above emits one object. `status watch --json`
instead emits a stream of complete objects, one JSON object per line:

```json
{"speaker":{"name":"Living room","ip_address":"192.0.2.10","model":"LS50 Wireless II"},"data":{}}
```

`data` is command-specific; `speaker` is omitted for local operations, including
version, configuration listing/discovery, saved-queue listing/deletion and local
UPnP index/search operations.
Parse JSON instead of human output. Failures have a nonzero exit status and
diagnostics on stderr; do not treat them as success.

The following describes `data` shapes (`bool`, `int`, and `null|...` denote
types, not literal JSON):

- Config listing: `{items:[{name,ip_address,model,configured,default}]}`.
  `configured` and `default` are booleans.
- Discovery: `{items:[{name,ip_address,model,configured,default}],saved:bool}`.
  Use `config speaker discover --save --json` only when saving is requested.
- Status: `{source,power,volume,muted,playback}`. `power` is `standby`,
  `powerOn`, `networkSetup`, or `firmwareUpgrade`. For physical sources or
  standby, `playback` is `null`. Otherwise it may be
  `{state,position_ms:null|int,duration_ms:null|int,track:null|{title,artist,album,image_url}}`.
  Duration uses playback status or track resource metadata; unknown values remain `null`.
  `image_url` is the speaker-provided artwork URL, or `""` when unavailable.
- Volume read: `{volume}`; write:
  `{action:"set_volume",accepted:true,volume}`.
- Mute read: `{muted}`; write:
  `{action:"set_mute",accepted:true,muted}`.
- Source read: `{source}`; write:
  `{action:"set_source",accepted:true,source}`.
- Pause, stop, off: `{action,accepted:bool}`, where `action` matches the command.
  Pause returns `accepted:false` without mutation when already not playing.
- Play: `{action:"play",accepted:bool,outcome,woke_from_standby,shuffled}`.
  `outcome` is `resumed`, `startedFromQueue`, `nothingToPlay`, or
  `alreadyPlaying`; the last two fields are booleans.
- Radio play: `{action:"play_radio",accepted:true,path}`.
- EQ read: `{eq_profile}`. Restore:
  `{action:"restore_eq_profile",accepted:true,source,eq_profile}`.
- EQ library `list`: `{items:[{name,profile_name,captured_at,speaker?,source?}]}`.
  `show NAME` returns the saved versioned profile document; `show` without a
  name reads the active speaker profile.
- EQ library `save`: `{action:"save_eq_profile",accepted:true,name,profile_name,captured_at}`.
  `load`: `{action:"load_eq_profile",accepted:true,name,profile_name,source}`.
  `import`: `{action:"import_eq_profile",accepted:true,name,profile_name}`.
  `delete`: `{action:"delete_eq_profile",accepted:true,name}`.
  `export NAME FILE`: `{action:"export_eq_profile",accepted:true,name,path}`.
  `export NAME` without a file returns the versioned profile document.

`accepted:true` reports HTTP acceptance, not a completed physical transition;
`accepted:false` reports a no-op. Report what was accepted and what a bounded
subsequent read actually observed. Do not equate missing playback metadata with
an error or evidence that a physical source is silent. For local configuration
or file changes, success acknowledges the completed local operation.

## Interactive player

```text
kefw2 player
kefw2 player --layout listening-desk
kefw2 --speaker "Living Room" player --layout discovery-browser
```

`player` requires a terminal and has no JSON interface; use the commands above
or `status watch --json` for automation. `--layout` accepts only
`listening-desk` and `discovery-browser`; omitting it always starts Listening
Desk. Both layouts share controls, and save pane arrangements separately.
Launching the player does not change the speaker's input or playback.

The initial target is selected from `--speaker NAME_OR_IP`, then
`KEFW2_SPEAKER` or the `speaker` config value, then the configured default or
sole configured speaker. Multiple speakers without a default open a chooser.
If there is no configured or explicit target, use
`kefw2 config speaker discover --save` or `kefw2 config speaker add IP` first.
Use an IP for ambiguous names; explicit unconfigured targets are session-only.

Uppercase `S` or clicking the speaker header opens the configured-speaker
chooser; lowercase `s` opens the input chooser. Use arrows and Enter to select,
or Esc to cancel. Switching control does not alter the saved default or
transfer playback, source, volume or queue. An unavailable target stays
selected with retry/choose-speaker options, never a silent fallback. An action
already accepted by the previous speaker cannot be recalled; failed actions
are not automatically replayed. On failed status reads, the selected speaker's
last confirmed state is marked stale. If the event stream disconnects, writes
pause until a fresh status poll succeeds; unsent actions are discarded, and
uncertain dispatched actions identify their original target.

The bottom row has separate Now Playing and Volume panes. Now Playing shows
confirmed power, the actual input, playback details and transport controls;
Volume contains the slider and mute control. Click a pane title or use Tab to
focus its controls. Radio and Podcasts are Wi-Fi services; browsing alone does
not switch inputs. Search radio/podcasts, open a series to see episodes, or browse UPnP and enqueue
supported tracks. A selection previews; explicit playback can switch input or
replace playback. Radio play requires confirmation and can replace the queue.
Radio-play and queue-clear dialogs default to Cancel: Tab/Shift+Tab or left/right
select a button, Enter activates it, `y` confirms, and `n`/Esc cancels.
TV, Optical, Coaxial, Aux and USB show `external audio`
when playback data is unavailable; transport and seek depend on the source and
media. Player UPnP supports browsing and enqueueing, not search, favorites or
direct playback from a result. Enqueue a track, then select it in the queue
and press Enter to play; enqueueing alone does not start playback.

The compact Sources sidebar lists physical inputs, **Off / Standby**, and
Radio/Podcasts/UPnP browsing shortcuts. Focus it with Alt+1 by default (or Tab), use
arrows or j/k to select, and Enter or the Open button to activate. A mouse
click selects an input; a double-click activates it. A single click on a Browse
Wi-Fi entry opens that service's top-level menu in Discover, also available with
Enter or Open. Selecting it again returns to the menu. `*` marks the confirmed current input;
unsupported inputs are dimmed and marked `-`. Off puts only the selected speaker
into standby. Browsing a Wi-Fi service does not change the physical input.
The `s` chooser also includes Off / Standby. Active panes have cyan double
borders; filled rows mark selection, and amber text identifies keyboard hints.
Custom pane widths are preserved; use `p`, then `R` to restore the narrower
default Sources width.

Use `Tab` / `Shift+Tab` to focus, arrows or `j` / `k` to navigate, `/` to
search and Enter to submit or activate. In Discover, `1` / `2` / `3` select
Radio / Podcasts / UPnP, `<` / `>` page results, and `e` enqueues a supported
UPnP track. `r` retries an offered failed discovery read only, never a failed
playback, favorite or enqueue action.

`Q` focuses the queue; in Discovery Browser, press it again to return to
discovery. `i` opens details. With queue focus, `Ctrl+k` / `Ctrl+j` reorder the
selected row, Enter plays, Delete removes, `c` prompts to clear (`y` confirms),
`h` toggles shuffle, and `t` cycles repeat. Use `r` to reload an uncertain
queue and `a` to acknowledge it after reviewing the refreshed result. Moves
check for intervening changes but cannot guarantee atomicity against another
controller.

`p` opens the pane menu (Move, Resize, Zoom, Reset); `z` toggles zoom on the
focused pane. In the menu, `m` moves, `r` resizes, `z` zooms and uppercase `R`
resets the active layout, and `a` toggles reduced motion (the player does not
currently animate). Use Left/Right and Enter to commit, Esc to cancel.
In Discovery Browser, `p` then `d` docks/undocks the queue in place of the
inspector; that choice is saved with the layout. Reset preserves the other
valid layout when only the active arrangement is invalid and the file's
version and schema are supported. Resetting a malformed, unsupported or
otherwise invalid file may discard other arrangements.

Outside menus, Space toggles playback, `[` / `]` skip, `,` / `.` seek by 10
seconds, `+` / `-` change volume by 1, and `m` toggles mute. Seek requires
supported, currently playing non-live media with a known position and
duration. The slider supports focused Left/Right keys and respects the
selected speaker's maximum; increases are disabled if that limit cannot be
read. Uppercase `R` retries a failed volume change when offered; uppercase `D`
dismisses control/volume errors. Review the speaker and current state before
retrying an uncertain action. Click controls, double-click a navigable
container to open it or a station/episode to play, and drag pane grips,
dividers, queue-row grips or the volume slider with a mouse. Printable
shortcuts apply outside text fields; pane and menu keys depend on focus. Use
`?` for help and `q` or Ctrl+C to quit.

## Live status

```text
kefw2 --speaker NAME_OR_IP status watch
kefw2 --speaker NAME_OR_IP status watch --minimal
kefw2 --speaker NAME_OR_IP status watch --ascii
kefw2 --speaker NAME_OR_IP status watch --json
```

`status` remains one-shot. `status watch` reads immediately, then follows
speaker events; there is no `--interval`, `--sleep`, or `--watch` flag. While
events are healthy, it does not periodically read status or emit heartbeat
snapshots. Changes can arrive as often as the speaker reports them; bursts are
coalesced. Registration/reconnection reconciles with another full read.

Human watch uses an alternate screen and requires terminal stdout. Cover art
stays in place during text-only updates. Native images are supported on
Kitty-compatible terminals and iTerm2 outside tmux; other terminals use ASCII. `--ascii`
always forces ASCII artwork and `--minimal` disables artwork. Resize or changed
image content can redraw artwork; failed artwork downloads show a warning and
are retried without making otherwise current speaker status stale.

Human-only controls: Space toggles existing playback, `n`/`p` select
next/previous, `+`/`-` adjust volume by 1, `m` toggles mute, and Left/Right seek
10 seconds. Playback controls require supported WiFi/Bluetooth playback.
Volume respects the configured maximum; seeks clamp to a known duration and
are unavailable for live/unknown-duration media. Controls never wake a standby
speaker, switch source, or start a stopped queue. Actions are not automatically
retried; acceptance is followed by observed status, not optimistic UI changes.
Press `q` or Ctrl+C to exit and restore the terminal.

`status watch --json` is non-interactive and accepts no keyboard controls. Each
line has the same `speaker`/`data` envelope and fields as `status --json`,
including `playback.track.image_url`; it is not an event delta. No images are
downloaded and no terminal decoration is written. Display-only flags are
ignored. Use a streaming JSON/JSON Lines parser, not a single-document parser.
The existing `events --json` command is different: it emits individual changes.

If events disconnect, watch reports degraded event delivery and polls status
internally while reconnecting (every 5 seconds). Failed status reads also
retry. Human mode marks failed reads stale and retains the last successful
state; JSON skips failed reads and sends diagnostics only to stderr, never
re-emitting stale data as fresh. Fallback polling stops after event recovery
and a successful read. Invalid arguments/configuration/selection and output
write failures terminate the command; transient speaker failures do not.
Ctrl+C ends JSON watch cleanly without a final non-JSON line.

## Radio paths and pagination

Search accepts `--offset` (default `0`) and `--limit` (default `24`, range
`1..100`). Response `data` is
`{items:[{title,path,description}],offset,limit,total_rows,next_offset,has_more}`.
Offset and total count raw service rows, not just playable stations. The speaker
may cap page size. Continue using the returned `next_offset` while `has_more`
is true, rather than adding the requested limit or counting items. Stop if
`next_offset` does not advance; bound requests and fetch only pages needed.

Select a result by title/description and pass its exact returned path to
`radio play --path "PATH" --json` on the same speaker. Do not supply a station
name simultaneously with `--path`. Name-based radio play with `--json` requires
an unambiguous, complete search page; prefer exact-path playback.

Treat paths as opaque and speaker-specific: do not rebuild or normalize them,
transfer them to another speaker, or store them as durable station IDs. They
are not guaranteed long-lived; search again if a path has expired.
An `airable:` path is a speaker API identifier, not a stream URL: pass it
unchanged to `--path`. Present station titles/descriptions to the user rather
than asking them to interpret or copy raw Airable paths.
The speaker can resolve broadcast identifiers without an embedded stream URL;
do not invent or request a replacement stream URL.

Radio playback selects WiFi first, waking a standby speaker and allowing its
player to initialize before clearing/replacing the current queue and playing.
It does not change volume or mute settings. A source-read or source-switch
failure stops the operation before queue mutation.
Some speakers require authenticated control writes. The CLI handles an explicit
HTTP 401 using TLS port 4430 on the same host, even for host-and-port targets;
keep the station path unchanged.
Other control failures are not automatically retried.
Failure after the clear can leave the queue changed. A timeout can also occur
after a mutation was accepted. Never automatically retry radio play or blindly
repeat queue mutations: they can replace, duplicate, or restart content.
Read current state and report uncertainty; prefer bounded reads to write retries.

## Content lists and exact selection

Most other content lists use summaries with `title`, `path`, `type`,
`description`, `artist`, and `album`. Unavailable text is empty; empty lists use
`[]`, not `null`. A list may contain navigable containers as well as playable
items. Keep paths opaque and on the same target speaker.

Paginated lists provide `offset`, `limit`, `total_rows`, `next_offset`, and
`has_more`. Follow the returned offset, not the number of filtered items or
requested limit. Full category/episode lists may instead return just `items`.
Failed pagination returns an error rather than claiming partial data is complete.
Use command help for accepted pagination ranges.

Browsing and searching with `--json` never implicitly play a sole result.
Flags that previously asked a picker to choose an action require an explicit
selection in JSON mode, or are rejected. Duplicate names are errors; prefer
paths, or numeric positions for queue items.

### Radio categories and favorites

```text
kefw2 --speaker NAME_OR_IP radio --json
kefw2 --speaker NAME_OR_IP radio favorites --json
kefw2 --speaker NAME_OR_IP radio popular --json
kefw2 --speaker NAME_OR_IP radio local --json
kefw2 --speaker NAME_OR_IP radio trending --json
kefw2 --speaker NAME_OR_IP radio hq --json
kefw2 --speaker NAME_OR_IP radio new --json
kefw2 --speaker NAME_OR_IP radio browse --path "CONTAINER_PATH" --json
kefw2 --speaker NAME_OR_IP radio favorites --path "STATION_PATH" --remove --json
kefw2 --speaker NAME_OR_IP radio browse --path "STATION_PATH" --save-favorite --json
```

Category commands with an explicit unique station name or `--path` play that
selection; `--save-favorite` saves instead where supported. `radio browse`
remains read-only unless explicitly saving a favorite. Its positional argument
is a human display path; `--path` is an opaque API path and requires JSON.
Favorite changes return `add_radio_favorite` or `remove_radio_favorite`.

### Podcasts

```text
kefw2 --speaker NAME_OR_IP podcast --json
kefw2 --speaker NAME_OR_IP podcast search "QUERY" --json
kefw2 --speaker NAME_OR_IP podcast favorites --json
kefw2 --speaker NAME_OR_IP podcast popular --json
kefw2 --speaker NAME_OR_IP podcast trending --json
kefw2 --speaker NAME_OR_IP podcast history --json
kefw2 --speaker NAME_OR_IP podcast browse popular --json
kefw2 --speaker NAME_OR_IP podcast filter "Genres/Comedy" --json
kefw2 --speaker NAME_OR_IP podcast episodes --path "SHOW_PATH" --json
kefw2 --speaker NAME_OR_IP podcast play --path "SHOW_PATH" --episode-path "EPISODE_PATH" --json
kefw2 --speaker NAME_OR_IP podcast play --path "SHOW_PATH" --latest --json
kefw2 --speaker NAME_OR_IP podcast play --episode-path "EPISODE_PATH" --queue --json
kefw2 --speaker NAME_OR_IP podcast play --path "SHOW_PATH" --action favorite --json
```

Prefer listing episodes and choosing an exact path. When both paths are supplied,
the episode must belong to that show. A direct `--episode-path` plays the exact
episode without falling back to its parent container.

Show-only playback requires explicit `--latest`: it selects the first audio
episode in service order, not an independently verified publication date.
Names in `SHOW/EPISODE` format must resolve uniquely.

Podcast `--queue` appends the chosen episode without clearing the queue or
starting playback. Favorite actions target shows, not episodes.
Search, browse and filter actions require explicit path selection and
`--action play|queue|favorite|remove`; without an action they return lists.
Category commands also support their documented selection/action flags.
Responses include `action`, `accepted`, the targeted `item`, and `latest`.

### UPnP/DLNA

```text
kefw2 --speaker NAME_OR_IP upnp --json
kefw2 --speaker NAME_OR_IP config upnp server list --json
kefw2 --speaker NAME_OR_IP config upnp server default --path "SERVER_PATH" --json
kefw2 --speaker NAME_OR_IP upnp browse --path "CONTAINER_PATH" --json
kefw2 --speaker NAME_OR_IP upnp play --path "TRACK_OR_CONTAINER_PATH" --json
kefw2 --speaker NAME_OR_IP upnp play --path "TRACK_PATH" --queue --json
kefw2 --speaker NAME_OR_IP upnp index --rebuild --json
kefw2 upnp index --json
kefw2 upnp search "QUERY" --json
```

`upnp --json` lists servers. Browse/search are read-only and reject `--queue`;
select a returned path and invoke `upnp play` separately. Lists accept
`--offset` and `--limit`. Positional paths use display names; `--path` uses
opaque speaker paths, and cannot be combined with a positional path.

UPnP playback replaces the queue unless `--queue` is specified. UPnP `--queue`
appends and starts playback only if the queue was empty. This differs from
podcast queueing. Responses include `action`, `accepted`, `path`, and
`track_count`; recursive containers preserve service order.

Search uses the local track index and does not require a speaker or access the
network. Inspect that index with `upnp index --json`; rebuild explicitly on a
selected speaker if missing or stale. Search reports index provenance: do not
assume an index/path came from another selected speaker. A failed recursive
scan returns an error; a failed rebuild preserves the previous index.
Opaque rebuild paths in either CLI mode must belong to the configured default UPnP server; select
that server first rather than rebuilding an index with misleading provenance.

The existing `config upnp server default` and `config upnp container browse`
or `index` commands support JSON getters/setters. Opaque setters use `--path`;
consult help rather than guessing container names.

## Queues and saved queues

```text
kefw2 --speaker NAME_OR_IP queue list --json
kefw2 --speaker NAME_OR_IP queue play 2 --json
kefw2 --speaker NAME_OR_IP queue remove 2 --json
kefw2 --speaker NAME_OR_IP queue move 3 before 1 --json
kefw2 --speaker NAME_OR_IP queue clear --json
kefw2 --speaker NAME_OR_IP queue mode --json
kefw2 --speaker NAME_OR_IP queue shuffle on --json
kefw2 --speaker NAME_OR_IP queue repeat all --json
kefw2 --speaker NAME_OR_IP queue save "NAME" --json
kefw2 queue saved --json
kefw2 --speaker NAME_OR_IP queue load "NAME" --json
kefw2 queue delete-saved "NAME" --json
```

`queue --json` and `queue list --json` return `items`, each with a **1-based**
`position` and content-summary fields. Positions are a snapshot, not durable
IDs. Re-read the queue after changes. Duplicate title/artist selections fail
rather than selecting the first match; numeric positions disambiguate them.

Move accepts numeric positions, unique names, `top`, `bottom`, `up`, `down`,
`next`, or `before|after TARGET`. A no-op move returns `accepted:false`.
Mutation responses identify the position, or `from`/`to`, using 1-based units.
Mode getters return `mode`, `shuffle`, and `repeat`; repeat is `off|one|all`.
The speaker's `shuffleRepeatOne` mode reports `shuffle:true` and `repeat:"one"`;
changing shuffle preserves the current repeat setting.

Saved-queue listing/deletion is local and needs no configured speaker.
Saved names cannot contain path separators or colons in JSON mode. Saving
requires restorable resources. Loading replaces the current queue and starts
playback; it is not an append or a read-only inspection.

## State, configuration and utilities

The following operational commands also accept `--json`:

- `info`: firmware, MAC address, maximum volume, network mode, power/source,
  mute and volume. `version`: version/build metadata, without speaker access.
- `next`, `previous`, `seek POSITION`: explicit navigation. Seek accepts seconds,
  `mm:ss` or `hh:mm:ss`, validates playback/duration, and reports `position_ms`.
- `config eq_profile [PROFILE.json]`: read the current EQ profile or restore a
  complete profile JSON file. The profile applies to the currently selected
  audio source; select the intended source before restoring. JSON restore
  reports the source and profile sent to the speaker. The `show`, `save`,
  `list`, `load`, `delete`, `import`, and `export` subcommands manage named
  snapshots in the local user config directory; they are not profiles stored
  on the speaker. `save` and `load` require a speaker, while the other library
  operations are local. Use `--force` to replace an existing saved profile or
  export file. Import accepts legacy profile JSON and versioned exports.
  `config maxvolume [VALUE]`: read or set the 0..100 maximum-volume limit.
- `config speaker add IP`, `remove SELECTOR`, `default [SELECTOR]`: explicit
  local configuration changes; add connects to the selected address.
  Names must be unique; use an IP to disambiguate.
- `config cache [SETTING [VALUE]]`: read/write cache configuration.
  `cache status` reads local cache summaries and settings; `cache clear`
  clears only the browse cache, not the row cache or track index.
- `reboot`: request a reboot only when explicitly intended.
  `reboot schedule`: inspect the schedule; use the documented
  `--enable|--disable`, `--day`, and `--time` flags to change it.
  Do not infer success from a disconnect if the speaker returned an HTTP rejection.

Do not pass authentication credentials through agent output or enable auth
debugging for authenticated actions. Configuration/schedule writes may
partially apply before a later operation fails. Return the error and inspect
state; do not claim atomicity or automatically retry all writes.

## Limits

The existing `events --json` stream is JSON Lines, not the single-object
contract above. Its poll timeout bounds an individual poll, not total runtime;
use an external deadline/cancellation if observing events. No enhanced event
monitoring or MCP server is implied. Consult help rather than extrapolating
additional flags or APIs.

## Installing the bootstrap

`kefw2 skill setup` installs the small offline bootstrap at
`~/.agents/skills/kefw2/SKILL.md`. It also detects existing `~/.claude`,
`~/.copilot`, `~/.codex` (legacy), `~/.cursor`, `~/.gemini`, and
`$XDG_CONFIG_HOME/opencode` (default `~/.config/opencode`) directories and
installs the same bootstrap under each one's `skills/kefw2/SKILL.md`.
Only the shared directory is created unconditionally; absent agents'
configuration directories and project-local skill directories are not created.
Current Codex can use the shared directory directly.

`--skills-dir DIR` disables detection and uses only that skills root;
`kefw2` is appended. `kefw2 skill remove` checks the same detected locations
and accepts the same override. Both
operations are repeatable, refuse symlink paths and foreign/modified bootstrap
files, and preserve other contents. A conflict at one selected skills root
does not prevent processing the others, but returns nonzero and identifies
the failing root(s). An unsafe agent directory is reported and skipped without
blocking other roots. Global detection errors, such as a relative
`XDG_CONFIG_HOME`, stop the command before any changes.
Use `-v`/`--verbose` with setup or removal to see selected paths and progress;
the default is quiet.
Skill commands do not read application
configuration, access the network, or create application config/cache directories.
The bootstrap loads this binary's current guide with `kefw2 skill show`,
so binary upgrades immediately update those instructions. Rerun setup to
refresh the installed discovery description; exact, unmodified earlier
bootstraps are upgraded automatically. Move a modified bootstrap aside manually
if you want to reinstall; there is no force overwrite.
