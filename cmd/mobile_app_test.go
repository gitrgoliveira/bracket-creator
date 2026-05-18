package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
