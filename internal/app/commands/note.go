package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type noteOptions struct {
	JSONOutput bool
}

func NewNoteCmd() *cobra.Command {
	var opts noteOptions

	cmd := &cobra.Command{
		Use:     "note [DATE-INDEX] NOTE",
		Aliases: []string{"annotate"},
		Short:   defaultText("note.short"),
		Long:    defaultText("note.long"),
		Args:    cobra.RangeArgs(1, 2),
		RunE:    func(cmd *cobra.Command, args []string) error { return runNoteCmd(cmd, args, &opts) },
	}

	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("note.flag.json"))
	return cmd
}

func runNoteCmd(cmd *cobra.Command, args []string, opts *noteOptions) error {
	activityKey, noteText, err := parseNoteArgs(args)
	if err != nil {
		return errors.Wrap(err, "parse arguments")
	}

	ctx := cmd.Context()

	rt := getRuntime(cmd)
	activity, err := resolveNoteActivity(ctx, rt.ActivityService, activityKey)
	if err != nil {
		return errors.Wrap(err, "resolve activity")
	}

	updated, err := appendNoteToActivity(ctx, rt.NotesRepository, activity, noteText)
	if err != nil {
		return errors.Wrap(err, "append note to activity")
	}

	out := cmd.OutOrStdout()
	if opts.JSONOutput {
		return writeJSONTo(out, updated)
	}

	fmt.Fprintln(out, text(cmd, "note.done"))
	return nil
}

func parseNoteArgs(args []string) (string, string, error) {
	switch len(args) {
	case 1:
		value := strings.TrimSpace(args[0])
		if value == "" {
			return "", "", errors.New(defaultText("note.error.empty"))
		}
		if _, _, err := models.ParseActivityKey(value); err == nil {
			return "", "", errors.New(defaultText("note.error.note_required"))
		}
		return "", value, nil
	case 2:
		if _, _, err := models.ParseActivityKey(strings.TrimSpace(args[0])); err != nil {
			return "", "", errors.Wrap(err, "parse index")
		}

		noteText := strings.TrimSpace(args[1])
		if noteText == "" {
			return "", "", errors.New(defaultText("note.error.empty"))
		}
		return strings.TrimSpace(args[0]), noteText, nil
	default:
		return "", "", errors.New(defaultText("note.error.note_required"))
	}
}

func resolveNoteActivity(ctx context.Context, svc ports.ActivityResolver, activityKey string) (models.Activity, error) {
	if activityKey == "" {
		return findLastActivity(ctx, svc)
	}
	return findActivityByIndex(ctx, svc, activityKey)
}

func appendNoteToActivity(
	ctx context.Context,
	notesRepo ports.NotesRepository,
	activity models.Activity,
	noteText string,
) (models.Activity, error) {
	if notesRepo == nil {
		return models.Activity{}, errors.New(defaultText("note.error.unavailable"))
	}

	existingNotes := strings.TrimSpace(activity.Notes)
	existingTags := append([]string(nil), activity.Tags...)

	storedNotes, storedTags, err := notesRepo.Get(ctx, activity.ID(), activity.StartTime)
	if err != nil {
		return models.Activity{}, errors.Wrap(err, "get notes")
	}

	if trimmed := strings.TrimSpace(storedNotes); trimmed != "" {
		existingNotes = trimmed
	}
	if len(storedTags) > 0 {
		existingTags = append([]string(nil), storedTags...)
	}

	updated := activity
	updated.Notes = joinNotes(existingNotes, noteText)
	updated.Tags = existingTags

	if err = notesRepo.Save(ctx, updated.ID(), updated.StartTime, updated.Notes, updated.Tags); err != nil {
		return models.Activity{}, errors.Wrap(err, "save note")
	}

	return updated, nil
}

func joinNotes(existingNotes, noteText string) string {
	existingNotes = strings.TrimSpace(existingNotes)
	noteText = strings.TrimSpace(noteText)

	if existingNotes == "" {
		return noteText
	}
	if noteText == "" {
		return existingNotes
	}
	return existingNotes + "\n\n" + noteText
}
