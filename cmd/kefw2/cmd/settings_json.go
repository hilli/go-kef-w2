// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"encoding/json"
	"fmt"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/hilli/go-kef-w2/kefw2"
)

const (
	settingsEnableFlag  = "enable"
	settingsDisableFlag = "disable"
)

func init() {
	setJSONHandler(infoCmd, runInfoJSON)
	setJSONHandler(VersionCmd, runVersionJSON)
	setJSONHandler(eqProfileCmd, runEQProfileJSON)
	setJSONHandler(maxVolumeCmd, runMaxVolumeJSON)
	setJSONHandler(seekCmd, runSeekJSON)
	setJSONHandler(nextTrackCmd, runTrackJSON)
	setJSONHandler(previousTrackCmd, runTrackJSON)
	setJSONHandler(rebootCmd, runRebootJSON)
	setJSONHandler(rebootScheduleCmd, runRebootScheduleJSON)
	setJSONHandler(configCacheCmd, runConfigCacheJSON)
	setJSONHandler(speakerAddCmd, runSpeakerAddJSON)
	setJSONHandler(speakerRemoveCmd, runSpeakerRemoveJSON)
	setJSONHandler(speakerSetDefaultCmd, runSpeakerDefaultJSON)
	setJSONHandler(cacheStatusCmd, runCacheStatusJSON)
	setJSONHandler(cacheClearCmd, runCacheClearJSON)
}

func runVersionJSON(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		Version   string `json:"version"`
		GitCommit string `json:"git_commit"`
		GitBranch string `json:"git_branch"`
		BuildDate string `json:"build_date"`
	}{Version, GitCommit, GitBranch, BuildDate})
}

func runInfoJSON(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if err := currentSpeaker.UpdateInfo(ctx); err != nil {
		return fmt.Errorf("updating speaker information: %w", err)
	}
	mode, err := currentSpeaker.NetworkOperationMode(ctx)
	if err != nil {
		return fmt.Errorf("getting network mode: %w", err)
	}
	state, err := currentSpeaker.SpeakerState(ctx)
	if err != nil {
		return fmt.Errorf("getting speaker state: %w", err)
	}
	source, err := currentSpeaker.Source(ctx)
	if err != nil {
		return fmt.Errorf("getting source: %w", err)
	}
	muted, err := currentSpeaker.IsMuted(ctx)
	if err != nil {
		return fmt.Errorf("getting mute state: %w", err)
	}
	volume, err := currentSpeaker.GetVolume(ctx)
	if err != nil {
		return fmt.Errorf("getting volume: %w", err)
	}
	return writeSpeakerJSON(cmd, struct {
		FirmwareVersion string              `json:"firmware_version"`
		MACAddress      string              `json:"mac_address"`
		MaxVolume       int                 `json:"max_volume"`
		NetworkMode     kefw2.CableMode     `json:"network_mode"`
		SpeakerState    kefw2.SpeakerStatus `json:"speaker_state"`
		Source          kefw2.Source        `json:"source"`
		Muted           bool                `json:"muted"`
		Volume          int                 `json:"volume"`
	}{currentSpeaker.FirmwareVersion, currentSpeaker.MacAddress, currentSpeaker.MaxVolume,
		mode, state, source, muted, volume})
}

func runEQProfileJSON(cmd *cobra.Command, args []string) error {
	// This command has no setter; do not silently ignore a supplied value.
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	profile, err := currentSpeaker.GetEQProfileV2(cmd.Context())
	if err != nil {
		return fmt.Errorf("getting EQ profile: %w", err)
	}
	return writeSpeakerJSON(cmd, struct {
		EQProfile kefw2.EQProfileV2 `json:"eq_profile"`
	}{profile})
}

