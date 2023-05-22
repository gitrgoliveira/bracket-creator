package cmd

import (
	"github.com/spf13/cobra"
)

// createCmd represents the version command
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create the bracket.",
	Long:  `create the bracket.`,
	Run: func(cmd *cobra.Command, args []string) {
		create.makeBracket()
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
}
