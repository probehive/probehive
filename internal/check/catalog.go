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

// Validate validates and compacts a versioned check configuration. Each failure pair
// contains field path at index 0 and exact message at index 1 in encounter order.
func (Catalog) Validate(checkType string, schemaVersion int, configuration json.RawMessage) (json.RawMessage, [][2]string) {
	if checkType != HTTPCheckType {
		return nil, [][2]string{{"checkType", fmt.Sprintf("The check type '%s' is not supported by this build.", checkType)}}
	}
	_, canonical, failures := ValidateHTTP(schemaVersion, configuration)
	return canonical, failures
}
