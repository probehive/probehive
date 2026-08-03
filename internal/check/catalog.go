// Package check owns the check catalog and versioned configuration validation.
package check

import (
	"encoding/json"
	"fmt"
)

// Catalog is the first-party check catalog. Its zero value is ready to use.
type Catalog struct{}

// NewCatalog returns the default catalog for this build.
func NewCatalog() Catalog { return Catalog{} }

// IsSupported reports whether this build contains a check type.
func (Catalog) IsSupported(checkType string) bool { return checkType == HTTPCheckType }

// Validate validates and compacts a versioned check configuration. Each failure triple
// contains stable code at index 0, field path at index 1, and current English message at
// index 2, in encounter order.
func (Catalog) Validate(checkType string, schemaVersion int, configuration json.RawMessage) (json.RawMessage, [][3]string) {
	if checkType != HTTPCheckType {
		return nil, [][3]string{{
			CheckTypeUnsupportedCode, "checkType",
			fmt.Sprintf("The check type '%s' is not supported by this build.", checkType),
		}}
	}
	_, canonical, failures := ValidateHTTP(schemaVersion, configuration)
	return canonical, failures
}