func runMaxVolumeJSON(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		volume, err := currentSpeaker.GetMaxVolume(cmd.Context())
		if err != nil {
			return fmt.Errorf("getting maximum volume: %w", err)
		}
		return writeSpeakerJSON(cmd, struct {
			MaxVolume int `json:"max_volume"`
		}{volume})
	}
	volume, err := parseVolume(args[0])
	if err != nil {
		return fmt.Errorf("invalid maximum volume: %w", err)
	}
	if err := currentSpeaker.SetMaxVolume(cmd.Context(), volume); err != nil {
		return fmt.Errorf("setting maximum volume: %w", err)
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		MaxVolume int `json:"max_volume"`
	}{actionResult{"set_max_volume", true}, volume})
}

func runSeekJSON(cmd *cobra.Command, args []string) error {
	position, err := parseTimePosition(args[0])
	if err != nil {
		return fmt.Errorf("invalid time position: %w", err)
	}
	if err := requirePlaybackControlJSON(cmd); err != nil {
		return err
	}
	pd, err := currentSpeaker.PlayerData(cmd.Context())
	if err != nil {
		return fmt.Errorf("getting player data: %w", err)
	}
	if pd.State != kefw2.PlayerStatePlaying {
		return fmt.Errorf("nothing is currently playing")
	}
	if pd.Status.Duration > 0 && position > int64(pd.Status.Duration) {
		return fmt.Errorf("seek position exceeds track duration")
	}
	if err := currentSpeaker.SeekTo(cmd.Context(), position); err != nil {
		return fmt.Errorf("seeking: %w", err)
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		PositionMS int64 `json:"position_ms"`
	}{actionResult{"seek", true}, position})
}

func requirePlaybackControlJSON(cmd *cobra.Command) error {
	ok, err := currentSpeaker.CanControlPlayback(cmd.Context())
	if err != nil {
		return fmt.Errorf("getting playback source: %w", err)
	}
	if !ok {
		return fmt.Errorf("playback control requires WiFi/Bluetooth source")
	}
	return nil
}

func runTrackJSON(cmd *cobra.Command, _ []string) error {
	if err := requirePlaybackControlJSON(cmd); err != nil {
		return err
	}
	var err error
	if cmd == nextTrackCmd {
		err = currentSpeaker.NextTrack(cmd.Context())
	} else {
		err = currentSpeaker.PreviousTrack(cmd.Context())
	}
	if err != nil {
		return fmt.Errorf("changing track: %w", err)
	}
	return writeSpeakerJSON(cmd, actionResult{cmd.Name(), true})
}

// The library's optional auth debug logger includes raw server bodies. Refuse
// authenticated JSON operations when enabled rather than risk credential output.
func checkJSONAuthLogging() error {
	if enabled, _ := strconv.ParseBool(os.Getenv("KEFW2_AUTH_DEBUG")); enabled {
		return fmt.Errorf("disable KEFW2_AUTH_DEBUG for authenticated JSON operations")
	}
	return nil
}

func runRebootJSON(cmd *cobra.Command, _ []string) error {
	if err := checkJSONAuthLogging(); err != nil {
		return err
	}
	// Reboot tolerates disconnects from a restarting speaker. Do not report
	// acceptance if a connection failed before a request could even be sent.
	var sent atomic.Bool
	ctx := httptrace.WithClientTrace(cmd.Context(), &httptrace.ClientTrace{
		GetConn:      func(string) { sent.Store(false) },
		WroteRequest: func(info httptrace.WroteRequestInfo) { sent.Store(info.Err == nil) },
	})
	if err := currentSpeaker.Reboot(ctx); err != nil || !sent.Load() {
		// Auth errors may contain echoed credentials or authorization headers.
		return fmt.Errorf("reboot request failed")
	}
	return writeSpeakerJSON(cmd, actionResult{"reboot", true})
}

