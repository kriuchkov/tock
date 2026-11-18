package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

// StartInput starts an activity.
type StartInput struct {
	Project     string   `json:"project"`
	Description string   `json:"description"`
	At          string   `json:"at,omitempty"    jsonschema:"start time; now by default"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ContinueInput starts a recent activity again.
type ContinueInput struct {
	Index       int      `json:"index,omitempty"       jsonschema:"index from recent; 0, the latest, by default"`
	Project     string   `json:"project,omitempty"     jsonschema:"replaces the project"`
	Description string   `json:"description,omitempty" jsonschema:"replaces the description"`
	At          string   `json:"at,omitempty"          jsonschema:"start time; now by default"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// StartOutput is the started activity and the one it stopped.
type StartOutput struct {
	Clock

	Started Activity  `json:"started"`
	Stopped *Activity `json:"stopped,omitempty" jsonschema:"the activity that was running and got stopped"`
}

// StopInput stops the running activity.
type StopInput struct {
	At    string   `json:"at,omitempty"    jsonschema:"end time; now by default"`
	Notes string   `json:"notes,omitempty" jsonschema:"notes to add"`
	Tags  []string `json:"tags,omitempty"  jsonschema:"tags to add"`
}

// AddInput records a finished activity.
type AddInput struct {
	Project     string   `json:"project"`
	Description string   `json:"description"`
	Start       string   `json:"start"`
	End         string   `json:"end"`
	Notes       string   `json:"notes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// EditInput changes an activity; absent fields keep their value.
type EditInput struct {
	Key         string   `json:"key"                   jsonschema:"DATE-INDEX key (YYYY-MM-DD-NN) from list"`
	Project     *string  `json:"project,omitempty"`
	Description *string  `json:"description,omitempty"`
	Start       *string  `json:"start,omitempty"`
	End         *string  `json:"end,omitempty"         jsonschema:"end time; an empty string makes the activity run again"`
	Notes       *string  `json:"notes,omitempty"       jsonschema:"replaces the notes"`
	Tags        []string `json:"tags,omitempty"        jsonschema:"replaces the tags"`
}

// NoteInput appends a note.
type NoteInput struct {
	Key  string `json:"key,omitempty" jsonschema:"DATE-INDEX key from list; the latest activity by default"`
	Note string `json:"note"`
}

// TagInput adds tags.
type TagInput struct {
	Key  string   `json:"key,omitempty" jsonschema:"DATE-INDEX key from list; the latest activity by default"`
	Tags []string `json:"tags"`
}

// KeyInput names one activity.
type KeyInput struct {
	Key string `json:"key" jsonschema:"DATE-INDEX key (YYYY-MM-DD-NN) from list"`
}

// ActivityOutput is one activity after a change.
type ActivityOutput struct {
	Clock

	Activity Activity `json:"activity"`
}

func (s *server) addActivityWriteTools() {
	addTool(s, writeAccess, &mcp.Tool{
		Name:        "start",
		Title:       "Start activity",
		Description: "Start tracking an activity. The running activity, if any, is stopped at the new start.",
	}, s.start)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "stop",
		Title:       "Stop activity",
		Description: "Stop the running activity.",
	}, s.stop)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "continue",
		Title:       "Continue activity",
		Description: "Start one of the recent activities again, optionally with another project or description.",
	}, s.continueActivity)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "add",
		Title:       "Add activity",
		Description: "Record a finished activity with its start and end.",
	}, s.add)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "edit",
		Title:       "Edit activity",
		Description: "Change the project, description, times, notes or tags of an activity.",
	}, s.edit)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "note",
		Title:       "Add note",
		Description: "Append a note to an activity.",
	}, s.note)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "tag",
		Title:       "Add tags",
		Description: "Add tags to an activity.",
	}, s.tag)

	addTool(s, destructiveAccess, &mcp.Tool{
		Name:        "remove",
		Title:       "Remove activity",
		Description: "Delete an activity with its notes and tags. This cannot be undone.",
	}, s.remove)
}

func requireText(project, description string) error {
	if strings.TrimSpace(project) == "" {
		return coreErrors.ErrProjectRequired
	}
	if strings.TrimSpace(description) == "" {
		return coreErrors.ErrDescriptionRequired
	}
	return nil
}

func (s *server) start(ctx context.Context, _ *mcp.CallToolRequest, in StartInput) (*mcp.CallToolResult, StartOutput, error) {
	out := StartOutput{Clock: s.clock()}
	if err := requireText(in.Project, in.Description); err != nil {
		return nil, out, err
	}
	now := s.opts.Now()
	at, err := parseTime(in.At, now, now)
	if err != nil {
		return nil, out, err
	}

	return s.startWith(ctx, out, func() (*models.Activity, error) {
		return s.opts.Activities.Start(ctx, models.StartActivityRequest{
			Project:     strings.TrimSpace(in.Project),
			Description: strings.TrimSpace(in.Description),
			StartTime:   at,
			Notes:       in.Notes,
			Tags:        cleanTags(in.Tags),
		})
	})
}

func (s *server) continueActivity(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ContinueInput,
) (*mcp.CallToolResult, StartOutput, error) {
	out := StartOutput{Clock: s.clock()}
	now := s.opts.Now()
	at, err := parseTime(in.At, now, now)
	if err != nil {
		return nil, out, err
	}

	return s.startWith(ctx, out, func() (*models.Activity, error) {
		return s.opts.Activities.Continue(ctx, models.ContinueActivityRequest{
			Index:       in.Index,
			Project:     strings.TrimSpace(in.Project),
			Description: strings.TrimSpace(in.Description),
			StartTime:   at,
			Notes:       in.Notes,
			Tags:        cleanTags(in.Tags),
		})
	})
}

// startWith runs start and reports the activity it stopped along with the started one.
func (s *server) startWith(
	ctx context.Context,
	out StartOutput,
	start func() (*models.Activity, error),
) (*mcp.CallToolResult, StartOutput, error) {
	running, err := s.running(ctx)
	if err != nil {
		return nil, out, err
	}

	started, err := start()
	if err != nil {
		return nil, out, errors.Wrap(err, "start activity")
	}
	s.started()

	out.Started = *s.keyed(ctx, started)
	if running != nil {
		stopped := *running
		if end := started.StartTime; !end.Before(stopped.StartTime) {
			stopped.EndTime = &end
		}
		out.Stopped = s.keyed(ctx, &stopped)
	}
	return nil, out, nil
}

func (s *server) stop(ctx context.Context, _ *mcp.CallToolRequest, in StopInput) (*mcp.CallToolResult, ActivityOutput, error) {
	out := ActivityOutput{Clock: s.clock()}
	now := s.opts.Now()
	at, err := parseTime(in.At, now, now)
	if err != nil {
		return nil, out, err
	}

	stopped, err := s.opts.Activities.Stop(ctx, models.StopActivityRequest{EndTime: at, Notes: in.Notes, Tags: cleanTags(in.Tags)})
	if err != nil {
		return nil, out, errors.Wrap(err, "stop activity")
	}
	out.Activity = *s.keyed(ctx, stopped)
	return nil, out, nil
}

func (s *server) add(ctx context.Context, _ *mcp.CallToolRequest, in AddInput) (*mcp.CallToolResult, ActivityOutput, error) {
	out := ActivityOutput{Clock: s.clock()}
	if err := requireText(in.Project, in.Description); err != nil {
		return nil, out, err
	}
	now := s.opts.Now()
	start, err := parseTime(in.Start, now, time.Time{})
	if err != nil {
		return nil, out, err
	}
	end, err := parseTime(in.End, now, time.Time{})
	if err != nil {
		return nil, out, err
	}
	if start.IsZero() || end.IsZero() {
		return nil, out, errors.New("start and end are required")
	}

	added, err := s.opts.Activities.Add(ctx, models.AddActivityRequest{
		Project:     strings.TrimSpace(in.Project),
		Description: strings.TrimSpace(in.Description),
		StartTime:   start,
		EndTime:     end,
		Notes:       in.Notes,
		Tags:        cleanTags(in.Tags),
	})
	if err != nil {
		return nil, out, errors.Wrap(err, "add activity")
	}
	out.Activity = *s.keyed(ctx, added)
	return nil, out, nil
}

func (s *server) edit(ctx context.Context, _ *mcp.CallToolRequest, in EditInput) (*mcp.CallToolResult, ActivityOutput, error) {
	out := ActivityOutput{Clock: s.clock()}
	original, err := s.find(ctx, in.Key)
	if err != nil {
		return nil, out, err
	}

	req, err := s.updateRequest(in)
	if err != nil {
		return nil, out, err
	}
	if req.IsEmpty() {
		return nil, out, errors.New("nothing to change")
	}

	updated, err := s.opts.Activities.Update(ctx, *original, req)
	if err != nil {
		return nil, out, errors.Wrap(err, "update activity")
	}
	out.Activity = *s.keyed(ctx, updated)
	return nil, out, nil
}

func (s *server) updateRequest(in EditInput) (models.UpdateActivityRequest, error) {
	req := models.UpdateActivityRequest{Project: in.Project, Description: in.Description, Notes: in.Notes}
	if in.Tags != nil {
		tags := cleanTags(in.Tags)
		req.Tags = &tags
	}

	now := s.opts.Now()
	if in.Start != nil {
		start, err := parseTime(*in.Start, now, time.Time{})
		if err != nil || start.IsZero() {
			return req, errors.Errorf("invalid start %q", *in.Start)
		}
		req.StartTime = &start
	}
	if in.End != nil {
		if strings.TrimSpace(*in.End) == "" {
			req.ClearEndTime = true
			return req, nil
		}
		end, err := parseTime(*in.End, now, time.Time{})
		if err != nil {
			return req, err
		}
		req.EndTime = &end
	}
	return req, nil
}

func (s *server) note(ctx context.Context, _ *mcp.CallToolRequest, in NoteInput) (*mcp.CallToolResult, ActivityOutput, error) {
	out := ActivityOutput{Clock: s.clock()}
	if strings.TrimSpace(in.Note) == "" {
		return nil, out, errors.New("note is empty")
	}
	target, err := s.target(ctx, in.Key)
	if err != nil {
		return nil, out, err
	}

	updated, err := s.opts.Activities.AddNote(ctx, *target, strings.TrimSpace(in.Note))
	if err != nil {
		return nil, out, errors.Wrap(err, "add note")
	}
	out.Activity = *s.keyed(ctx, updated)
	return nil, out, nil
}

func (s *server) tag(ctx context.Context, _ *mcp.CallToolRequest, in TagInput) (*mcp.CallToolResult, ActivityOutput, error) {
	out := ActivityOutput{Clock: s.clock()}
	tags := cleanTags(in.Tags)
	if len(tags) == 0 {
		return nil, out, errors.New("no tags given")
	}
	target, err := s.target(ctx, in.Key)
	if err != nil {
		return nil, out, err
	}

	updated, err := s.opts.Activities.AddTags(ctx, *target, tags)
	if err != nil {
		return nil, out, errors.Wrap(err, "add tags")
	}
	out.Activity = *s.keyed(ctx, updated)
	return nil, out, nil
}

func (s *server) remove(ctx context.Context, _ *mcp.CallToolRequest, in KeyInput) (*mcp.CallToolResult, ActivityOutput, error) {
	out := ActivityOutput{Clock: s.clock()}
	activity, err := s.find(ctx, in.Key)
	if err != nil {
		return nil, out, err
	}
	removed := s.activity(*activity, strings.TrimSpace(in.Key))

	if err = s.opts.Activities.Remove(ctx, *activity); err != nil {
		return nil, out, errors.Wrap(err, "remove activity")
	}
	out.Activity = removed
	return nil, out, nil
}

func (s *server) find(ctx context.Context, key string) (*models.Activity, error) {
	activity, err := s.opts.Activities.GetByKey(ctx, strings.TrimSpace(key))
	if err != nil {
		return nil, errors.Wrap(err, "find activity")
	}
	return activity, nil
}

// target is the activity named by key, else the latest one.
func (s *server) target(ctx context.Context, key string) (*models.Activity, error) {
	if strings.TrimSpace(key) != "" {
		return s.find(ctx, key)
	}
	last, err := s.opts.Activities.GetLast(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "find latest activity")
	}
	return last, nil
}
