package models

import (
	"sort"
	"time"
)

func UniqueProjects(activities []Activity) []string {
	seen := make(map[string]struct{})
	projects := make([]string, 0, len(activities))

	for _, activity := range activities {
		if activity.Project == "" {
			continue
		}
		if _, exists := seen[activity.Project]; exists {
			continue
		}
		seen[activity.Project] = struct{}{}
		projects = append(projects, activity.Project)
	}

	sort.Strings(projects)
	return projects
}

func DescriptionsForProject(activities []Activity, project string) []string {
	seen := make(map[string]struct{})
	descriptions := make([]string, 0, len(activities))

	for _, activity := range activities {
		if activity.Project != project || activity.Description == "" {
			continue
		}
		if _, exists := seen[activity.Description]; exists {
			continue
		}
		seen[activity.Description] = struct{}{}
		descriptions = append(descriptions, activity.Description)
	}

	sort.Strings(descriptions)
	return descriptions
}

func FindTargetDate(activities []Activity, current time.Time, dir int) *time.Time {
	var target *time.Time

	for _, activity := range activities {
		date := time.Date(
			activity.StartTime.Year(), activity.StartTime.Month(), activity.StartTime.Day(),
			0, 0, 0, 0, activity.StartTime.Location(),
		)

		if dir < 0 {
			if !date.Before(current) {
				continue
			}
			if target == nil || date.After(*target) {
				candidate := date
				target = &candidate
			}
			continue
		}

		if !date.After(current) {
			continue
		}
		if target == nil || date.Before(*target) {
			candidate := date
			target = &candidate
		}
	}

	return target
}
