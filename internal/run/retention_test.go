package run

import (
	"testing"
	"time"
)

func TestMonthBoundsAreUTCAndHalfOpen(t *testing.T) {
	t.Parallel()
	month := MonthOf(time.Date(2026, time.July, 27, 23, 59, 59, 0, time.UTC))
	if month.Year != 2026 || month.Month != time.July {
		t.Fatalf("MonthOf() = %v, want 2026-07", month)
	}
	wantStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	if !month.Start().Equal(wantStart) || !month.End().Equal(wantEnd) {
		t.Fatalf("Month bounds = [%v, %v), want [%v, %v)", month.Start(), month.End(), wantStart, wantEnd)
	}
}

// A non-UTC instant near a month boundary belongs to the month its UTC representation falls
// in, because the partition key is stored in UTC.
func TestMonthOfNormalizesToUTC(t *testing.T) {
	t.Parallel()
	shanghai := time.FixedZone("CST", 8*3600)
	instant := time.Date(2026, time.August, 1, 3, 0, 0, 0, shanghai)
	if month := MonthOf(instant); month.Month != time.July {
		t.Fatalf("MonthOf(%v) = %v, want July from its UTC instant", instant, month)
	}
}

func TestMonthRollsOverTheYear(t *testing.T) {
	t.Parallel()
	december := Month{Year: 2026, Month: time.December}
	next := december.Next()
	if next.Year != 2027 || next.Month != time.January {
		t.Fatalf("December.Next() = %v, want 2027-01", next)
	}
	if got := december.Suffix(); got != "2026_12" {
		t.Fatalf("Suffix() = %q, want %q", got, "2026_12")
	}
}

func TestMonthSuffixRoundTrips(t *testing.T) {
	t.Parallel()
	for _, month := range []Month{
		{Year: 2026, Month: time.January},
		{Year: 2026, Month: time.July},
		{Year: 2099, Month: time.December},
	} {
		parsed, ok := ParseMonthSuffix(month.Suffix())
		if !ok || parsed != month {
			t.Fatalf("ParseMonthSuffix(%q) = %v/%v, want %v/true", month.Suffix(), parsed, ok, month)
		}
	}
}

// An unrecognised suffix means the partition is not ours to manage, so parsing
// must reject rather than guess.
func TestParseMonthSuffixRejectsUnrecognisedNames(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{
		"", "2026", "2026_", "_07", "26_07", "2026_7", "2026_13", "2026_00",
		"2026_07_old", "abcd_ef", "2026-07", "0000_07",
	} {
		if month, ok := ParseMonthSuffix(suffix); ok {
			t.Fatalf("ParseMonthSuffix(%q) = %v/true, want false", suffix, month)
		}
	}
}

func TestRetentionValidation(t *testing.T) {
	t.Parallel()
	for _, days := range []int{MinRetentionDays, DefaultRetentionDays, MaxRetentionDays} {
		if _, err := NewRetention(days); err != nil {
			t.Fatalf("NewRetention(%d) error = %v", days, err)
		}
	}
	for _, days := range []int{0, -1, MaxRetentionDays + 1} {
		if _, err := NewRetention(days); err == nil {
			t.Fatalf("NewRetention(%d) = nil error, want a rejection", days)
		}
	}
	if got := DefaultRetention().Days; got != DefaultRetentionDays {
		t.Fatalf("DefaultRetention().Days = %d, want %d", got, DefaultRetentionDays)
	}
}

// A partition is dropped only once its entire range has aged out, which is what makes
// effective retention exceed the configured window by up to a month.
func TestRetentionExpiresOnlyWhollyAgedPartitions(t *testing.T) {
	t.Parallel()
	retention := Retention{Days: 30}
	july := Month{Year: 2026, Month: time.July}

	cases := map[string]struct {
		now  time.Time
		want bool
	}{
		"inside the month":            {time.Date(2026, time.July, 15, 0, 0, 0, 0, time.UTC), false},
		"one day after the month":     {time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC), false},
		"cutoff still inside July":    {time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC), false},
		"cutoff exactly at month end": {time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC), true},
		"well past the window":        {time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC), true},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := retention.Expired(july, testCase.now); got != testCase.want {
				t.Fatalf("Expired(2026-07, %v) = %v, want %v", testCase.now, got, testCase.want)
			}
		})
	}
}

func TestPartitionPlanCoversTheCurrentMonthAndLookahead(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 20, 12, 0, 0, 0, time.UTC)
	months, err := PartitionPlan(now, DefaultPartitionsAhead)
	if err != nil {
		t.Fatalf("PartitionPlan() error = %v", err)
	}
	want := []Month{
		{Year: 2026, Month: time.November},
		{Year: 2026, Month: time.December},
		{Year: 2027, Month: time.January},
	}
	if len(months) != len(want) {
		t.Fatalf("PartitionPlan() = %v, want %v", months, want)
	}
	for index, month := range want {
		if months[index] != month {
			t.Fatalf("PartitionPlan()[%d] = %v, want %v", index, months[index], month)
		}
	}
}

func TestPartitionPlanRejectsUnusableLookahead(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC)
	if _, err := PartitionPlan(now, -1); err == nil {
		t.Fatalf("PartitionPlan(-1) = nil error, want a rejection")
	}
	if _, err := PartitionPlan(now, MaxPartitionsAhead+1); err == nil {
		t.Fatalf("PartitionPlan(too many) = nil error, want a rejection")
	}
	if _, err := PartitionPlan(time.Time{}, 1); err == nil {
		t.Fatalf("PartitionPlan(zero instant) = nil error, want a rejection")
	}
	months, err := PartitionPlan(now, 0)
	if err != nil || len(months) != 1 {
		t.Fatalf("PartitionPlan(0) = %v/%v, want exactly the current month", months, err)
	}
}
