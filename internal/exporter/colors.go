package exporter

import "strings"

// ColorStatus is the interpretation of a script's HandlerResult.background
// RGB string, aligned with miaospeed-scripts/consts/colors.ts.
type ColorStatus struct {
	Value      float64
	Label      string
	Skip       bool // true for C_NA ("142,140,142"): do not export a metric at all
	Recognized bool // false when rgb did not match any known color constant
}

const colorNA = "142,140,142"

// colorValues maps the known background RGB strings to a numeric
// unlock-status value and a short label.
var colorValues = map[string]ColorStatus{
	"186,230,126": {Value: 1, Label: "unlocked", Recognized: true},
	"239,107,115": {Value: 0, Label: "failed", Recognized: true},
	"92,207,230":  {Value: -1, Label: "unknown", Recognized: true},
	"253,109,20":  {Value: 0.5, Label: "warning", Recognized: true},
}

// normalizeColor trims whitespace around each comma-separated component so
// "186, 230, 126" and "186,230,126" compare equal.
func normalizeColor(rgb string) string {
	parts := strings.Split(rgb, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, ",")
}

// ClassifyColor maps a script's background RGB string to a ColorStatus.
func ClassifyColor(rgb string) ColorStatus {
	norm := normalizeColor(rgb)
	if norm == colorNA {
		return ColorStatus{Label: "n/a", Skip: true}
	}
	if cs, ok := colorValues[norm]; ok {
		return cs
	}
	return ColorStatus{Value: -1, Label: "unknown", Recognized: false}
}

// statusValues maps a script's explicit `status` string to a ColorStatus,
// mirroring colorValues' semantics for scripts that report status directly
// instead of (or in addition to) a background color.
var statusValues = map[string]ColorStatus{
	"unlocked": {Value: 1, Label: "unlocked", Recognized: true},
	"failed":   {Value: 0, Label: "failed", Recognized: true},
	"warning":  {Value: 0.5, Label: "warning", Recognized: true},
	"unknown":  {Value: -1, Label: "unknown", Recognized: true},
	"na":       {Label: "n/a", Skip: true, Recognized: true},
	"n/a":      {Label: "n/a", Skip: true, Recognized: true},
}

// Classify resolves a script result's status, preferring the explicit
// `status` string (engine.Result.Status) when the script sets one and
// falling back to background-color classification (ClassifyColor) for
// scripts that only follow the original miaospeed-scripts {text,
// background} contract.
func Classify(status, background string) ColorStatus {
	norm := strings.ToLower(strings.TrimSpace(status))
	if norm == "" {
		return ClassifyColor(background)
	}
	if cs, ok := statusValues[norm]; ok {
		return cs
	}
	return ColorStatus{Value: -1, Label: "unknown", Recognized: false}
}
