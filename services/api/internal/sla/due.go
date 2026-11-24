package sla

import "time"

const (
	ImportanceNormal    = "обычная"
	ImportanceHigh      = "повышенная"
	ImportanceImmediate = "немедленная"
)

// AddWorking returns the deadline in UTC. One working day is 8 hours inside
// 09:00–17:00, Monday to Friday. Enterprise holidays are not in this calendar yet.
func AddWorking(start time.Time, dueDays int, importance string) time.Time {
	days, hours := dueDays, 0
	switch importance {
	case ImportanceImmediate:
		days, hours = 0, 4
	case ImportanceHigh:
		if days > 0 {
			days = (days + 1) / 2
			if days < 1 {
				days = 1
			}
		}
	case "", ImportanceNormal:
	default:
	}
	hours += days * 8
	if hours <= 0 {
		return start.UTC()
	}
	cursor := nextOpen(start.UTC())
	for hours > 0 {
		closeAt := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), 17, 0, 0, 0, time.UTC)
		room := int(closeAt.Sub(cursor).Hours())
		if room >= hours {
			return cursor.Add(time.Duration(hours) * time.Hour)
		}
		hours -= room
		cursor = nextOpen(closeAt)
	}
	return cursor
}

func DeadlineWithin(now time.Time, workingHours int) time.Time {
	cursor := nextOpen(now.UTC())
	left := workingHours
	for left > 0 {
		closeAt := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), 17, 0, 0, 0, time.UTC)
		room := int(closeAt.Sub(cursor).Hours())
		if room >= left {
			return cursor.Add(time.Duration(left) * time.Hour)
		}
		left -= room
		cursor = nextOpen(closeAt)
	}
	return cursor
}

func nextOpen(t time.Time) time.Time {
	for i := 0; i < 14; i++ {
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		switch t.Weekday() {
		case time.Saturday:
			t = day.AddDate(0, 0, 2).Add(9 * time.Hour)
			continue
		case time.Sunday:
			t = day.AddDate(0, 0, 1).Add(9 * time.Hour)
			continue
		}
		open := day.Add(9 * time.Hour)
		closeAt := day.Add(17 * time.Hour)
		if t.Before(open) {
			return open
		}
		if !t.Before(closeAt) {
			t = open.Add(24 * time.Hour)
			continue
		}
		return t
	}
	return t
}
