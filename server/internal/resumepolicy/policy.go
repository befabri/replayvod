// Package resumepolicy defines defaults for viewers who have not saved preferences.
package resumepolicy

// Default thresholds use seconds and whole percentages and must match the
// database defaults; changing them requires a new migration.
const (
	MinSeconds       = 5.0
	EndMarginSeconds = 30.0
	EndMarginPercent = 5.0
)
