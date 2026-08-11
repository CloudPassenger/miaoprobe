package exporter

import (
	"errors"
	"testing"

	"github.com/CloudPassenger/miaoprobe/internal/engine"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
)

func TestClassifyWarning(t *testing.T) {
	tests := []struct {
		name       string
		outcome    probe.Outcome
		want       WarningInfo
		wantExists bool
	}{
		{
			name:    "non-warning",
			outcome: probe.Outcome{Result: engine.Result{Status: "unlocked"}},
		},
		{
			name:    "execution error",
			outcome: probe.Outcome{Err: errors.New("boom")},
		},
		{
			name:       "waf blocked",
			outcome:    probe.Outcome{Result: engine.Result{Status: "warning", StatusReason: "waf_blocked"}},
			want:       WarningInfo{Class: WarningClassAccess, Reason: WarningReasonWAFBlocked},
			wantExists: true,
		},
		{
			name:       "originals only",
			outcome:    probe.Outcome{Result: engine.Result{Status: "warning", StatusReason: "originals_only"}},
			want:       WarningInfo{Class: WarningClassRestriction, Reason: WarningReasonOriginalsOnly},
			wantExists: true,
		},
		{
			name:       "overseas only",
			outcome:    probe.Outcome{Result: engine.Result{Status: "warning", StatusReason: "overseas_only"}},
			want:       WarningInfo{Class: WarningClassRestriction, Reason: WarningReasonOverseasOnly},
			wantExists: true,
		},
		{
			name:       "legacy warning",
			outcome:    probe.Outcome{Result: engine.Result{Status: "warning"}},
			want:       WarningInfo{Class: WarningClassRestriction, Reason: WarningReasonPartialAccess},
			wantExists: true,
		},
		{
			name:       "unknown structured reason",
			outcome:    probe.Outcome{Result: engine.Result{Status: "warning", StatusReason: "new_reason"}},
			want:       WarningInfo{Class: WarningClassProbe, Reason: WarningReasonUnknown},
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exists := ClassifyWarning(tt.outcome)
			if exists != tt.wantExists || got != tt.want {
				t.Fatalf("ClassifyWarning() = (%+v, %v), want (%+v, %v)", got, exists, tt.want, tt.wantExists)
			}
		})
	}
}
