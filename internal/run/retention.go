package run

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Month is one monthly partition range. ADR 0021 partitions raw Run storage by month and
// expires it by dropping whole partitions, so the unit of retention is a month even though
// the retention window an operator configures is expressed in days.
type Month struct {
	Year  int
	Month time.Month
}

// MonthOf returns the partition a UTC instant belongs to.
func MonthOf(instant time.Time) Month {
	utc := instant.UTC()
	return Month{Year: utc.Year(), Month: utc.Month()}
}

// Start is the inclusive lower bound of the range, in UTC.
func (value Month) Start() time.Time {
	return time.Date(value.Year, value.Month, 1, 0, 0, 0, 0, time.UTC)
}

// End is the exclusive upper bound of the range, in UTC, which is the next month's Start.
func (value Month) End() time.Time { return value.Next().Start() }

// Next returns the following month.
func (value Month) Next() Month { return MonthOf(value.Start().AddDate(0, 1, 0)) }

// Suffix is the stable identifier a partition name ends in, such as "2026_07".
func (value Month) Suffix() string {
	return fmt.Sprintf("%04d_%02d", value.Year, int(value.Month))
}

// ParseMonthSuffix reads back a Suffix. A name it does not recognise is not ours to manage:
// ADR 0025 leaves an operator-attached partition alone rather than guessing its range.
func ParseMonthSuffix(suffix string) (Month, bool) {
	year, month, found := strings.Cut(suffix, "_")
	if !found || len(year) != 4 || len(month) != 2 {
		return Month{}, false
	}
	parsedYear, err := strconv.Atoi(year)
	if err != nil || parsedYear < 1 {
		return Month{}, false
	}
	parsedMonth, err := strconv.Atoi(month)
	if err != nil || parsedMonth < 1 || parsedMonth > 12 {
		return Month{}, false
	}
	return Month{Year: parsedYear, Month: time.Month(parsedMonth)}, true
}

// Retention window defaults and bounds. ADR 0021 requires the default to be small enough
// that an unattended installation cannot fill its disk with raw rows.
const (
	// DefaultRetentionDays is the retention window an operator who configures nothing gets.
	DefaultRetentionDays = 30
	// MinRetentionDays keeps at least a day of raw rows, which is the shortest window in
	// which "what happened last night" is still answerable.
	MinRetentionDays = 1
	// MaxRetentionDays bounds raw retention at roughly two years. Longer horizons are the
	// job of the rollup tables ADR 0021 keeps separate.
	MaxRetentionDays = 730
	// DefaultPartitionsAhead is how many months beyond the current one are created in
	// advance, so a missing future partition is an operational alert rather than an insert
	// failure discovered at midnight.
	DefaultPartitionsAhead = 2
	// MaxPartitionsAhead bounds the lookahead so maintenance cannot be asked to create years
	// of empty partitions.
	MaxPartitionsAhead = 24
)

// Retention is the operator-configured raw retention window, in whole days (ADR 0021).
type Retention struct {
	Days int
}

// NewRetention validates an operator-supplied window.
func NewRetention(days int) (Retention, error) {
	if days < MinRetentionDays || days > MaxRetentionDays {
		return Retention{}, fmt.Errorf("raw retention is %d to %d whole days", MinRetentionDays, MaxRetentionDays)
	}
	return Retention{Days: days}, nil
}

// DefaultRetention is the window an installation that configures nothing gets.
func DefaultRetention() Retention { return Retention{Days: DefaultRetentionDays} }

// Cutoff is the instant before which data is no longer required to be kept.
func (value Retention) Cutoff(now time.Time) time.Time {
	return now.UTC().AddDate(0, 0, -value.Days)
}

// Expired reports whether a whole partition has aged out. The entire range must be older
// than the cutoff, because dropping a partition drops rows the window still covers otherwise.
//
// The consequence, recorded in ADR 0025, is that effective retention exceeds the configured
// window by up to a month. Retention is a floor on what is kept, not a ceiling.
func (value Retention) Expired(month Month, now time.Time) bool {
	return !month.End().After(value.Cutoff(now))
}

// PartitionPlan is the set of months maintenance should have in place at an instant: the
// month now falls in, plus a bounded lookahead.
func PartitionPlan(now time.Time, monthsAhead int) ([]Month, error) {
	if monthsAhead < 0 || monthsAhead > MaxPartitionsAhead {
		return nil, fmt.Errorf("partition lookahead is 0 to %d months", MaxPartitionsAhead)
	}
	if now.IsZero() {
		return nil, errors.New("a partition plan requires the current instant")
	}
	months := make([]Month, 0, monthsAhead+1)
	current := MonthOf(now)
	for index := 0; index <= monthsAhead; index++ {
		months = append(months, current)
		current = current.Next()
	}
	return months, nil
}
