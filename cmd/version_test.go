package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionCmd(t *testing.T) {
	cmd := rootCmd
	for _, c := range cmd.Commands() {
		if c.Name() == "version" {
			assert.NotNil(t, c)
			assert.Equal(t, "version", c.Use)

			// We can't easily test the output of rootCmd.Execute() because it exits
			// but we can test the command's Execute directly if we silence it.
			err := c.Execute()
			assert.NoError(t, err)
			return
		}
	}
	t.Fatal("version command not found")
}
