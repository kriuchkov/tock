package commands

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunTrayCmd_DisabledByDefault(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})

	var out bytes.Buffer
	cmd.SetOut(&out)

	// With the default (empty) config, tray.enabled is false, so the command
	// must not attempt to launch the menu bar loop and just prints a hint.
	err := runTrayCmd(cmd)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "disabled")
}
