package cmd

import (
	"fmt"
	"os"

	"github.com/hilli/go-kef-w2/kefw2"
	"github.com/spf13/cobra"
)

// volumeCmd represents the volume command
var volumeCmd = &cobra.Command{
	Use:   "volume",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		s, err := kefw2.NewSpeaker(os.Getenv("KEFW2_IP"))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		volume, _ := s.GetVolume()
		fmt.Printf("Volume is: %d%%\n", volume)
	},
}

func init() {
	rootCmd.AddCommand(volumeCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// volumeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// volumeCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
