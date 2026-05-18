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
	folder       string
	bindAddress  string
	port         int
	lockPassword bool
}

func newMobileAppCmd() *cobra.Command {
	o := &mobileAppOptions{}

	cmd := &cobra.Command{
		Use:          "mobile-app",
		Short:        "serves the tournament management web app",
		SilenceUsage: true,
		RunE:         o.run,
	}

	folder := os.Getenv("TOURNAMENT_DATA_DIR")
	if folder == "" {
		folder = "."
	}
	cmd.Flags().StringVarP(&o.folder, "folder", "f", folder, "folder to store tournament state (env: TOURNAMENT_DATA_DIR)")

	bindAddress := os.Getenv("BIND_ADDRESS")
	if bindAddress == "" {
		bindAddress = "localhost"
	}
	cmd.Flags().StringVarP(&o.bindAddress, "bind", "b", bindAddress, "bind address (env: BIND_ADDRESS)")

	portStr := os.Getenv("PORT")
	port := 8080
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}
	cmd.Flags().IntVarP(&o.port, "port", "p", port, "port number (env: PORT)")

	// --lock-password switches the server into "locked" auth mode:
	//   * /api/tournament/reset returns 404
	//   * GET /api/auth-config reports mode=locked, resetEnabled=false
	//   * Authentication compares X-Tournament-Password against a bcrypt
	//     hash supplied via the TOURNAMENT_PASSWORD_HASH env var. The
	//     on-disk tournament.md password is ignored.
	// The flag is recommended for any internet-exposed deployment; for
	// local/private use the default (unlocked) behavior keeps the
	// recovery-via-/reset path available.
	cmd.Flags().BoolVar(&o.lockPassword, "lock-password", os.Getenv("LOCK_PASSWORD") == "true",
		"disable /reset and authenticate via bcrypt hash from TOURNAMENT_PASSWORD_HASH")

	return cmd
}

func (o *mobileAppOptions) run(cmd *cobra.Command, args []string) error {
	store, err := state.NewStore(o.folder)
	if err != nil {
		return fmt.Errorf("failed to initialize state store: %w", err)
	}

	// Select the auth source. Fail-closed: when --lock-password is set
	// but TOURNAMENT_PASSWORD_HASH is empty or malformed, refuse to
	// start rather than silently falling back to file mode (which would
	// expose the admin endpoints to whatever's in tournament.md, or to
	// the new-tournament bootstrap path on an empty install).
	var verifier mobileapp.PasswordVerifier
	if o.lockPassword {
		hash := os.Getenv("TOURNAMENT_PASSWORD_HASH")
		v, err := mobileapp.NewBcryptVerifier(hash)
		if err != nil {
			return fmt.Errorf("--lock-password set but TOURNAMENT_PASSWORD_HASH invalid: %w", err)
		}
		verifier = v
		log.Printf("Starting mobile-app server in LOCKED mode (auth from TOURNAMENT_PASSWORD_HASH; /reset disabled)")
	} else {
		verifier = mobileapp.NewFileVerifier(store)
	}

	log.Printf("Starting mobile-app server on %s:%d using folder %s", o.bindAddress, o.port, o.folder)
	eng := engine.New(store)
	r := mobileapp.NewRouter(store, eng, GetResources(), verifier)
	return r.Run(o.bindAddress + ":" + strconv.Itoa(o.port))
}

func init() {
	rootCmd.AddCommand(newMobileAppCmd())
}
