package cmd

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/mobileapp"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/spf13/cobra"
)

type mobileAppOptions struct {
	folder      string
	bindAddress string
	port        int
}

func newMobileAppCmd() *cobra.Command {
	o := &mobileAppOptions{}

	cmd := &cobra.Command{
		Use:          "mobile-app",
		Short:        "serves the tournament management web app",
		SilenceUsage: true,
		RunE:         o.run,
	}

	cmd.Flags().StringVarP(&o.folder, "folder", "f", ".", "folder to store tournament state")

	bindAddress := os.Getenv("BIND_ADDRESS")
	if bindAddress == "" {
		bindAddress = "localhost"
	}
	cmd.Flags().StringVarP(&o.bindAddress, "bind", "b", bindAddress, "bind address")

	portStr := os.Getenv("PORT")
	port := 8080
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}
	cmd.Flags().IntVarP(&o.port, "port", "p", port, "port number")

	return cmd
}

func (o *mobileAppOptions) run(cmd *cobra.Command, args []string) error {
	store, err := state.NewStore(o.folder)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}

	log.Printf("Starting mobile-app server on %s:%d using folder %s", o.bindAddress, o.port, o.folder)
	eng := engine.New(store)
	r := mobileapp.NewRouter(store, eng, GetResources())
	return r.Run(o.bindAddress + ":" + strconv.Itoa(o.port))
}

func init() {
	rootCmd.AddCommand(newMobileAppCmd())
}
