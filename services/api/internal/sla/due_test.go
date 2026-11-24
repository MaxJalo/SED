package sla

import (
	"testing"
	"time"
)

func TestAddWorkingImmediateFromFridayEvening(t *testing.T) {
	start := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC) // Friday
	got := AddWorking(start, 3, ImportanceImmediate)
	want := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC) // Monday 12:00
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestHighImportanceHalvesDays(t *testing.T) {
	start := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC) // Monday
	got := AddWorking(start, 3, ImportanceHigh)
	want := time.Date(2026, 11, 17, 17, 0, 0, 0, time.UTC) // Tuesday 17:00, 2 working days
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestDeadlineWithinSixteenWorkingHours(t *testing.T) {
	start := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	got := DeadlineWithin(start, 16)
	want := time.Date(2026, 11, 17, 17, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}
