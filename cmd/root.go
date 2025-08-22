// Package cmd handle the cli commands
package cmd

import (
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "bracket-creator",
	Short: "A tournament bracket creator",
	Long:  `An opinionated template for new Golang cli projects.`,
}

// appResources holds application resources
var appResources *resources.Resources

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called for backward compatibility
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// ExecuteWithResources adds all child commands to the root command with resources.
// This is the preferred way to execute commands with proper dependency injection.
func ExecuteWithResources(res *resources.Resources) {
	appResources = res
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

// GetResources returns the application resources
func GetResources() *resources.Resources {
	return appResources
}

func init() {
}
