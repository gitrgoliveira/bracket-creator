package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// runHashPassword executes the hash-password subcommand with the given
// args and stdin string. Always injects stdin via cmd.SetIn (even for
// the empty-string case) so the empty-stdin test exercises a deliberate
// EOF rather than the real process stdin — without the always-swap, the
// test could block in environments where stdin is open. Internal helper
// used by every test case below.
func runHashPassword(t *testing.T, stdin string, args []string) (string, error) {
	t.Helper()

	cmd := newHashPasswordCmd()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)

	err := cmd.Execute()
	return stdout.String(), err
}

func TestHashPassword_FromArg_ProducesValidBcryptHash(t *testing.T) {
	out, err := runHashPassword(t, "", []string{"mysecret"})
	require.NoError(t, err)
	out = strings.TrimSpace(out)
	require.NotEmpty(t, out)
	assert.Truef(t, strings.HasPrefix(out, "$2a$"), "expected bcrypt hash, got %q", out)
	// Round-trip: the produced hash authenticates the same plaintext.
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(out), []byte("mysecret")))
}

func TestHashPassword_FromStdin_TrimsLF(t *testing.T) {
	out, err := runHashPassword(t, "mysecret\n", []string{})
	require.NoError(t, err)
	out = strings.TrimSpace(out)
	require.NotEmpty(t, out)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(out), []byte("mysecret")),
		"hash should authenticate the plaintext without the trailing newline")
}

// TestHashPassword_FromStdin_TrimsCRLF guards against the Windows
// regression: PowerShell / cmd.exe pipe \r\n line endings, and an
// untrimmed \r in the hash input produces a hash that authenticates
// "secret\r" — but the browser then sends "secret" and auth fails.
// The fix is in hash_password.go's TrimRight on both \r and \n.
func TestHashPassword_FromStdin_TrimsCRLF(t *testing.T) {
	out, err := runHashPassword(t, "mysecret\r\n", []string{})
	require.NoError(t, err)
	out = strings.TrimSpace(out)
	require.NotEmpty(t, out)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(out), []byte("mysecret")),
		"hash must authenticate the plaintext without the trailing CRLF (Windows fix)")
	// And the converse: the hash must NOT authenticate the un-trimmed
	// form, otherwise we'd silently accept "<password>\r" too.
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(out), []byte("mysecret\r")),
		"hash should not authenticate the CRLF-suffixed form")
}

func TestHashPassword_EmptyArg_Errors(t *testing.T) {
	_, err := runHashPassword(t, "", []string{""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestHashPassword_EmptyStdin_Errors(t *testing.T) {
	// Empty stdin: read returns io.EOF immediately, plaintext stays "".
	_, err := runHashPassword(t, "", []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestHashPassword_TooLong_Errors(t *testing.T) {
	huge := strings.Repeat("x", 73) // bcrypt's hard limit is 72 bytes
	_, err := runHashPassword(t, "", []string{huge})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "72-byte")
}

func TestHashPassword_OutputIsSingleLine(t *testing.T) {
	// The hash is printed via Println so it ends in exactly one newline.
	// Any extra output would break operators piping into an env var:
	// `TOURNAMENT_PASSWORD_HASH=$(bracket-creator hash-password foo)`.
	out, err := runHashPassword(t, "", []string{"foo"})
	require.NoError(t, err)
	// Exactly one trailing newline; everything before is one hash line.
	assert.True(t, strings.HasSuffix(out, "\n"))
	body := strings.TrimSuffix(out, "\n")
	assert.NotContains(t, body, "\n", "hash output should be a single line")
}
