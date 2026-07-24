// Package clock provides composition-time clock implementations.
package clock

import "time"

// System reads the operating system wall clock. Its zero value is ready to use.
type System struct{}

// Now returns the current instant represented in UTC.
func (System) Now() time.Time { return time.Now().UTC() }
