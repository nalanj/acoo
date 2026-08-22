package scheduler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule represents a parsed schedule
type Schedule struct {
	Spec     string
	Type     ScheduleType
	Interval time.Duration
	Cron     cron.Schedule
	Next     time.Time
	LastRun  time.Time // Last time this job was run
}

// ScheduleType indicates the type of schedule
type ScheduleType int

const (
	ScheduleTypeCron     ScheduleType = iota
	ScheduleTypeInterval
	ScheduleTypeOnce
)

// EveryRegex matches @every duration (including seconds)
var EveryRegex = regexp.MustCompile(`^@every\s+(\d+(?:ms|[smhd])+)`)

// Parse parses a schedule string into a Schedule
func Parse(spec string) (*Schedule, error) {
	return ParseWithLastRun(spec, time.Time{})
}

// ParseWithLastRun parses a schedule string with a known last run time
func ParseWithLastRun(spec string, lastRun time.Time) (*Schedule, error) {
	spec = strings.TrimSpace(spec)

	switch {
	case spec == "@once":
		return &Schedule{
			Spec:    spec,
			Type:    ScheduleTypeOnce,
			Next:    time.Now(),
			LastRun: lastRun,
		}, nil

	case strings.HasPrefix(spec, "@every "):
		sched, err := parseInterval(spec)
		if err != nil {
			return nil, err
		}
		sched.LastRun = lastRun
		return sched, nil

	default:
		sched, err := parseCron(spec)
		if err != nil {
			return nil, err
		}
		sched.LastRun = lastRun
		return sched, nil
	}
}

// parseInterval parses @every duration formats (supports seconds)
func parseInterval(spec string) (*Schedule, error) {
	matches := EveryRegex.FindStringSubmatch(spec)
	if matches == nil {
		return nil, fmt.Errorf("invalid interval format: %s", spec)
	}

	duration, err := time.ParseDuration(matches[1])
	if err != nil {
		return nil, fmt.Errorf("invalid duration: %w", err)
	}

	if duration < time.Millisecond {
		return nil, fmt.Errorf("duration must be at least 1ms")
	}

	return &Schedule{
		Spec:     spec,
		Type:     ScheduleTypeInterval,
		Interval: duration,
		Next:     time.Now().Add(duration),
	}, nil
}

// parseCron parses standard cron expressions
func parseCron(spec string) (*Schedule, error) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	parsed, err := parser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	return &Schedule{
		Spec: spec,
		Type: ScheduleTypeCron,
		Cron: parsed,
		Next: parsed.Next(time.Now()),
	}, nil
}

// NextRun returns the time of the next execution and advances the schedule
func (s *Schedule) NextRun() time.Time {
	now := time.Now()

	switch s.Type {
	case ScheduleTypeCron:
		s.Next = s.Cron.Next(now)
	case ScheduleTypeInterval:
		if s.LastRun.IsZero() {
			// Never run - run immediately
			s.Next = now
		} else {
			// Resume from last run + interval
			s.Next = s.LastRun.Add(s.Interval)
			if s.Next.Before(now) {
				// We've fallen behind, run immediately
				s.Next = now
			}
		}
	case ScheduleTypeOnce:
		// One-shot schedules don't advance
	}

	return s.Next
}

// SleepUntil returns the duration to wait until next run
func (s *Schedule) SleepUntil() time.Duration {
	now := time.Now()
	wait := s.Next.Sub(now)
	if wait < 0 {
		return 0
	}
	return wait
}

// IsOneShot returns true for @once schedules
func (s *Schedule) IsOneShot() bool {
	return s.Type == ScheduleTypeOnce
}

// FormatInterval returns a human-readable interval
func FormatInterval(d time.Duration) string {
	if d < time.Minute {
		seconds := int(d.Seconds())
		if seconds == 1 {
			return "1 second"
		}
		return strconv.Itoa(seconds) + " seconds"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "1 minute"
		}
		return strconv.Itoa(minutes) + " minutes"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if minutes == 0 {
		if hours == 1 {
			return "1 hour"
		}
		return strconv.Itoa(hours) + " hours"
	}

	return fmt.Sprintf("%dh %dm", hours, minutes)
}
