package scheduler

import (
	"testing"
	"time"
)

func TestParseInterval(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSec int
		wantErr bool
	}{
		{"seconds", "@every 30s", 30, false},
		{"minutes", "@every 5m", 300, false},
		{"hours", "@every 1h", 3600, false},
		{"with spaces", "@every  30s", 30, false},
		{"invalid", "@every abc", 0, true},
		{"missing unit", "@every 30", 0, true},
		{"negative", "@every -5s", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Parse() unexpected error: %v", err)
				return
			}
			if s.Interval != time.Duration(tt.wantSec)*time.Second {
				t.Errorf("Parse().Interval = %v, want %v", s.Interval, time.Duration(tt.wantSec)*time.Second)
			}
			if s.Type != ScheduleTypeInterval {
				t.Errorf("Parse().Type = %v, want ScheduleTypeInterval", s.Type)
			}
		})
	}
}

func TestParseCron(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"every second", "* * * * * *", false},
		{"every minute", "0 * * * * *", false},
		{"daily at midnight", "0 0 0 * * *", false},
		{"daily at 9am", "0 0 9 * * *", false},
		{"invalid seconds", "60 * * * * *", true},
		{"invalid day", "0 0 0 32 * *", true},
		{"too few fields", "0 0 9 * *", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Parse() unexpected error: %v", err)
				return
			}
			if s.Type != ScheduleTypeCron {
				t.Errorf("Parse().Type = %v, want ScheduleTypeCron", s.Type)
			}
		})
	}
}

func TestParseOnce(t *testing.T) {
	s, err := Parse("@once")
	if err != nil {
		t.Errorf("Parse(@once) error: %v", err)
	}
	if s.Type != ScheduleTypeOnce {
		t.Errorf("Parse(@once).Type = %v, want ScheduleTypeOnce", s.Type)
	}
	if !s.IsOneShot() {
		t.Error("Parse(@once).IsOneShot() = false, want true")
	}
}

func TestScheduleNextRun(t *testing.T) {
	// Test interval schedule with known last run (resuming)
	lastRun := time.Now().Add(-2 * time.Hour) // Last ran 2 hours ago
	s, _ := ParseWithLastRun("@every 1h", lastRun)
	before := time.Now()
	_ = s.NextRun()

	// Next run should be approximately 1 hour from last run (1 hour from 2 hours ago = 1 hour ago)
	// Since we've fallen behind, it should run immediately (within 1 second of now)
	if s.Next.After(before.Add(time.Second)) {
		t.Errorf("NextRun() = %v, want within 1s of %v (should run immediately when behind)", s.Next, before)
	}

	// Verify time didn't go backwards (NextRun advances)
	nextBeforeAdvancing := s.Next
	_ = s.NextRun()
	if s.Next.Before(nextBeforeAdvancing) {
		t.Error("NextRun() went backwards")
	}
}

func TestScheduleNextRunNeverRun(t *testing.T) {
	// Test interval schedule with no last run (never run before)
	// Should run immediately
	s, _ := Parse("@every 1h")
	before := time.Now()
	_ = s.NextRun()

	// Should run immediately since never run (within 1 second of now)
	if s.Next.After(before.Add(time.Second)) {
		t.Errorf("NextRun() = %v, want immediately (never run before)", s.Next)
	}
}

func TestScheduleSleepUntil(t *testing.T) {
	// Schedule for 1 second in the future
	s := &Schedule{
		Type:     ScheduleTypeInterval,
		Interval: time.Second,
		Next:     time.Now().Add(time.Second),
	}

	// Should sleep approximately 1 second
	sleep := s.SleepUntil()

	if sleep < 900*time.Millisecond || sleep > 1100*time.Millisecond {
		t.Errorf("SleepUntil() = %v, expected ~1s", sleep)
	}

	// Test past schedule (should return 0)
	s.Next = time.Now().Add(-time.Second)
	if sleep := s.SleepUntil(); sleep != 0 {
		t.Errorf("SleepUntil() for past schedule = %v, want 0", sleep)
	}
}

func TestIsOneShot(t *testing.T) {
	s, _ := Parse("@once")
	if !s.IsOneShot() {
		t.Error("@once schedule should be one-shot")
	}

	s, _ = Parse("@every 30s")
	if s.IsOneShot() {
		t.Error("@every schedule should not be one-shot")
	}

	s, _ = Parse("0 * * * * *")
	if s.IsOneShot() {
		t.Error("cron schedule should not be one-shot")
	}
}

func TestFormatInterval(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{time.Second, "1 second"},
		{5 * time.Second, "5 seconds"},
		{time.Minute, "1 minute"},
		{5 * time.Minute, "5 minutes"},
		{time.Hour, "1 hour"},
		{2 * time.Hour, "2 hours"},
		{time.Hour + 30*time.Minute, "1h 30m"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
	}

	for _, tt := range tests {
		result := FormatInterval(tt.input)
		if result != tt.expected {
			t.Errorf("FormatInterval(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
