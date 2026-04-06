package export

import (
	"fmt"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
)

func CombinedCalendar(activities []models.Activity) string {
	sorted := models.SortActivitiesByStart(activities)
	ids := models.ActivitySequenceIDs(sorted)

	var sb strings.Builder
	for _, activity := range sorted {
		sb.WriteString(GenerateEvent(activity, ids[activity.StartTime.UnixNano()]))
	}

	return WrapCalendar(sb.String())
}

func ResolveExportFileName(configured string) string {
	fileName := configured
	if fileName == "" {
		fileName = "tock_export.ics"
	}
	if !strings.HasSuffix(fileName, ".ics") {
		fileName = fmt.Sprintf("%s.ics", fileName)
	}
	return fileName
}

func Generate(act models.Activity, uidKey string) string {
	event := GenerateEvent(act, uidKey)
	return WrapCalendar(event)
}

func WrapCalendar(eventsBody string) string {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\n")
	sb.WriteString("VERSION:2.0\n")
	sb.WriteString("PRODID:-//Tock//NONSGML v1.0//EN\n")
	sb.WriteString(eventsBody)
	sb.WriteString("END:VCALENDAR")
	return sb.String()
}

func GenerateEvent(act models.Activity, uidKey string) string {
	now := time.Now().UTC().Format("20060102T150405Z")
	start := act.StartTime.UTC().Format("20060102T150405Z")

	var end string
	if act.EndTime != nil {
		end = act.EndTime.UTC().Format("20060102T150405Z")
	} else {
		end = time.Now().UTC().Format("20060102T150405Z")
	}

	summary := fmt.Sprintf("%s: %s", act.Project, act.Description)
	uid := fmt.Sprintf("%s@tock", uidKey)

	var sb strings.Builder
	sb.WriteString("BEGIN:VEVENT\n")
	fmt.Fprintf(&sb, "UID:%s\n", uid)
	fmt.Fprintf(&sb, "DTSTAMP:%s\n", now)
	fmt.Fprintf(&sb, "DTSTART:%s\n", start)
	fmt.Fprintf(&sb, "DTEND:%s\n", end)
	fmt.Fprintf(&sb, "SUMMARY:%s\n", escapeProperty(summary))

	description := act.Description
	if act.Notes != "" {
		description += "\n\n" + act.Notes
	}

	fmt.Fprintf(&sb, "DESCRIPTION:%s\n", escapeProperty(description))

	if len(act.Tags) > 0 {
		escapedTags := make([]string, len(act.Tags))
		for i, tag := range act.Tags {
			escapedTags[i] = escapeProperty(tag)
		}

		fmt.Fprintf(&sb, "CATEGORIES:%s\n", strings.Join(escapedTags, ","))
	}

	sb.WriteString("END:VEVENT\n")
	return sb.String()
}

func escapeProperty(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
