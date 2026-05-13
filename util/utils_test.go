package util

import "testing"

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

func TestIsClockWithinTimeRangeDefaultsAllowWhenNoValidRange(t *testing.T) {
	if !IsClockWithinTimeRange("13:00", "bad-range") {
		t.Fatal("expected invalid-only record_time to default allow")
	}
}
