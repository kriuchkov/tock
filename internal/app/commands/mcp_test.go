package commands

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
)

func newMCPTestCommand(t *testing.T, enabled bool) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := newTestCLICommand(&stubActivityResolver{})
	rt, ok := appruntime.FromContext(cmd.Context())
	require.True(t, ok)
	rt.Config.MCP.Enabled = enabled

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	return cmd, &out
}

func stubMCPClient(t *testing.T, found bool) *[][]string {
	t.Helper()
	cli, look := mcpClientCLI, mcpLookPath
	t.Cleanup(func() { mcpClientCLI, mcpLookPath = cli, look })

	var calls [][]string
	mcpClientCLI = func(_ *cobra.Command, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}
	mcpLookPath = func(name string) (string, error) {
		if !found {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/" + name, nil
	}
	return &calls
}

func TestRunMCPCmdRefusesWhenDisabled(t *testing.T) {
	cmd, out := newMCPTestCommand(t, false)
	err := runMCPCmd(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mcp.enabled: true")
	assert.Empty(t, out.String(), "nothing may reach stdout, the client reads JSON-RPC there")
}

func TestMCPInstallNeedsEnabled(t *testing.T) {
	calls := stubMCPClient(t, true)
	cmd, _ := newMCPTestCommand(t, false)

	err := runMCPInstall(cmd, &mcpInstallOptions{Client: mcpClientClaudeCode, Scope: mcpScopeUser})
	require.Error(t, err)
	assert.Empty(t, *calls)
}

func TestMCPInstallClaudeCodeReplacesEntry(t *testing.T) {
	calls := stubMCPClient(t, true)
	cmd, out := newMCPTestCommand(t, true)

	require.NoError(t, runMCPInstall(cmd, &mcpInstallOptions{Client: mcpClientClaudeCode, Scope: mcpScopeProject}))
	require.Len(t, *calls, 2)
	assert.Equal(t, []string{"claude", "mcp", "remove", "tock", "--scope", "project"}, (*calls)[0])
	add := (*calls)[1]
	assert.Equal(t, []string{"claude", "mcp", "add", "--scope", "project", "tock", "--"}, add[:7])
	assert.Equal(t, "mcp", add[len(add)-1])
	assert.Contains(t, out.String(), "Registered the tock MCP server in claude-code (project scope)")
}

func TestMCPInstallOpencodeGlobal(t *testing.T) {
	calls := stubMCPClient(t, true)
	cmd, _ := newMCPTestCommand(t, true)

	require.NoError(t, runMCPInstall(cmd, &mcpInstallOptions{Client: mcpClientOpencode, Scope: mcpScopeUser}))
	require.Len(t, *calls, 1)
	assert.Equal(t, []string{"opencode", "mcp", "add", "tock", "--global", "--"}, (*calls)[0][:6])
}

func TestMCPInstallPrintsSetupWhenCLIMissing(t *testing.T) {
	calls := stubMCPClient(t, false)
	cmd, out := newMCPTestCommand(t, true)

	require.NoError(t, runMCPInstall(cmd, &mcpInstallOptions{Client: mcpClientOpencode, Scope: mcpScopeUser}))
	assert.Empty(t, *calls)
	assert.Contains(t, out.String(), "opencode was not found in PATH")
	assert.Contains(t, out.String(), `"servers"`)
	assert.Contains(t, out.String(), `"type": "local"`)
}

func TestMCPInstallRejectsUnknownClient(t *testing.T) {
	stubMCPClient(t, true)
	cmd, _ := newMCPTestCommand(t, true)
	require.Error(t, runMCPInstall(cmd, &mcpInstallOptions{Client: "cursor", Scope: mcpScopeUser}))
	require.Error(t, runMCPInstall(cmd, &mcpInstallOptions{Client: mcpClientOpencode, Scope: "global"}))
}

func TestMCPUninstall(t *testing.T) {
	calls := stubMCPClient(t, true)
	cmd, out := newMCPTestCommand(t, false)

	require.NoError(t, runMCPUninstall(cmd, &mcpInstallOptions{Client: mcpClientClaudeCode, Scope: mcpScopeUser}))
	assert.Equal(t, [][]string{{"claude", "mcp", "remove", "tock", "--scope", "user"}}, *calls)

	out.Reset()
	require.NoError(t, runMCPUninstall(cmd, &mcpInstallOptions{Client: mcpClientOpencode, Scope: mcpScopeUser}))
	assert.Contains(t, out.String(), "opencode.json")
	assert.Len(t, *calls, 1)
}

func TestShellJoinQuotes(t *testing.T) {
	assert.Equal(t, `claude mcp add '/Users/me/My Apps/tock' 'it'\''s'`,
		shellJoin([]string{"claude", "mcp", "add", "/Users/me/My Apps/tock", "it's"}))
}