var scheduleTimeJSON = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func rebootScheduleChangedJSON(cmd *cobra.Command) (bool, error) {
	flags := cmd.Flags()
	day, _ := flags.GetInt("day")
	clock, _ := flags.GetString("time")
	if flags.Changed(settingsEnableFlag) && flags.Changed(settingsDisableFlag) {
		return false, fmt.Errorf("--enable and --disable are mutually exclusive")
	}
	if flags.Changed("day") && (day < 0 || day > 6) {
		return false, fmt.Errorf("day must be between 0 and 6")
	}
	if flags.Changed("time") && !scheduleTimeJSON.MatchString(clock) {
		return false, fmt.Errorf("time must be in HH:MM 24-hour format")
	}
	changed := flags.Changed(settingsEnableFlag) || flags.Changed(settingsDisableFlag) ||
		flags.Changed("day") || flags.Changed("time")
	if changed {
		if err := checkJSONAuthLogging(); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func runRebootScheduleJSON(cmd *cobra.Command, _ []string) error {
	changed, err := rebootScheduleChangedJSON(cmd)
	if err != nil {
		return err
	}
	schedule, err := currentSpeaker.GetScheduledReboot(cmd.Context())
	if err != nil {
		return fmt.Errorf("reading scheduled reboot failed")
	}
	if !changed {
		return writeSpeakerJSON(cmd, schedule)
	}
	flags := cmd.Flags()
	if flags.Changed("day") {
		day, _ := flags.GetInt("day")
		schedule.DayOfWeek = day
	}
	if flags.Changed("time") {
		clock, _ := flags.GetString("time")
		schedule.Time = clock
	}
	if flags.Changed(settingsEnableFlag) {
		enable, _ := flags.GetBool(settingsEnableFlag)
		schedule.Enabled = enable
	}
	disable, _ := flags.GetBool(settingsDisableFlag)
	if flags.Changed(settingsDisableFlag) && disable {
		schedule.Enabled = false
	}
	if err := currentSpeaker.SetScheduledReboot(cmd.Context(), schedule); err != nil {
		return fmt.Errorf("setting scheduled reboot failed")
	}
	return writeSpeakerJSON(cmd, struct {
		actionResult
		kefw2.ScheduledReboot
	}{actionResult{"set_reboot_schedule", true}, schedule})
}

type cacheSettingsJSON struct {
	Enabled    bool `mapstructure:"enabled" json:"enabled"`
	TTLDefault int  `mapstructure:"ttl_default" json:"ttl_default"`
	TTLRadio   int  `mapstructure:"ttl_radio" json:"ttl_radio"`
	TTLPodcast int  `mapstructure:"ttl_podcast" json:"ttl_podcast"`
	TTLUPnP    int  `mapstructure:"ttl_upnp" json:"ttl_upnp"`
}

func currentCacheSettingsJSON() (cacheSettingsJSON, error) {
	var settings cacheSettingsJSON
	if err := viper.UnmarshalKey("cache", &settings); err != nil {
		// Avoid printing config values, which may have been pasted credentials.
		return settings, fmt.Errorf("invalid cache configuration")
	}
	return settings, nil
}

func runConfigCacheJSON(cmd *cobra.Command, args []string) error {
	settings, err := currentCacheSettingsJSON()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return writeJSON(cmd, settings)
	}
	setting := args[0]
	var key string
	var value any
	switch setting {
	case settingsEnableFlag, settingsDisableFlag:
		if len(args) != 1 {
			return fmt.Errorf("%s does not accept a value", setting)
		}
		key, value = "cache.enabled", setting == settingsEnableFlag
	case "ttl-default", "ttl-radio", "ttl-podcast", "ttl-upnp":
		key = "cache.ttl_" + strings.TrimPrefix(setting, "ttl-")
		if len(args) == 1 {
			return writeJSON(cmd, map[string]int{strings.TrimPrefix(key, "cache."): viper.GetInt(key)})
		}
		seconds, err := strconv.Atoi(args[1])
		if err != nil || seconds <= 0 || int64(seconds) > int64((1<<63-1)/time.Second) {
			return fmt.Errorf("TTL must be a positive whole number of seconds within duration range")
		}
		value = seconds
	default:
		return fmt.Errorf("unknown cache setting %q", setting)
	}
	old := viper.Get(key)
	viper.Set(key, value)
	if err := saveSpeakerConfig(); err != nil {
		viper.Set(key, old)
		return err
	}
	settings, err = currentCacheSettingsJSON()
	if err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		actionResult
		cacheSettingsJSON
	}{actionResult{"set_cache_config", true}, settings})
}

