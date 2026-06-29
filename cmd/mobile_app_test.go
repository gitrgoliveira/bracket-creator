package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMobileAppCmd(t *testing.T) {
	cmd := newMobileAppCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "mobile-app", cmd.Use)
}

func TestMobileAppOptions_EnvVars(t *testing.T) {
	t.Setenv("BIND_ADDRESS", "1.2.3.4")
	t.Setenv("PORT", "9999")
	t.Setenv("TOURNAMENT_DATA_DIR", "/tmp/td-env-test")

	cmd := newMobileAppCmd()
	bind, _ := cmd.Flags().GetString("bind")
	port, _ := cmd.Flags().GetInt("port")
	folder, _ := cmd.Flags().GetString("folder")

	assert.Equal(t, "1.2.3.4", bind)
	assert.Equal(t, 9999, port)
	assert.Equal(t, "/tmp/td-env-test", folder)
}

func TestMobileAppOptions_FolderDefault(t *testing.T) {
	t.Setenv("TOURNAMENT_DATA_DIR", "")

	cmd := newMobileAppCmd()
	folder, _ := cmd.Flags().GetString("folder")

	assert.Equal(t, ".", folder)
}

func TestMobileAppOptions_PortDefault(t *testing.T) {
	t.Setenv("PORT", "")

	cmd := newMobileAppCmd()
	port, _ := cmd.Flags().GetInt("port")

	assert.Equal(t, 8080, port)
}

func TestMobileAppOptions_PortInvalid(t *testing.T) {
	t.Setenv("PORT", "not-a-number")

	cmd := newMobileAppCmd()
	port, _ := cmd.Flags().GetInt("port")

	assert.Equal(t, 8080, port)
}

func TestMobileAppOptions_RunError(t *testing.T) {
	o := &mobileAppOptions{
		folder: "/non/existent/dir",
	}
	// This might not error immediately depending on how NewStore is implemented
	err := o.run(nil, nil)
	// It will likely fail at r.Run because it can't bind or something,
	// but NewStore might also fail.
	assert.NotNil(t, err)
}

// Fail-closed: --lock-password without TOURNAMENT_PASSWORD_HASH must
// refuse to start. The alternative (silent fall-through to file mode)
// would let an operator believe they were running in locked mode while
// the server actually serves the tournament.md plaintext password,
// exactly the misconfiguration the flag was added to prevent.
func TestMobileAppOptions_LockPasswordRequiresHash(t *testing.T) {
	t.Setenv("TOURNAMENT_PASSWORD_HASH", "")
	dir := t.TempDir()
	o := &mobileAppOptions{
		folder:       dir,
		bindAddress:  "127.0.0.1",
		port:         0,
		lockPassword: true,
	}
	err := o.run(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TOURNAMENT_PASSWORD_HASH")
}

// Locked mode also rejects a malformed hash at startup (caught by
// bcrypt.Cost). The operator gets a clear error rather than a 401-on-
// every-request runtime puzzle.
func TestMobileAppOptions_LockPasswordRejectsBadHash(t *testing.T) {
	t.Setenv("TOURNAMENT_PASSWORD_HASH", "not-a-bcrypt-hash")
	dir := t.TempDir()
	o := &mobileAppOptions{
		folder:       dir,
		bindAddress:  "127.0.0.1",
		port:         0,
		lockPassword: true,
	}
	err := o.run(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid bcrypt hash")
}

// TestMobileAppOptions_RunReachesServer exercises the server-startup code
// path (lines after store/verifier creation) by using a valid store and an
// invalid port that causes ListenAndServe to return immediately with an error.
// This covers the file-verifier selection, hub creation, and select branch.
func TestMobileAppOptions_RunReachesServer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SSE_MAX_CLIENTS", "not-a-number") // exercises the Warn branch
	o := &mobileAppOptions{
		folder:      dir,
		bindAddress: "localhost",
		port:        99999, // >65535: net.Listen fails immediately
	}
	err := o.run(nil, nil)
	// The server fails to bind, expect a non-nil listen error.
	require.Error(t, err)
}

// TestMobileAppOptions_RunSSEMaxClientsValid exercises the valid-positive-int
// branch of SSE_MAX_CLIENTS parsing within run().
func TestMobileAppOptions_RunSSEMaxClientsValid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SSE_MAX_CLIENTS", "10")
	o := &mobileAppOptions{
		folder:      dir,
		bindAddress: "localhost",
		port:        99999,
	}
	err := o.run(nil, nil)
	require.Error(t, err) // fails to bind, as intended
}

// LOCK_PASSWORD env var is parsed via strconv.ParseBool so that values
// like "TRUE", "True", "1", "t" all activate locked mode (not just "true").
// Invalid values (e.g. "yes", "enabled") must produce a clear startup
// error rather than being silently treated as false, an operator who
// typo'd the env var should get a loud rejection, not a server that
// quietly starts in the wrong mode.
func TestMobileAppOptions_LockPasswordEnvVarFormats(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		wantMsg string // substring expected in the error message
	}{
		// Valid truthy values: run() proceeds to the TOURNAMENT_PASSWORD_HASH
		// check (which fails because we leave the hash empty), not the parse step.
		{"uppercase TRUE", "TRUE", "TOURNAMENT_PASSWORD_HASH"},
		{"mixed-case True", "True", "TOURNAMENT_PASSWORD_HASH"},
		{"numeric 1", "1", "TOURNAMENT_PASSWORD_HASH"},
		{"lowercase t", "t", "TOURNAMENT_PASSWORD_HASH"},
		// Invalid values: must be caught before the hash check.
		{"word yes", "yes", "unrecognised boolean value"},
		{"word enabled", "enabled", "unrecognised boolean value"},
		{"word on", "on", "unrecognised boolean value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("LOCK_PASSWORD", tt.envVal)
			t.Setenv("TOURNAMENT_PASSWORD_HASH", "")
			o := &mobileAppOptions{
				folder:      dir,
				bindAddress: "127.0.0.1",
				port:        0,
			}
			// Pass the real command so cmd.Flags().Changed("lock-password")
			// returns false (flag was not set on the command line) and the
			// env-var parsing branch executes.
			c := newMobileAppCmd()
			err := o.run(c, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestParseScheduleEnabled(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"1 is truthy", "1", true},
		{"true is truthy", "true", true},
		{"TRUE is truthy", "TRUE", true},
		{"yes is truthy", "yes", true},
		{"on is truthy", "on", true},
		{"empty string is false", "", false},
		{"0 is false", "0", false},
		{"false is false", "false", false},
		{"no is false", "no", false},
		{"off is false", "off", false},
		{"garbage is false", "garbage", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScheduleEnabled(tt.input)
			assert.Equal(t, tt.want, got,
				"parseScheduleEnabled(%q) = %v; want %v", tt.input, got, tt.want)
		})
	}
}
