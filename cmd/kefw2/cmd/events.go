/*
Copyright © 2023-2025 Jens Hilligsøe

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
)

var eventsCmd = &cobra.Command{
	Use:     "events",
	Aliases: []string{"ev", "watch"},
	Short:   "Follow real-time events from the speaker",
	Long: `Subscribe to and display real-time events from the speaker.

This command connects to the speaker's event stream and displays
changes as they happen: volume, source, playback position, etc.

Press Ctrl+C to stop.`,
	Args: cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		jsonOutput, _ := cmd.Flags().GetBool("json")
		timeout, _ := cmd.Flags().GetInt("timeout")

		// Create event client
		eventClient, err := currentSpeaker.NewEventClient(
			kefw2.WithPollTimeout(timeout),
		)
		if err != nil {
			errorPrinter.Printf("Failed to create event client: %v\n", err)
			os.Exit(1)
		}
		defer eventClient.Close()

		if !jsonOutput {
			headerPrinter.Printf("Speaker: ")
			contentPrinter.Printf("%s (%s)\n", currentSpeaker.Name, currentSpeaker.IPAddress)
			headerPrinter.Printf("Queue ID: ")
			contentPrinter.Printf("%s\n", eventClient.QueueID())
			fmt.Println()
			taskConpletedPrinter.Println("Listening for events... (Ctrl+C to stop)")
			fmt.Println()
		}

		// Setup graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		// Start event processing in a goroutine
		go func() {
			for event := range eventClient.Events() {
				if jsonOutput {
					printEventJSON(event)
				} else {
					printEventHuman(event)
				}
			}
		}()

		// Handle shutdown signal
		go func() {
			<-sigChan
			if !jsonOutput {
				fmt.Println("\nShutting down...")
			}
			cancel()
		}()

		// Start polling (blocks until context cancelled or error)
		if err := eventClient.Start(ctx); err != nil {
			if err != context.Canceled {
				errorPrinter.Printf("Event stream error: %v\n", err)
				os.Exit(1)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(eventsCmd)
	eventsCmd.Flags().BoolP("json", "j", false, "Output events as JSON")
	eventsCmd.Flags().IntP("timeout", "t", 0, "Poll timeout in seconds (0 for infinite)")
}

func printEventJSON(event kefw2.Event) {
	output := map[string]interface{}{
		"time": event.Timestamp().Format("2006-01-02T15:04:05Z07:00"),
		"type": event.Type(),
		"path": event.Path(),
	}

	// Add type-specific fields
	switch e := event.(type) {
	case *kefw2.VolumeEvent:
		output["volume"] = e.Volume
	case *kefw2.SourceEvent:
		output["source"] = string(e.Source)
	case *kefw2.PowerEvent:
		output["status"] = string(e.Status)
	case *kefw2.MuteEvent:
		output["muted"] = e.Muted
	case *kefw2.PlayTimeEvent:
		output["position_ms"] = e.PositionMS
	case *kefw2.PlayerDataEvent:
		output["state"] = e.State
		output["title"] = e.Title
		output["artist"] = e.Artist
		output["album"] = e.Album
		output["duration"] = e.Duration
		output["icon"] = e.Icon
	case *kefw2.PlayModeEvent:
		output["mode"] = e.Mode
	case *kefw2.EQProfileEvent:
		output["profile_name"] = e.Profile.ProfileName
	case *kefw2.PlaylistEvent:
		output["changes"] = e.Changes
		output["version"] = e.Version
	case *kefw2.BluetoothEvent:
		output["bluetooth"] = e.Bluetooth
	}

	jsonBytes, _ := json.Marshal(output)
	fmt.Println(string(jsonBytes))
}

func printEventHuman(event kefw2.Event) {
	timestamp := event.Timestamp().Format("15:04:05")

	switch e := event.(type) {
	case *kefw2.VolumeEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Printf("VOLUME: %d\n", e.Volume)

	case *kefw2.SourceEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Printf("SOURCE: %s\n", e.Source)

	case *kefw2.PowerEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Printf("POWER: %s\n", e.Status)

	case *kefw2.MuteEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		if e.Muted {
			contentPrinter.Println("MUTE: on")
		} else {
			contentPrinter.Println("MUTE: off")
		}

	case *kefw2.PlayTimeEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		if e.PositionMS >= 0 {
			secs := e.PositionMS / 1000
			mins := secs / 60
			secs = secs % 60
			contentPrinter.Printf("PLAY_TIME: %d:%02d\n", mins, secs)
		} else {
			contentPrinter.Println("PLAY_TIME: stopped")
		}

	case *kefw2.PlayerDataEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		if e.Title != "" || e.Artist != "" {
			contentPrinter.Printf("TRACK: %s", e.State)
			if e.Artist != "" {
				contentPrinter.Printf(" - %s", e.Artist)
			}
			if e.Title != "" {
				contentPrinter.Printf(" - %s", e.Title)
			}
			if e.Album != "" {
				contentPrinter.Printf(" (%s)", e.Album)
			}
			contentPrinter.Println()
		} else {
			contentPrinter.Printf("PLAYER: %s\n", e.State)
		}

	case *kefw2.PlayModeEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Printf("PLAY_MODE: %s\n", e.Mode)

	case *kefw2.EQProfileEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		eq := e.Profile
		contentPrinter.Printf("EQ_PROFILE: %s (desk:%v wall:%v bass:%s)\n",
			eq.ProfileName, eq.DeskMode, eq.WallMode, eq.BassExtension)

	case *kefw2.PlaylistEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		adds := 0
		removes := 0
		for _, c := range e.Changes {
			if c.Type == "add" {
				adds++
			} else if c.Type == "remove" {
				removes++
			}
		}
		contentPrinter.Printf("PLAYLIST: %d added, %d removed (v%d)\n", adds, removes, e.Version)

	case *kefw2.BluetoothEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		bt := e.Bluetooth
		contentPrinter.Printf("BLUETOOTH: state=%s connected=%v pairing=%v\n",
			bt.State, bt.Connected, bt.Pairing)

	case *kefw2.NetworkEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Println("NETWORK: info changed")

	case *kefw2.FirmwareEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Println("FIRMWARE: update status changed")

	case *kefw2.NotificationEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Println("NOTIFICATION: display queue changed")

	case *kefw2.UnknownEvent:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Printf("UNKNOWN: path=%s\n", e.RawPath)

	default:
		headerPrinter.Printf("[%s] ", timestamp)
		contentPrinter.Printf("%s: %s\n", event.Type(), event.Path())
	}
}
