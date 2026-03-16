package todotxt

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
)

var ErrSkip = errors.New("skip line")

const (
	dateLayout           = "2006-01-02"
	extStart             = "tock_start"
	extEnd               = "tock_end"
	extProject           = "tock_project"
	extDescription       = "tock_desc"
	extTags              = "tock_tags"
	tagsSeparator        = "\x1f"
	defaultCompletedHour = 23
	defaultCompletedMin  = 59
	defaultCompletedSec  = 59
)

type parsedPrefix struct {
	index          int
	isCompleted    bool
	completionDate *time.Time
	creationDate   *time.Time
}

type parsedTokens struct {
	metadata         map[string]string
	projects         []string
	contexts         []string
	descriptionParts []string
}

func ParseActivity(line string) (*models.Activity, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return nil, ErrSkip
	}

	prefix := parsePrefix(fields)
	if prefix.index >= len(fields) {
		return nil, ErrSkip
	}

	tokens := parseTokens(fields[prefix.index:])

	startTime, err := resolveStartTime(tokens.metadata, prefix.creationDate, prefix.completionDate)
	if err != nil {
		return nil, err
	}

	endTime, err := resolveEndTime(tokens.metadata, prefix.isCompleted, startTime, prefix.completionDate)
	if err != nil {
		return nil, err
	}

	description, err := resolveDescription(tokens)
	if err != nil {
		return nil, err
	}

	project, err := resolveProject(tokens)
	if err != nil {
		return nil, err
	}

	tags, err := resolveTags(tokens)
	if err != nil {
		return nil, err
	}

	return &models.Activity{
		Description: description,
		Project:     project,
		StartTime:   startTime,
		EndTime:     endTime,
		Tags:        tags,
	}, nil
}

func parsePrefix(fields []string) parsedPrefix {
	prefix := parsedPrefix{}
	if len(fields) == 0 {
		return prefix
	}

	if fields[0] == "x" {
		return parseCompletedPrefix(fields)
	}

	if isPriorityToken(fields[0]) {
		prefix.index++
	}
	prefix.creationDate, prefix.index = parseOptionalDate(fields, prefix.index)
	return prefix
}

func parseCompletedPrefix(fields []string) parsedPrefix {
	prefix := parsedPrefix{isCompleted: true, index: 1}
	prefix.completionDate, prefix.index = parseOptionalDate(fields, prefix.index)
	prefix.creationDate, prefix.index = parseOptionalDate(fields, prefix.index)
	return prefix
}

func parseOptionalDate(fields []string, index int) (*time.Time, int) {
	if index >= len(fields) {
		return nil, index
	}
	parsed, ok := parseDateToken(fields[index])
	if !ok {
		return nil, index
	}
	return &parsed, index + 1
}

func parseTokens(fields []string) parsedTokens {
	tokens := parsedTokens{
		metadata:         make(map[string]string),
		projects:         make([]string, 0),
		contexts:         make([]string, 0),
		descriptionParts: make([]string, 0, len(fields)),
	}

	for _, token := range fields {
		switch {
		case isExtensionToken(token):
			key, value, _ := strings.Cut(token, ":")
			tokens.metadata[key] = value
		case strings.HasPrefix(token, "+") && len(token) > 1:
			tokens.projects = append(tokens.projects, token[1:])
		case strings.HasPrefix(token, "@") && len(token) > 1:
			tokens.contexts = append(tokens.contexts, token[1:])
		default:
			tokens.descriptionParts = append(tokens.descriptionParts, token)
		}
	}

	return tokens
}

func resolveDescription(tokens parsedTokens) (string, error) {
	description := strings.Join(tokens.descriptionParts, " ")
	encodedDescription, ok := tokens.metadata[extDescription]
	if !ok {
		return description, nil
	}

	decoded, err := decodeValue(encodedDescription)
	if err != nil {
		return "", errors.Wrap(err, "decode description")
	}
	return decoded, nil
}

func resolveProject(tokens parsedTokens) (string, error) {
	encodedProject, ok := tokens.metadata[extProject]
	if ok {
		decoded, err := decodeValue(encodedProject)
		if err != nil {
			return "", errors.Wrap(err, "decode project")
		}
		return decoded, nil
	}
	if len(tokens.projects) > 0 {
		return tokens.projects[0], nil
	}
	return "", nil
}

