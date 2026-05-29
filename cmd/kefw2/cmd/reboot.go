/*
Copyright © 2023-2026 Jens Hilligsøe

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
	"fmt"

	"github.com/spf13/cobra"
)

var dayNames = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func dayName(d int) string {
	if d < 0 || d >= len(dayNames) {
		return "Unknown"
	}
	return dayNames[d]
}

// rebootCmd triggers an immediate reboot of the speaker.
var rebootCmd = &cobra.Command{
	Use:   "reboot",
	Short: "Reboot the speaker now",
	Long:  `Reboot the speaker immediately. Use 'reboot schedule' to manage automatic scheduled reboots.`,
	Args:  cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()
		err := currentSpeaker.Reboot(ctx)
		exitOnError(err, "Failed to reboot speaker")
		taskConpletedPrinter.Println("Reboot command sent")
	},
}

var (
	rebootScheduleEnable  bool
	rebootScheduleDisable bool
	rebootScheduleDay     int
	rebootScheduleTime    string
)

// rebootScheduleCmd manages the automatic scheduled reboot.
var rebootScheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Get or set the scheduled automatic reboot",
	Long: `Display or update the speaker's scheduled automatic reboot.

With no flags, prints the current schedule. Use --enable/--disable to toggle
the schedule, and --day/--time to change when it runs. Day uses 0=Sunday
through 6=Saturday (encoding not yet confirmed against the KEF Connect app).`,
	Args: cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, _ []string) {
		ctx := cmd.Context()

		dayChanged := cmd.Flags().Changed("day")
		timeChanged := cmd.Flags().Changed("time")
		enableChanged := cmd.Flags().Changed("enable")
		disableChanged := cmd.Flags().Changed("disable")

		if rebootScheduleEnable && rebootScheduleDisable {
			exitOnError(fmt.Errorf("--enable and --disable are mutually exclusive"), "Invalid flags")
		}

		anyChange := dayChanged || timeChanged || enableChanged || disableChanged

		if anyChange {
			sr, err := currentSpeaker.GetScheduledReboot(ctx)
			exitOnError(err, "Failed to read current scheduled reboot")

			if dayChanged {
				sr.DayOfWeek = rebootScheduleDay
			}
			if timeChanged {
				sr.Time = rebootScheduleTime
			}
			if enableChanged {
				sr.Enabled = rebootScheduleEnable
			}
			if disableChanged && rebootScheduleDisable {
				sr.Enabled = false
			}

			err = currentSpeaker.SetScheduledReboot(ctx, sr)
			exitOnError(err, "Failed to set scheduled reboot")
		}

		sr, err := currentSpeaker.GetScheduledReboot(ctx)
		exitOnError(err, "Failed to read scheduled reboot")

		state := "disabled"
		if sr.Enabled {
			state = "enabled"
		}
		headerPrinter.Print("Scheduled reboot: ")
		contentPrinter.Println(state)
		headerPrinter.Print("Day: ")
		contentPrinter.Printf("%s (%d)\n", dayName(sr.DayOfWeek), sr.DayOfWeek)
		headerPrinter.Print("Time: ")
		contentPrinter.Println(sr.Time)
	},
}

func init() {
	rootCmd.AddCommand(rebootCmd)
	rebootCmd.AddCommand(rebootScheduleCmd)

	rebootScheduleCmd.Flags().BoolVar(&rebootScheduleEnable, "enable", false, "Enable the scheduled reboot")
	rebootScheduleCmd.Flags().BoolVar(&rebootScheduleDisable, "disable", false, "Disable the scheduled reboot")
	rebootScheduleCmd.Flags().IntVar(&rebootScheduleDay, "day", 0, "Day of week (0=Sunday..6=Saturday)")
	rebootScheduleCmd.Flags().StringVar(&rebootScheduleTime, "time", "", "Time in HH:MM (24-hour) format")
}
