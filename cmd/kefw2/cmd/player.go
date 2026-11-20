// Copyright (c) 2026 Jens Hilligsøe. Licensed under the MIT license.

package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

const (
	playerListeningDesk    = "listening-desk"
	playerDiscoveryBrowser = "discovery-browser"
)

var playerCmd = &cobra.Command{
	Use:   "player",
	Short: "Open the interactive speaker player",
	Long: `Open the interactive player with a Listening Desk or Discovery Browser layout.
Use S to choose a configured speaker without changing the saved default.`,
	Args:              validatePlayerArgs,
	ValidArgsFunction: noFileCompletion,
	RunE:              runPlayer,
}

func init() {
	rootCmd.AddCommand(playerCmd)
	playerCmd.Flags().String("layout", playerListeningDesk, "player layout: listening-desk or discovery-browser")
	_ = playerCmd.RegisterFlagCompletionFunc("layout", completePlayerLayout)
}

func validatePlayerArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return err
	}
	layout, err := cmd.Flags().GetString("layout")
	if err != nil {
		return err
	}
	if layout != playerListeningDesk && layout != playerDiscoveryBrowser {
		return fmt.Errorf("invalid player layout %q; choose %s or %s", layout, playerListeningDesk, playerDiscoveryBrowser)
	}
	return nil
}

func completePlayerLayout(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	var matches []string
	for _, layout := range []string{playerListeningDesk, playerDiscoveryBrowser} {
		if strings.HasPrefix(layout, prefix) {
			matches = append(matches, layout)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}

func runPlayer(cmd *cobra.Command, _ []string) error {
	selection, err := initialPlayerSelection(
		speakers, viper.GetString("defaultSpeaker"), currentSpeakerParam, viper.GetString("speaker"),
	)
	if err != nil {
		return err
	}
	if !playerTerminal(cmd.InOrStdin()) || !playerTerminal(cmd.OutOrStdout()) {
		return fmt.Errorf("player requires a terminal for both input and output")
	}
	layout, err := cmd.Flags().GetString("layout")
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	model := newPlayerModel(ctx, layout, selection, newPlayerSession)
	configPath := cfgFile
	if configPath == "" {
		configPath = viper.ConfigFileUsed()
	}
	if configPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("locating player layout: %w", err)
		}
		configPath = filepath.Join(configDir, "kefw2", "kefw2.yaml")
	}
	model.ui = newPlayerLayoutUI(layout, configPath)
	if model.chooserOpen {
		model.ui.focus.Push("speaker-chooser")
	}
	model.ui.controls = newPlayerControlsUI()
	model.ui.discovery = newPlayerDiscoveryUI()
	model.ui.queue = newPlayerQueueUI()
	model.addExtension(model.ui.extension())
	model.addExtension(model.ui.controls.extension())
	model.addExtension(model.ui.discovery.extension())
	model.addExtension(model.ui.queue.extension())
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()), tea.WithoutSignalHandler())
	_, runErr := program.Run()
	shutdownErr := model.shutdown()
	if errors.Is(runErr, tea.ErrProgramKilled) && ctx.Err() != nil {
		runErr = nil
	}
	return errors.Join(runErr, shutdownErr)
}

func playerTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}
