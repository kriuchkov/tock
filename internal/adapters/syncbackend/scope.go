package syncbackend

import (
	"slices"

	"github.com/kriuchkov/tock/internal/core/ports"
)

// everything is the scope of a compare that reads the whole backend.
func everything() ports.SyncScope {
	return ports.SyncScope{AllActivities: true, Reminders: true}
}

// differingParts returns the parts of two tokens whose values differ, a part only one of them holds
// included, sorted.
func differingParts(since, now ports.SyncToken) []string {
	var parts []string
	for key, value := range since {
		if other, ok := now[key]; !ok || other != value {
			parts = append(parts, key)
		}
	}
	for key := range now {
		if _, ok := since[key]; !ok {
			parts = append(parts, key)
		}
	}
	slices.Sort(parts)
	return parts
}

// withSpans sets the activity spans of scope, merged, unless it already covers every activity.
func withSpans(scope ports.SyncScope, spans []ports.SyncSpan) ports.SyncScope {
	if scope.AllActivities {
		scope.Activities = nil
		return scope
	}
	scope.Activities = mergeSpans(spans)
	return scope
}

// mergeSpans sorts spans by start and joins the ones that overlap or touch.
func mergeSpans(spans []ports.SyncSpan) []ports.SyncSpan {
	if len(spans) == 0 {
		return nil
	}
	sorted := slices.Clone(spans)
	slices.SortFunc(sorted, func(a, b ports.SyncSpan) int {
		return a.From.Compare(b.From)
	})

	merged := sorted[:1]
	for _, span := range sorted[1:] {
		last := &merged[len(merged)-1]
		if span.From.After(last.To) {
			merged = append(merged, span)
			continue
		}
		if span.To.After(last.To) {
			last.To = span.To
		}
	}
	return merged
}
