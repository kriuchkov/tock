package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
)

const (
	mcpClientClaudeCode = "claude-code"
	mcpClientOpencode   = "opencode"
	mcpScopeUser        = "user"
	mcpScopeProject     = "project"
	mcpServerName       = "tock"
	opencodeSchemaURL   = "https://opencode.ai/config.json"
	claudeCLI           = "claude"
	cliMCP              = "mcp"
	cliAdd              = "add"
	cliRemove           = "remove"
	cliScope            = "--scope"
)

// mcpClientCLI runs a client's own CLI, so its config files (JSONC for opencode) are never rewritten by tock.
var mcpClientCLI = func(cmd *cobra.Command, name string, args ...string) error {
	proc := exec.CommandContext(cmd.Context(), name, args...)
	proc.Stdout, proc.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
	return proc.Run()
}

var mcpLookPath = exec.LookPath

type mcpInstallOptions struct {
	Client string
	Scope  string
	Print  bool
}

func newMCPInstallCmd() *cobra.Command {
	var opts mcpInstallOptions
	cmd := &cobra.Command{
		Use:   "install",
		Short: defaultText("mcp.install.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runMCPInstall(cmd, &opts) },
	}
	addMCPClientFlags(cmd, &opts)
	cmd.Flags().BoolVar(&opts.Print, "print", false, defaultText("mcp.flag.print"))
	return cmd
}

func newMCPUninstallCmd() *cobra.Command {
	var opts mcpInstallOptions
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: defaultText("mcp.uninstall.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runMCPUninstall(cmd, &opts) },
	}
	addMCPClientFlags(cmd, &opts)
	return cmd
}

func addMCPClientFlags(cmd *cobra.Command, opts *mcpInstallOptions) {
	cmd.Flags().StringVar(&opts.Client, "client", "", defaultText("mcp.flag.client"))
	cmd.Flags().StringVar(&opts.Scope, "scope", mcpScopeUser, defaultText("mcp.flag.scope"))
	_ = cmd.MarkFlagRequired("client")
	_ = cmd.RegisterFlagCompletionFunc("client", cobra.FixedCompletions(
		[]string{mcpClientClaudeCode, mcpClientOpencode}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("scope", cobra.FixedCompletions(
		[]string{mcpScopeUser, mcpScopeProject}, cobra.ShellCompDirectiveNoFileComp))
}

func validateMCPClient(cmd *cobra.Command, opts *mcpInstallOptions) error {
	if !slices.Contains([]string{mcpClientClaudeCode, mcpClientOpencode}, opts.Client) {
		return errors.New(text(cmd, "mcp.install.unknown_client", opts.Client))
	}
	if !slices.Contains([]string{mcpScopeUser, mcpScopeProject}, opts.Scope) {
		return errors.New(text(cmd, "mcp.install.unknown_scope", opts.Scope))
	}
	return nil
}

func runMCPInstall(cmd *cobra.Command, opts *mcpInstallOptions) error {
	if err := validateMCPClient(cmd, opts); err != nil {
		return err
	}
	if !getRuntime(cmd).Config.MCP.Enabled {
		return errors.New(text(cmd, "mcp.install.needs_enabled"))
	}

	server, err := mcpServerCommand(cmd)
	if err != nil {
		return err
	}
	if binaryLooksUnstable(server[0]) {
		fmt.Fprintln(cmd.ErrOrStderr(), text(cmd, "mcp.install.unstable_path", server[0]))
	}

	cli, args := mcpAddCommand(opts, server)
	out := cmd.OutOrStdout()
	if opts.Print {
		return printMCPManualSetup(cmd, text(cmd, "mcp.install.manual"), opts, cli, args, server)
	}
	if !mcpCLIAvailable(cli) {
		return printMCPManualSetup(cmd, text(cmd, "mcp.install.cli_missing", cli), opts, cli, args, server)
	}

	if opts.Client == mcpClientClaudeCode {
		// claude refuses to add a name that exists; drop the old entry so install also updates it.
		_ = mcpClientCLI(silenced(cmd), cli, claudeRemoveArgs(opts.Scope)...)
	}
	if err = mcpClientCLI(cmd, cli, args...); err != nil {
		return errors.Wrapf(err, "run %s", cli)
	}
	fmt.Fprintln(out, text(cmd, "mcp.install.done", opts.Client, opts.Scope))
	return nil
}

func runMCPUninstall(cmd *cobra.Command, opts *mcpInstallOptions) error {
	if err := validateMCPClient(cmd, opts); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.Client == mcpClientOpencode {
		fmt.Fprintln(out, text(cmd, "mcp.uninstall.opencode", opencodeConfigPath(opts.Scope)))
		return nil
	}

	args := claudeRemoveArgs(opts.Scope)
	if !mcpCLIAvailable(claudeCLI) {
		fmt.Fprintln(out, text(cmd, "mcp.uninstall.cli_missing", claudeCLI))
		fmt.Fprintln(out, shellJoin(append([]string{claudeCLI}, args...)))
		return nil
	}
	if err := mcpClientCLI(cmd, claudeCLI, args...); err != nil {
		return errors.Wrapf(err, "run %s", claudeCLI)
	}
	fmt.Fprintln(out, text(cmd, "mcp.uninstall.done", opts.Client, opts.Scope))
	return nil
}

// mcpServerCommand is how a client starts the server: this binary, the root flags that pick the
// data, then "mcp".
func mcpServerCommand(cmd *cobra.Command) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, errors.Wrap(err, "resolve executable")
	}
	server := append([]string{exe}, rootPassthroughFlags(cmd)...)
	return append(server, cliMCP), nil
}

// mcpAddCommand returns the client CLI and its arguments that register server.
func mcpAddCommand(opts *mcpInstallOptions, server []string) (string, []string) {
	if opts.Client == mcpClientOpencode {
		args := []string{cliMCP, cliAdd, mcpServerName}
		if opts.Scope == mcpScopeUser {
			args = append(args, "--global")
		}
		return mcpClientOpencode, append(append(args, "--"), server...)
	}
	args := []string{cliMCP, cliAdd, cliScope, opts.Scope, mcpServerName, "--"}
	return claudeCLI, append(args, server...)
}

func claudeRemoveArgs(scope string) []string {
	return []string{cliMCP, cliRemove, mcpServerName, cliScope, scope}
}

func mcpCLIAvailable(name string) bool {
	_, err := mcpLookPath(name)
	return err == nil
}

func printMCPManualSetup(cmd *cobra.Command, intro string, opts *mcpInstallOptions, cli string, args, server []string) error {
	snippet, path := mcpConfigSnippet(opts, server)
	payload, err := json.MarshalIndent(snippet, "", "  ")
	if err != nil {
		return errors.Wrap(err, "encode config snippet")
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, intro)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  "+shellJoin(append([]string{cli}, args...)))
	fmt.Fprintln(out)
	fmt.Fprintln(out, text(cmd, "mcp.install.config_entry", path))
	fmt.Fprintln(out, string(payload))
	return nil
}

// mcpConfigSnippet is the entry to add by hand, and the file it goes to.
func mcpConfigSnippet(opts *mcpInstallOptions, server []string) (map[string]any, string) {
	if opts.Client == mcpClientOpencode {
		return map[string]any{
			"$schema": opencodeSchemaURL,
			cliMCP: map[string]any{"servers": map[string]any{
				mcpServerName: map[string]any{"type": "local", "command": server},
			}},
		}, opencodeConfigPath(opts.Scope)
	}

	path := ".mcp.json"
	if opts.Scope == mcpScopeUser {
		path = "~/.claude.json"
	}
	return map[string]any{"mcpServers": map[string]any{
		mcpServerName: map[string]any{"type": "stdio", "command": server[0], "args": server[1:]},
	}}, path
}

func opencodeConfigPath(scope string) string {
	if scope == mcpScopeProject {
		return "opencode.json"
	}
	return filepath.Join("~", ".config", "opencode", "opencode.json")
}

// silenced is cmd with its output dropped, for a client call whose failure is expected.
func silenced(cmd *cobra.Command) *cobra.Command {
	quiet := &cobra.Command{}
	quiet.SetContext(cmd.Context())
	quiet.SetOut(io.Discard)
	quiet.SetErr(io.Discard)
	return quiet
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

// shellJoin quotes args for a POSIX shell.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if shellSafe.MatchString(arg) {
			quoted[i] = arg
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}
