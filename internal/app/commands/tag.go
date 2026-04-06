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

type tagOptions struct {
	JSONOutput bool
}

func NewTagCmd() *cobra.Command {
	var opts tagOptions

	cmd := &cobra.Command{
		Use:     "tag [DATE-INDEX] TAG [TAG...]",
		Aliases: []string{"tags"},
		Short:   defaultText("tag.short"),
		Long:    defaultText("tag.long"),
		Args:    cobra.MinimumNArgs(1),
		RunE:    func(cmd *cobra.Command, args []string) error { return runTagCmd(cmd, args, &opts) },
	}

	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("tag.flag.json"))
	return cmd
}

func runTagCmd(cmd *cobra.Command, args []string, opts *tagOptions) error {
	activityKey, tags, err := parseTagArgs(args)
	if err != nil {
		return errors.Wrap(err, "parse arguments")
	}

	ctx := cmd.Context()
	rt := getRuntime(cmd)
	activity, err := resolveNoteActivity(ctx, rt.ActivityService, activityKey)
	if err != nil {
		return errors.Wrap(err, "resolve activity")
	}

	updated, err := appendTagsToActivity(ctx, rt.NotesRepository, activity, tags)
	if err != nil {
		return errors.Wrap(err, "append tags to activity")
	}

	out := cmd.OutOrStdout()
	if opts.JSONOutput {
		return writeJSONTo(out, updated)
	}

	fmt.Fprintln(out, text(cmd, "tag.done"))
	return nil
}

func parseTagArgs(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New(defaultText("tag.error.required"))
	}

	first := strings.TrimSpace(args[0])
	if first == "" {
		return "", nil, errors.New(defaultText("tag.error.required"))
	}

	if _, _, err := models.ParseActivityKey(first); err == nil {
		tags := parseTagValues(args[1:])
		if len(tags) == 0 {
			return "", nil, errors.New(defaultText("tag.error.required"))
		}
		return first, tags, nil
	}

	tags := parseTagValues(args)
	if len(tags) == 0 {
		return "", nil, errors.New(defaultText("tag.error.required"))
	}
	return "", tags, nil
}

func parseTagValues(values []string) []string {
	seen := make(map[string]struct{})
	tags := make([]string, 0, len(values))

	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			tag := strings.TrimSpace(part)
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
	}

	return tags
}

func appendTagsToActivity(
	ctx context.Context,
	notesRepo ports.NotesRepository,
	activity models.Activity,
	newTags []string,
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
	updated.Notes = existingNotes
	updated.Tags = mergeTags(existingTags, newTags)

	if err = notesRepo.Save(ctx, updated.ID(), updated.StartTime, updated.Notes, updated.Tags); err != nil {
		return models.Activity{}, errors.Wrap(err, "save tags")
	}

	return updated, nil
}

func mergeTags(existingTags, newTags []string) []string {
	seen := make(map[string]struct{}, len(existingTags)+len(newTags))
	merged := make([]string, 0, len(existingTags)+len(newTags))

	for _, tag := range append(append([]string(nil), existingTags...), newTags...) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}

	return merged
}
