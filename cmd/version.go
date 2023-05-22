package cmd

import (
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the application version.",
	Long:  `Print the application version.`,
	Run: func(cmd *cobra.Command, args []string) {
		version.Print()
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