func configuredSpeakerIndexJSON(selector string) (int, error) {
	if strings.TrimSpace(selector) == "" {
		return -1, fmt.Errorf("speaker selector must not be empty")
	}
	for _, byIP := range []bool{true, false} {
		found := -1
		for i, speaker := range speakers {
			match := speaker.Name == selector
			if byIP {
				match = speaker.IPAddress == selector
			}
			if !match {
				continue
			}
			if found >= 0 {
				return -1, fmt.Errorf("speaker selector is ambiguous; use a unique IP address")
			}
			found = i
		}
		if found >= 0 {
			return found, nil
		}
	}
	return -1, fmt.Errorf("configured speaker not found")
}

func configuredDefaultJSON() (int, error) {
	selector := viper.GetString("defaultSpeaker")
	if selector != "" {
		return configuredSpeakerIndexJSON(selector)
	}
	if len(speakers) == 1 {
		return 0, nil
	}
	return -1, nil
}

func persistSpeakersJSON(items []kefw2.KEFSpeaker, defaultIP string) error {
	oldSpeakers, oldDefault := viper.Get("speakers"), viper.Get("defaultSpeaker")
	viper.Set("speakers", items)
	viper.Set("defaultSpeaker", defaultIP)
	if err := saveSpeakerConfig(); err != nil {
		viper.Set("speakers", oldSpeakers)
		viper.Set("defaultSpeaker", oldDefault)
		return err
	}
	speakers = items
	defaultSpeaker = nil
	for i := range speakers {
		if speakers[i].IPAddress == defaultIP {
			defaultSpeaker = &speakers[i]
			break
		}
	}
	return nil
}

func runSpeakerAddJSON(cmd *cobra.Command, args []string) error {
	host := args[0]
	parsed, err := url.Parse("http://" + host)
	if err != nil || parsed.Host != host || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		strings.TrimSpace(host) != host {
		return fmt.Errorf("invalid speaker host; use an IP address or hostname, optionally with a port")
	}
	def, err := configuredDefaultJSON()
	if err != nil {
		return err
	}
	speaker := &kefw2.KEFSpeaker{IPAddress: host}
	if err := speaker.UpdateInfo(cmd.Context()); err != nil {
		return fmt.Errorf("adding speaker: %w", err)
	}
	if !speakerDefined(host) {
		items := append(append([]kefw2.KEFSpeaker{}, speakers...), *speaker)
		defaultIP := ""
		if def >= 0 {
			defaultIP = speakers[def].IPAddress
		} else if len(items) == 1 {
			defaultIP = host
		}
		if err := persistSpeakersJSON(items, defaultIP); err != nil {
			return err
		}
	}
	return writeJSON(cmd, struct {
		actionResult
		Speaker speakerIdentity `json:"speaker"`
	}{actionResult{"add_speaker", true}, identifySpeaker(speaker)})
}

func runSpeakerRemoveJSON(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	index, err := configuredSpeakerIndexJSON(args[0])
	if err != nil {
		return err
	}
	def, err := configuredDefaultJSON()
	if err != nil {
		return err
	}
	removed := identifySpeaker(&speakers[index])
	items := append([]kefw2.KEFSpeaker{}, speakers[:index]...)
	items = append(items, speakers[index+1:]...)
	defaultIP := ""
	if def >= 0 && def != index {
		defaultIP = speakers[def].IPAddress
	} else if len(items) == 1 {
		defaultIP = items[0].IPAddress
	}
	if err := persistSpeakersJSON(items, defaultIP); err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		actionResult
		Speaker speakerIdentity `json:"speaker"`
	}{actionResult{"remove_speaker", true}, removed})
}

