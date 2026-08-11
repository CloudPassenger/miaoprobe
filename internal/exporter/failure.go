package exporter

import (
	"strings"

	"github.com/CloudPassenger/miaoprobe/internal/network"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
)

const (
	FailureClassAvailability = "availability"
	FailureClassNetwork      = "network"
	FailureClassService      = "service"
	FailureClassProbe        = "probe"

	FailureReasonRegionUnavailable  = "region_unavailable"
	FailureReasonIPBlocked          = "ip_blocked"
	FailureReasonRateLimited        = "rate_limited"
	FailureReasonParseError         = "parse_error"
	FailureReasonUnexpectedResponse = "unexpected_response"
	FailureReasonScriptError        = "script_error"
	FailureReasonUnknown            = "unknown"
)

const (
	scriptErrorNetwork   = "\u7f51\u7edc"
	scriptErrorIPBlock   = "ip\u963b\u6b62"
	scriptErrorRateLimit = "\u9650\u6d41"
	scriptErrorParse     = "\u89e3\u6790"
	scriptErrorResponse  = "\u54cd\u5e94"
	scriptErrorStatus    = "\u72b6\u6001"
	scriptErrorDevice    = "\u8bbe\u5907"
	scriptErrorToken     = "\u4ee4\u724c"
)

// FailureInfo is a normalized, low-cardinality explanation of the latest
// failed or indeterminate probe result.
type FailureInfo struct {
	Class  string
	Reason string
}

// ClassifyFailure maps execution errors, script business errors and the
// latest exhausted fetch error to a stable failure class and reason.
func ClassifyFailure(outcome probe.Outcome) (FailureInfo, bool) {
	if outcome.Err != nil {
		return FailureInfo{Class: FailureClassProbe, Reason: FailureReasonScriptError}, true
	}

	status := Classify(outcome.Result.Status, outcome.Result.Background)
	if status.Skip || status.Value == 1 || status.Value == 0.5 {
		return FailureInfo{}, false
	}

	detail := strings.ToLower(strings.TrimSpace(outcome.Result.Error + " " + outcome.Result.Message))
	switch {
	case containsAny(detail, scriptErrorNetwork, "network"):
		reason := network.ClassifyRequestError(outcome.RequestErr)
		if reason == "" {
			reason = FailureReasonUnknown
		}
		return FailureInfo{Class: FailureClassNetwork, Reason: reason}, true
	case containsAny(detail, scriptErrorIPBlock, "ip blocked", "ip block", "banned"):
		return FailureInfo{Class: FailureClassService, Reason: FailureReasonIPBlocked}, true
	case containsAny(detail, scriptErrorRateLimit, "rate limit", "too many requests"):
		return FailureInfo{Class: FailureClassService, Reason: FailureReasonRateLimited}, true
	case containsAny(detail, scriptErrorParse, "parse", "decode", "unmarshal"):
		return FailureInfo{Class: FailureClassProbe, Reason: FailureReasonParseError}, true
	case containsAny(detail, scriptErrorResponse, "response", scriptErrorStatus, "status", scriptErrorDevice, "device", scriptErrorToken, "token"):
		return FailureInfo{Class: FailureClassProbe, Reason: FailureReasonUnexpectedResponse}, true
	case detail != "":
		return FailureInfo{Class: FailureClassProbe, Reason: FailureReasonUnknown}, true
	case status.Value == 0:
		return FailureInfo{Class: FailureClassAvailability, Reason: FailureReasonRegionUnavailable}, true
	default:
		return FailureInfo{Class: FailureClassProbe, Reason: FailureReasonUnknown}, true
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
