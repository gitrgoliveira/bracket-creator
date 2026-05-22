package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// Server tuning constants for the mobile-app HTTP listener. mp-663 Phase 2:
// closes slowloris and never-drains-connection vectors that the default
// (zero-timeout) http.Server inherits from r.Run.
const (
	// httpReadHeaderTimeout caps how long the server waits for the request
	// line + headers. Defends against slowloris-header attacks where a
	// client trickles one byte every few seconds.
	httpReadHeaderTimeout = 10 * time.Second
	// httpReadTimeout caps how long the body read may take. POST bodies
	// on this server are JSON (small) or CSV import (~MBs); 30s allows
	// the import on a slow link but kills indefinite slow uploads.
	httpReadTimeout = 30 * time.Second
	// httpIdleTimeout closes keep-alive connections that sit idle. Bounds
	// the file-descriptor commitment per idle client.
	httpIdleTimeout = 120 * time.Second
	// httpMaxHeaderBytes caps request header size — defends against
	// header-bombs (1 MB is generous; default is 1 MB but explicit is
	// clearer).
	httpMaxHeaderBytes = 1 << 20
	// httpShutdownTimeout is how long we wait for in-flight requests to
	// finish before force-closing connections. Long enough for a typical
	// CSV import to land safely, short enough that container restarts
	// don't hang.
	httpShutdownTimeout = 30 * time.Second
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

	slog.Info("mobile-app: initializing",
		"tournamentDataDir", o.folder,
		"bind", o.bindAddress,
		"port", o.port,
		"lockPassword", o.lockPassword,
	)
	store, err := state.NewStore(o.folder)
	if err != nil {
		if hint := diagnoseFolderError(o.folder); hint != "" {
			return fmt.Errorf("failed to initialize state store at %q: %w\n%s", o.folder, err, hint)
		}
		return fmt.Errorf("failed to initialize state store at %q: %w", o.folder, err)
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
		slog.Info("mobile-app: locked mode", "authSource", "TOURNAMENT_PASSWORD_HASH", "resetDisabled", true)
	} else {
		verifier = mobileapp.NewFileVerifier(store)
	}

	slog.Info("mobile-app: starting", "bind", o.bindAddress, "port", o.port, "tournamentDataDir", o.folder)
	eng := engine.New(store)

	// SSE subscriber cap is configurable via SSE_MAX_CLIENTS to handle
	// deployments with unusually high viewer counts; non-numeric values
	// silently fall back to the default rather than failing startup —
	// the cap is a soft DoS mitigation, not a correctness gate.
	maxClients := mobileapp.DefaultMaxSSEClients
	if raw := os.Getenv("SSE_MAX_CLIENTS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			maxClients = v
		} else {
			// slog.Warn escapes attribute values through its encoder
			// (text/JSON), so a malicious env var like `1\nFAKE LOG
			// ENTRY` lands as a quoted attribute value rather than
			// splitting into a second log line.
			slog.Warn("mobile-app: SSE_MAX_CLIENTS not a positive integer; using default",
				"value", raw, "default", maxClients)
		}
	}
	hub := mobileapp.NewHubWithLimits(mobileapp.DefaultHistorySize, maxClients)

	r, _ := mobileapp.NewRouterWithHub(store, eng, GetResources(), verifier, hub)

	// Explicit http.Server with timeouts (mp-663 Phase 2). r.Run uses a
	// zero-value http.Server, which has no read/write/idle timeouts and
	// no graceful-shutdown hook — a single slowloris client can pin a
	// goroutine + fd forever, and SIGTERM kills in-flight requests mid-
	// response. WriteTimeout is intentionally left at 0 because the SSE
	// stream is unbounded; per-request write cancellation happens via
	// Request.Context().Done() which the SSE handler already selects on.
	srv := &http.Server{
		Addr:              o.bindAddress + ":" + strconv.Itoa(o.port),
		Handler:           r,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		IdleTimeout:       httpIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}

	// Close the SSE hub on shutdown — Shutdown waits for active requests
	// to finish, but SSE handlers loop forever on the per-client channel
	// and on the request context. Closing the hub closes each client
	// channel, which makes the per-connection streaming goroutine return
	// (the `case msg, ok := <-ch` arm sees !ok). Without this, Shutdown
	// hangs until httpShutdownTimeout elapses on every SIGTERM.
	srv.RegisterOnShutdown(hub.Close)

	serveErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	// Defer unregistration so BOTH exit paths (clean shutdown OR
	// listener error) drop the signal subscription. Without this the
	// shutdown branch left sigCh registered with the signal package,
	// leaking the channel reference if run() is invoked multiple times
	// in the same process (notably from tests). Idempotent — signal.Stop
	// on an already-stopped channel is a no-op.
	defer signal.Stop(sigCh)

	select {
	case sig := <-sigCh:
		slog.Info("mobile-app: shutting down", "signal", sig.String(), "deadline", httpShutdownTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("mobile-app: shutdown error", "err", err)
			return err
		}
		// Drain the listener goroutine so deferred cleanup completes.
		<-serveErr
		slog.Info("mobile-app: shutdown complete")
		return nil
	case err := <-serveErr:
		return err
	}
}

func init() {
	rootCmd.AddCommand(newMobileAppCmd())
}
