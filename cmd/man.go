package cmd

import (
	"fmt"
	"os"

	mcobra "github.com/muesli/mango-cobra"
	"github.com/muesli/roff"
	"github.com/spf13/cobra"
)

// manCmd represents the man command
var manCmd = &cobra.Command{
	Use:    "man",
	Short:  "Generates bracket-creator's command line manpages",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		manPage, err := mcobra.NewManPage(1, rootCmd)
		if err != nil {
			panic(err)
		}

		manPage = manPage.WithSection("Copyright", "(C) 2023 Ricardo Oliveira <oliveira.rg@gmail.com>")

		if _, err := fmt.Fprint(os.Stdout, manPage.Build(roff.NewDocument())); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing man page: %v\n", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(manCmd)
}
