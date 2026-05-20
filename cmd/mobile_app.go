package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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
	cmd.Flags().BoolVar(&o.lockPassword, "lock-password", false,
		"disable POST /api/tournament/reset (SPA /reset page still shows a disabled message) and authenticate via bcrypt hash from TOURNAMENT_PASSWORD_HASH")

	return cmd
}

func (o *mobileAppOptions) run(cmd *cobra.Command, args []string) error {
	// Resolve LOCK_PASSWORD env var when the flag was not explicitly set
	// on the command line. strconv.ParseBool accepts the same set of values
	// as Go's strconv package (1/t/T/TRUE/true/True/0/f/F/FALSE/false/False)
	// and returns an error for anything else, so LOCK_PASSWORD=True or
	// LOCK_PASSWORD=1 work, while LOCK_PASSWORD=yes or LOCK_PASSWORD=enabled
	// are rejected with a clear message rather than silently ignored.
	if cmd != nil && !cmd.Flags().Changed("lock-password") {
		if raw := os.Getenv("LOCK_PASSWORD"); raw != "" {
			v, err := strconv.ParseBool(raw)
			if err != nil {
				return fmt.Errorf("LOCK_PASSWORD=%q: unrecognised boolean value (accepted: 1/t/T/TRUE/true/True or 0/f/F/FALSE/false/False)", raw)
			}
			o.lockPassword = v
		}
	}

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
		log.Printf("Starting mobile-app server in LOCKED mode (auth from TOURNAMENT_PASSWORD_HASH; POST /api/tournament/reset disabled)")
	} else {
		verifier = mobileapp.NewFileVerifier(store)
	}

	log.Printf("Starting mobile-app server on %s:%d using folder %s", o.bindAddress, o.port, o.folder)
	eng := engine.New(store)
	r := mobileapp.NewRouter(store, eng, GetResources(), verifier)

	addr := o.bindAddress + ":" + strconv.Itoa(o.port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
		// Prevent Slowloris: header must arrive within 10 s.
		ReadHeaderTimeout: 10 * time.Second,
		// Individual request body read deadline (not SSE — streaming
		// connections upgrade WriteTimeout via ResponseController).
		ReadTimeout: 30 * time.Second,
		// WriteTimeout=0 leaves SSE connections open indefinitely.
		// gin.Default()'s Recovery middleware still caps panics on the
		// non-streaming path; the safeGo helpers protect viewer goroutines.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("mobile-app server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down mobile-app server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("mobile-app server forced shutdown: %v", err)
		return err
	}
	log.Println("mobile-app server exited cleanly")
	return nil
}

func init() {
	rootCmd.AddCommand(newMobileAppCmd())
}
