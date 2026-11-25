// Package mcpserver exposes tock over the Model Context Protocol, so AI clients can read and
// change tracked time through the same services the CLI uses.
package mcpserver

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-faster/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	serverName  = "tock"
	serverTitle = "Tock"

	instructions = `Tock is the user's local time tracker. Times are in the user's local time zone; every
result carries "now" and "timezone", use them to read words like "today" or "yesterday".
Accepted time inputs: RFC 3339, "YYYY-MM-DD HH:MM", or "HH:MM" for today. Only one activity runs at a
time: starting one stops the running one. Activities are addressed by their "key" (YYYY-MM-DD-NN,
the NN-th activity started that day), as returned by list.`
)

// Options configures the server.
type Options struct {
	Activities ports.ActivityResolver
	// Reminders may be nil; the reminder tools are left out then.
	Reminders ports.ReminderResolver
	// WorkingHours returns the configured end of the working day; enabled is false when auto-stop is off.
	WorkingHours func() (hours models.WorkingHours, enabled bool, err error)
	// ReadOnly leaves out every tool that changes data.
	ReadOnly bool
	// DisabledTools are tool names to leave out.
	DisabledTools []string
	Version       string
	// Snooze is how long remind_snooze snoozes by default; 10 minutes when zero.
	Snooze time.Duration
	// Now returns the current time; time.Now when nil.
	Now func() time.Time
	// OnStarted runs after a tool started an activity.
	OnStarted func()
	// AfterCall runs after every tool call while calls are still serialized: tock syncs there what the
	// call changed, as after a CLI command. Every call, since a read can write too through the
	// working-hours auto-stop. Its failures are its own to report.
	AfterCall func(ctx context.Context)
}

type server struct {
	opts     Options
	mcp      *mcp.Server
	disabled map[string]bool
	zone     string
	// calls runs one tool at a time: clients send calls in parallel, and rules such as
	// "starting stops the running activity" read and then write.
	calls sync.Mutex
}

// New builds the MCP server with the tools and prompts the options allow.
func New(opts Options) *mcp.Server {
	if opts.Now == nil {
		opts.Now = time.Now
	}

	s := &server{opts: opts, disabled: make(map[string]bool, len(opts.DisabledTools)), zone: zoneName(opts.Now())}
	for _, name := range opts.DisabledTools {
		s.disabled[strings.TrimSpace(name)] = true
	}

	s.mcp = mcp.NewServer(
		&mcp.Implementation{Name: serverName, Title: serverTitle, Version: opts.Version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	s.addActivityReadTools()
	s.addActivityWriteTools()
	if opts.Reminders != nil {
		s.addReminderTools()
	}
	s.addPrompts()
	return s.mcp
}

// Run serves over transport until the client disconnects or ctx is done.
func Run(ctx context.Context, opts Options, transport mcp.Transport) error {
	if err := New(opts).Run(ctx, transport); err != nil {
		return errors.Wrap(err, "run mcp server")
	}
	return nil
}

// access is what a tool may do to the user's data.
type access int

const (
	readAccess access = iota
	writeAccess
	destructiveAccess
)

// addTool registers tool unless the options leave it out. Calls run one at a time, and each first
// stops an activity left running past the working day, as every CLI command does: the server lives
// longer than one command.
func addTool[In, Out any](s *server, a access, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	if s.disabled[tool.Name] || (s.opts.ReadOnly && a != readAccess) {
		return
	}

	closedWorld := false
	tool.Annotations = &mcp.ToolAnnotations{
		Title:          tool.Title,
		ReadOnlyHint:   a == readAccess,
		IdempotentHint: a == readAccess,
		OpenWorldHint:  &closedWorld,
	}
	if a != readAccess {
		destructive := a == destructiveAccess
		tool.Annotations.DestructiveHint = &destructive
	}

	mcp.AddTool(s.mcp, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		s.calls.Lock()
		defer s.calls.Unlock()

		defer s.afterCall(ctx)
		if err := s.reconcile(ctx); err != nil {
			var zero Out
			return nil, zero, err
		}
		return handler(ctx, req, in)
	})
}

func (s *server) afterCall(ctx context.Context) {
	if s.opts.AfterCall != nil {
		s.opts.AfterCall(ctx)
	}
}

func (s *server) reconcile(ctx context.Context) error {
	if s.opts.WorkingHours == nil {
		return nil
	}
	hours, enabled, err := s.opts.WorkingHours()
	if err != nil || !enabled {
		return err
	}
	_, err = s.opts.Activities.StopAfterWorkingHours(ctx, hours, s.opts.Now())
	switch {
	case errors.Is(err, coreErrors.ErrActivityOwnerUnknown):
		// The activity is left running until sync can tell its owner; the call goes on.
		return nil
	case err != nil:
		return errors.Wrap(err, "auto-stop after working hours")
	}
	return nil
}

func (s *server) started() {
	if s.opts.OnStarted != nil {
		s.opts.OnStarted()
	}
}

// Clock tells the client the server's time, so it reads relative dates right.
type Clock struct {
	Now      string `json:"now"      jsonschema:"current local time, RFC 3339"`
	Timezone string `json:"timezone" jsonschema:"local time zone"`
}

func (s *server) clock() Clock {
	return Clock{Now: s.opts.Now().Format(time.RFC3339), Timezone: s.zone}
}

// zoneName returns the IANA name of the local zone where it can be found, else its abbreviation.
func zoneName(now time.Time) string {
	if name := now.Location().String(); name != "" && name != "Local" {
		return name
	}
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		return tz
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(link, "zoneinfo/"); ok {
			return name
		}
	}
	name, _ := now.Zone()
	return name
}
