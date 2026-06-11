package util

import (
	"testing"
	"time"
)

func TestIsClockWithinTimeRangeSupportsMultipleRanges(t *testing.T) {
	timeRanges := "08:00-12:00,14:00-18:00"

	if !IsClockWithinTimeRange("09:30", timeRanges) {
		t.Fatal("expected 09:30 to be inside the first range")
	}
	if !IsClockWithinTimeRange("14:00", timeRanges) {
		t.Fatal("expected 14:00 to be inside the second range")
	}
	if IsClockWithinTimeRange("13:00", timeRanges) {
		t.Fatal("expected 13:00 to be outside all ranges")
	}
}

func TestIsClockWithinTimeRangeSupportsCrossDayRange(t *testing.T) {
	timeRanges := "08:00-12:00,22:00-06:00"

	if !IsClockWithinTimeRange("23:30", timeRanges) {
		t.Fatal("expected 23:30 to be inside cross-day range")
	}
	if !IsClockWithinTimeRange("05:30", timeRanges) {
		t.Fatal("expected 05:30 to be inside cross-day range")
	}
	if IsClockWithinTimeRange("13:00", timeRanges) {
		t.Fatal("expected 13:00 to be outside all ranges")
	}
}

func TestIsClockWithinTimeRangeAllowsTwentyFourHundred(t *testing.T) {
	if !IsClockWithinTimeRange("23:59", "00:00-24:00") {
		t.Fatal("expected 00:00-24:00 to cover the whole day")
	}
}

func TestIsClockWithinTimeRangeTreatsEqualStartEndAsDisabledRange(t *testing.T) {
	if IsClockWithinTimeRange("08:00", "08:00-08:00") {
		t.Fatal("expected equal start/end range to be disabled")
	}
	if IsClockWithinTimeRange("13:00", "00:00-00:00") {
		t.Fatal("expected zero-length all-day-looking range to be disabled")
	}
	if !IsClockWithinTimeRange("09:30", "08:00-08:00,09:00-10:00") {
		t.Fatal("expected later valid range to still be honored")
	}
}

func TestIsClockWithinTimeRangeDefaultsAllowWhenNoValidRange(t *testing.T) {
	if !IsClockWithinTimeRange("13:00", "bad-range") {
		t.Fatal("expected invalid-only record_time to default allow")
	}
}

func TestIsTimeRangeEndingSoonDetectsEndBoundary(t *testing.T) {
	beforeEnd := time.Date(2026, 6, 11, 8, 59, 59, 0, time.Local)
	if !IsTimeRangeEndingSoon(beforeEnd, "08:00-09:00", 2*time.Second) {
		t.Fatal("expected time just before end boundary to be treated as ending soon")
	}

	atEndMinute := time.Date(2026, 6, 11, 9, 0, 30, 0, time.Local)
	if !IsTimeRangeEndingSoon(atEndMinute, "08:00-09:00", 2*time.Second) {
		t.Fatal("expected end minute to be treated as ending soon")
	}

	notNearEnd := time.Date(2026, 6, 11, 8, 59, 57, 0, time.Local)
	if IsTimeRangeEndingSoon(notNearEnd, "08:00-09:00", 2*time.Second) {
		t.Fatal("expected time outside guard window not to be treated as ending soon")
	}
}

func TestIsTimeRangeEndingSoonKeepsAllDayDefaultContinuous(t *testing.T) {
	now := time.Date(2026, 6, 11, 23, 59, 59, 0, time.Local)
	if IsTimeRangeEndingSoon(now, "00:00-23:59", 2*time.Second) {
		t.Fatal("expected default all-day range not to be treated as ending soon")
	}
	if IsTimeRangeEndingSoon(now, "00:00-24:00", 2*time.Second) {
		t.Fatal("expected explicit all-day range not to be treated as ending soon")
	}

	rangeEnd := time.Date(2026, 6, 11, 9, 0, 0, 0, time.Local)
	if IsTimeRangeEndingSoon(rangeEnd, "00:00-23:59,08:00-09:00", 2*time.Second) {
		t.Fatal("expected all-day range to keep mixed schedule continuous")
	}
}

func TestIsTimeRangeEndingSoonSupportsCrossDayRange(t *testing.T) {
	beforeEnd := time.Date(2026, 6, 11, 5, 59, 59, 0, time.Local)
	if !IsTimeRangeEndingSoon(beforeEnd, "22:00-06:00", 2*time.Second) {
		t.Fatal("expected cross-day range near morning end to be treated as ending soon")
	}

	evening := time.Date(2026, 6, 11, 23, 30, 0, 0, time.Local)
	if IsTimeRangeEndingSoon(evening, "22:00-06:00", 2*time.Second) {
		t.Fatal("expected evening side of cross-day range not to be treated as ending soon")
	}
}
