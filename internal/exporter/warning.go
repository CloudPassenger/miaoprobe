package exporter

import (
	"strings"

	"github.com/CloudPassenger/miaoprobe/internal/probe"
)

const (
	WarningClassAccess      = "access"
	WarningClassRestriction = "restriction"
	WarningClassProbe       = "probe"

	WarningReasonWAFBlocked    = "waf_blocked"
	WarningReasonOriginalsOnly = "originals_only"
	WarningReasonOverseasOnly  = "overseas_only"
	WarningReasonPartialAccess = "partial_access"
	WarningReasonUnknown       = "unknown"
)

// WarningInfo is a normalized, low-cardinality explanation of the latest
// partially available or obstructed probe result.
type WarningInfo struct {
	Class  string
	Reason string
}

// ClassifyWarning maps a structured script statusReason to a stable warning
// class and reason. Missing reasons from older scripts safely fall back to a
// generic restriction instead of disappearing from warning aggregations.
func ClassifyWarning(outcome probe.Outcome) (WarningInfo, bool) {
	if outcome.Err != nil {
		return WarningInfo{}, false
	}

	status := Classify(outcome.Result.Status, outcome.Result.Background)
	if status.Skip || status.Value != 0.5 {
		return WarningInfo{}, false
	}

	switch strings.ToLower(strings.TrimSpace(outcome.Result.StatusReason)) {
	case WarningReasonWAFBlocked:
		return WarningInfo{Class: WarningClassAccess, Reason: WarningReasonWAFBlocked}, true
	case WarningReasonOriginalsOnly:
		return WarningInfo{Class: WarningClassRestriction, Reason: WarningReasonOriginalsOnly}, true
	case WarningReasonOverseasOnly:
		return WarningInfo{Class: WarningClassRestriction, Reason: WarningReasonOverseasOnly}, true
	case WarningReasonPartialAccess, "":
		return WarningInfo{Class: WarningClassRestriction, Reason: WarningReasonPartialAccess}, true
	default:
		return WarningInfo{Class: WarningClassProbe, Reason: WarningReasonUnknown}, true
	}
}
