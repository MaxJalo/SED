package store

import (
	"testing"
	"time"
)

func TestCursorRoundTrip(t *testing.T) {
	stamp := time.Date(2026, 11, 20, 7, 8, 9, 10, time.UTC)
	encoded := encodeCursor(stamp, "b0000001-0000-4000-8000-000000000001")
	gotTime, gotID, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(stamp) || gotID != "b0000001-0000-4000-8000-000000000001" {
		t.Fatalf("got %s %s", gotTime, gotID)
	}
	if _, _, err := DecodeCursor("%%%"); err == nil {
		t.Fatal("expected invalid cursor")
	}
}
