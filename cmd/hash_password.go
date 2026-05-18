package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

// newHashPasswordCmd produces the bcrypt hash used by the mobile-app's
// locked mode (TOURNAMENT_PASSWORD_HASH env var).
//
// Without this helper, operators would have to write a one-off Go program
// or use third-party tools (htpasswd, online generators, etc.) to produce
// a bcrypt hash — friction that discourages adoption of the locked-password
// mode. Bundling it as a subcommand keeps the workflow `bracket-creator
// hash-password mysecret` → copy the line into the env var.
//
// Plaintext input precedence:
//  1. Positional argument (`bracket-creator hash-password mysecret`).
//     Convenient for ad-hoc generation, but leaves the secret in shell
//     history. Suitable for one-shot dev/test use.
//  2. Stdin (no positional arg). Read one line. Stdin is NOT echo-disabled,
//     so the terminal will display what the operator types; for production
//     rotation, pipe from a secrets manager or here-doc rather than typing
//     interactively. We read via cmd.InOrStdin() (cobra's input
//     abstraction) so embedded callers and tests can inject input without
//     mutating the process-global os.Stdin.
//
// Output is the bcrypt hash on stdout (single line, no trailing newline
// beyond what the println adds). Operator can `export TOURNAMENT_PASSWORD_HASH="$(...)"`
// or paste into a secrets manager.
func newHashPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "hash-password [plaintext]",
		Short:        "produce a bcrypt hash for use with --lock-password / TOURNAMENT_PASSWORD_HASH",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var plaintext string
			if len(args) == 1 {
				plaintext = args[0]
			} else {
				// Stdin path. Read a single line, trim only the trailing
				// newline (passwords may legitimately contain whitespace
				// elsewhere; the runtime auth check is exact-string match
				// on the X-Tournament-Password header, so don't strip
				// anything else here).
				reader := bufio.NewReader(cmd.InOrStdin())
				line, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return fmt.Errorf("failed to read password from stdin: %w", err)
				}
				// Trim both \n (Unix) and \r\n (Windows CRLF / piped from
				// PowerShell). Without the \r trim, the hash is generated
				// for "<password>\r" — the browser then sends "<password>"
				// at runtime and authentication fails. Don't TrimSpace —
				// passwords may legitimately contain leading/trailing
				// whitespace and the runtime auth is exact-string match.
				plaintext = strings.TrimRight(line, "\r\n")
			}
			if plaintext == "" {
				return errors.New("password is empty (pass as arg or pipe via stdin)")
			}
			// bcrypt has a hard 72-byte limit on the plaintext input.
			// Unlike the C reference implementation (which silently
			// truncated to 72 bytes), Go's bcrypt.GenerateFromPassword
			// returns ErrPasswordTooLong for longer inputs. Reject up
			// front with a clear message rather than surfacing a
			// bcrypt-internal error.
			if len(plaintext) > 72 {
				return errors.New("password exceeds bcrypt's 72-byte limit; pick a shorter passphrase or use a derived key")
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}
			// Write to cmd.OutOrStdout() (cobra's writer abstraction)
			// rather than fmt.Println so callers (including tests) can
			// redirect output. Production behavior is unchanged because
			// cobra's default writer is os.Stdout.
			if _, werr := fmt.Fprintln(cmd.OutOrStdout(), string(hash)); werr != nil {
				return fmt.Errorf("failed to write hash: %w", werr)
			}
			return nil
		},
	}
	return cmd
}

func init() {
	rootCmd.AddCommand(newHashPasswordCmd())
}
