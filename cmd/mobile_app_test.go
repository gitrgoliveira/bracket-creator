package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMobileAppCmd(t *testing.T) {
	cmd := newMobileAppCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "mobile-app", cmd.Use)
}

func TestMobileAppOptions_EnvVars(t *testing.T) {
	os.Setenv("BIND_ADDRESS", "1.2.3.4")
	os.Setenv("PORT", "9999")
	defer os.Unsetenv("BIND_ADDRESS")
	defer os.Unsetenv("PORT")

	cmd := newMobileAppCmd()
	bind, _ := cmd.Flags().GetString("bind")
	port, _ := cmd.Flags().GetInt("port")

	assert.Equal(t, "1.2.3.4", bind)
	assert.Equal(t, 9999, port)
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
