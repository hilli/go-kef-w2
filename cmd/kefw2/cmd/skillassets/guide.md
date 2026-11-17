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
and utility workflows below. Command groups that only print help, shell
completion, and skill installation/display are not JSON APIs. Existing human
output and TUIs remain the default when `--json` is omitted.

```text
kefw2 config speaker list --json
kefw2 config speaker discover --json
kefw2 --speaker NAME_OR_IP status --json
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
```

Volume accepts an integer from 0 through 100. Omit the argument to read volume,
mute or source; provide it to write. Source values are `analog`, `bluetooth`,
`coaxial`, `optical`, `standby`, `tv`, `usb`, and `wifi`.
Basic `play` may wake the WiFi source and start an existing queue, not merely
resume the last audible content. Confirm that behavior matches the request.

## JSON results

Each successful invocation above emits one object:

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
  `{state,position_ms:null|int,duration_ms:null|int,track:null|{title,artist,album}}`.
  Duration uses playback status or track resource metadata; unknown values remain `null`.
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

`accepted:true` reports HTTP acceptance, not a completed physical transition;
`accepted:false` reports a no-op. Report what was accepted and what a bounded
subsequent read actually observed. Do not equate missing playback metadata with
an error or evidence that a physical source is silent. For local configuration
or file changes, success acknowledges the completed local operation.

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
- `config eq_profile`: typed EQ configuration, read-only.
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
