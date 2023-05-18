package cmd

import (
	"github.com/spf13/cobra"
)

// configCmd represents the config command
var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure kefw2",
	Long: `kefw2 needs to be configured with the IP address of your W2 speaker.
	This will do it.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help() // Just display help for bare config command
	},
}

func init() {
	// rootCmd.AddCommand(configCmd)
	ConfigCmd.AddCommand(speakerCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// configCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// configCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
