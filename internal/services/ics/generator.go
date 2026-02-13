package ics

import (
	"fmt"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
)

// Generate returns a full iCalendar string for a single activity.
func Generate(act models.Activity, uidKey string) string {
	event := GenerateEvent(act, uidKey)
	return WrapCalendar(event)
}

// WrapCalendar wraps the given event(s) string in a VCALENDAR block.
func WrapCalendar(eventsBody string) string {
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\n")
	sb.WriteString("VERSION:2.0\n")
	sb.WriteString("PRODID:-//Tock//NONSGML v1.0//EN\n")
	sb.WriteString(eventsBody)
	sb.WriteString("END:VCALENDAR")
	return sb.String()
}

// GenerateEvent returns the VEVENT block for a single activity.
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
	sb.WriteString(fmt.Sprintf("UID:%s\n", uid))
	sb.WriteString(fmt.Sprintf("DTSTAMP:%s\n", now))
	sb.WriteString(fmt.Sprintf("DTSTART:%s\n", start))
	sb.WriteString(fmt.Sprintf("DTEND:%s\n", end))
	sb.WriteString(fmt.Sprintf("SUMMARY:%s\n", escapeProperty(summary)))

	description := act.Description
	if act.Notes != "" {
		description += "\n\n" + act.Notes
	}

	sb.WriteString(fmt.Sprintf("DESCRIPTION:%s\n", escapeProperty(description)))

	if len(act.Tags) > 0 {
		escapedTags := make([]string, len(act.Tags))
		for i, tag := range act.Tags {
			escapedTags[i] = escapeProperty(tag)
		}

		sb.WriteString(fmt.Sprintf("CATEGORIES:%s\n", strings.Join(escapedTags, ",")))
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
