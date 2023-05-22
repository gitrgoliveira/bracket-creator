// Package cmd handle the cli commands
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

type poolOptions struct {
	numPlayers int
	filePath   string
}

func defaultPoolOptions() *poolOptions {
	return &poolOptions{}
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "bracket-creator",
	Short: "A tournament bracket creator",
	Long:  `An opinionated template for new Golang cli projects.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	poolOptions := defaultPoolOptions()

	rootCmd.PersistentFlags().IntVar(&poolOptions.numPlayers, "players", 3, "number of players per pool")

	rootCmd.PersistentFlags().StringVar(&poolOptions.filePath, "path", "", "file with the list of players")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