func runSpeakerDefaultJSON(cmd *cobra.Command, args []string) error {
	index, err := configuredDefaultJSON()
	if len(args) > 0 {
		index, err = configuredSpeakerIndexJSON(args[0])
	}
	if err != nil {
		return err
	}
	if index < 0 {
		return fmt.Errorf("no default speaker configured")
	}
	speaker := identifySpeaker(&speakers[index])
	if len(args) == 0 {
		return writeJSON(cmd, struct {
			Default speakerIdentity `json:"default"`
		}{speaker})
	}
	if err := persistSpeakersJSON(speakers, speaker.IPAddress); err != nil {
		return err
	}
	return writeJSON(cmd, struct {
		actionResult
		Default speakerIdentity `json:"default"`
	}{actionResult{"set_default_speaker", true}, speaker})
}

type cacheFileJSON struct {
	Path      string     `json:"path"`
	Exists    bool       `json:"exists"`
	Entries   int        `json:"entries"`
	SizeBytes int64      `json:"size_bytes"`
	OldestAt  *time.Time `json:"oldest_at"`
}

// Read directly, without NewBrowseCache/InitCache: status must not create files
// and malformed/unreadable caches must not be reported as successfully empty.
func readCacheFileJSON(path string) (cacheFileJSON, error) {
	result := cacheFileJSON{Path: path}
	raw, err := os.ReadFile(path) //nolint:gosec // Constructed from the user's cache directory.
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("reading cache file: %w", err)
	}
	var entries map[string]*struct {
		FetchedAt time.Time `json:"fetched_at"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return result, fmt.Errorf("decoding cache file: %w", err)
	}
	if entries == nil {
		return result, fmt.Errorf("invalid cache file: expected an object")
	}
	result.Exists, result.Entries, result.SizeBytes = true, len(entries), int64(len(raw))
	for _, entry := range entries {
		if entry == nil || entry.FetchedAt.IsZero() {
			return result, fmt.Errorf("invalid cache entry: missing timestamp")
		}
		if result.OldestAt == nil || entry.FetchedAt.Before(*result.OldestAt) {
			stamp := entry.FetchedAt
			result.OldestAt = &stamp
		}
	}
	return result, nil
}

func localCacheDirJSON() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating user cache directory: %w", err)
	}
	return filepath.Join(dir, "kefw2"), nil
}

func runCacheStatusJSON(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	settings, err := currentCacheSettingsJSON()
	if err != nil {
		return err
	}
	dir, err := localCacheDirJSON()
	if err != nil {
		return err
	}
	browse, err := readCacheFileJSON(filepath.Join(dir, "browse_cache.json"))
	if err != nil {
		return err
	}
	rows, err := readCacheFileJSON(filepath.Join(dir, "rows_cache.json"))
	if err != nil {
		return err
	}
	index, err := kefw2.LoadTrackIndex()
	if err != nil {
		return fmt.Errorf("reading track index: %w", err)
	}
	type indexSummary struct {
		Path          string    `json:"path"`
		ServerName    string    `json:"server_name"`
		ContainerName string    `json:"container_name"`
		TrackCount    int       `json:"track_count"`
		IndexedAt     time.Time `json:"indexed_at"`
	}
	var summary *indexSummary
	if index != nil {
		summary = &indexSummary{
			kefw2.TrackIndexPath(), index.ServerName, index.ContainerName, index.TrackCount, index.IndexedAt,
		}
	}
	return writeJSON(cmd, struct {
		Browse     cacheFileJSON     `json:"browse"`
		Rows       cacheFileJSON     `json:"rows"`
		TrackIndex *indexSummary     `json:"track_index"`
		Settings   cacheSettingsJSON `json:"settings"`
	}{browse, rows, summary, settings})
}

func runCacheClearJSON(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	dir, err := localCacheDirJSON()
	if err != nil {
		return err
	}
	// Match the existing command: clear the CLI browse cache only, not the
	// separate library API response cache or the UPnP track index.
	if err := os.Remove(filepath.Join(dir, "browse_cache.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing browse cache: %w", err)
	}
	return writeJSON(cmd, actionResult{"clear_browse_cache", true})
}