func resolveTags(tokens parsedTokens) ([]string, error) {
	encodedTags, ok := tokens.metadata[extTags]
	if !ok {
		return append([]string(nil), tokens.contexts...), nil
	}

	decoded, err := decodeTags(encodedTags)
	if err != nil {
		return nil, errors.Wrap(err, "decode tags")
	}
	return decoded, nil
}

func FormatActivity(activity models.Activity) string {
	parts := make([]string, 0, 8+len(activity.Tags))
	if activity.EndTime != nil {
		parts = append(parts, "x", activity.EndTime.Format(dateLayout), activity.StartTime.Format(dateLayout))
	} else {
		parts = append(parts, activity.StartTime.Format(dateLayout))
	}

	if activity.Description != "" {
		parts = append(parts, strings.Fields(activity.Description)...)
	}

	if isTokenSafe(activity.Project) {
		parts = append(parts, "+"+activity.Project)
	}

	for _, tag := range uniqueSorted(activity.Tags) {
		if isTokenSafe(tag) {
			parts = append(parts, "@"+tag)
		}
	}

	parts = append(parts, extStart+":"+activity.StartTime.Format(time.RFC3339))
	if activity.EndTime != nil {
		parts = append(parts, extEnd+":"+activity.EndTime.Format(time.RFC3339))
	}
	if activity.Project != "" {
		parts = append(parts, extProject+":"+encodeValue(activity.Project))
	}
	parts = append(parts, extDescription+":"+encodeValue(activity.Description))
	if len(activity.Tags) > 0 {
		parts = append(parts, extTags+":"+encodeTags(activity.Tags))
	}

	return strings.Join(parts, " ")
}

func resolveStartTime(metadata map[string]string, creationDate, completionDate *time.Time) (time.Time, error) {
	if encodedStart, ok := metadata[extStart]; ok {
		parsed, err := parseTimestamp(encodedStart)
		if err != nil {
			return time.Time{}, errors.Wrap(err, "parse start time")
		}
		return parsed, nil
	}
	if creationDate != nil {
		return *creationDate, nil
	}
	if completionDate != nil {
		return *completionDate, nil
	}
	return time.Time{}, ErrSkip
}

func resolveEndTime(
	metadata map[string]string,
	isCompleted bool,
	startTime time.Time,
	completionDate *time.Time,
) (*time.Time, error) {
	if encodedEnd, ok := metadata[extEnd]; ok {
		parsed, err := parseTimestamp(encodedEnd)
		if err != nil {
			return nil, errors.Wrap(err, "parse end time")
		}
		return &parsed, nil
	}
	if !isCompleted {
		var endTime *time.Time
		return endTime, nil
	}
	if completionDate == nil {
		completed := startTime
		return &completed, nil
	}
	completed := time.Date(
		completionDate.Year(),
		completionDate.Month(),
		completionDate.Day(),
		defaultCompletedHour,
		defaultCompletedMin,
		defaultCompletedSec,
		0,
		completionDate.Location(),
	)
	if completed.Before(startTime) {
		completed = startTime
	}
	return &completed, nil
}

func parseDateToken(token string) (time.Time, bool) {
	parsed, err := time.ParseInLocation(dateLayout, token, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func parseTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.Local(), nil
	}
	return time.ParseInLocation(dateLayout, value, time.Local)
}

func isPriorityToken(token string) bool {
	return len(token) == 3 && token[0] == '(' && token[2] == ')' && token[1] >= 'A' && token[1] <= 'Z'
}

func isExtensionToken(token string) bool {
	key, _, found := strings.Cut(token, ":")
	if !found {
		return false
	}
	switch key {
	case extStart, extEnd, extProject, extDescription, extTags:
		return true
	default:
		return false
	}
}

func isTokenSafe(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\n\r")
}

func encodeValue(value string) string {
	return url.QueryEscape(value)
}

func decodeValue(value string) (string, error) {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return "", err
	}
	return decoded, nil
}

func encodeTags(tags []string) string {
	return encodeValue(strings.Join(uniqueSorted(tags), tagsSeparator))
}

func decodeTags(encoded string) ([]string, error) {
	decoded, err := decodeValue(encoded)
	if err != nil {
		return nil, err
	}
	if decoded == "" {
		return []string{}, nil
	}
	return strings.Split(decoded, tagsSeparator), nil
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}
