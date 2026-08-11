package exporter

import (
	"context"
	"errors"
	"testing"

	"github.com/CloudPassenger/miaoprobe/internal/engine"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
)

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name       string
		outcome    probe.Outcome
		want       FailureInfo
		wantExists bool
	}{
		{
			name:       "execution error",
			outcome:    probe.Outcome{Err: errors.New("boom")},
			want:       FailureInfo{Class: FailureClassProbe, Reason: FailureReasonScriptError},
			wantExists: true,
		},
		{
			name:    "successful result ignores earlier request error",
			outcome: probe.Outcome{Result: engine.Result{Status: "unlocked"}, RequestErr: context.DeadlineExceeded},
		},
		{
			name:       "region unavailable",
			outcome:    probe.Outcome{Result: engine.Result{Status: "failed"}},
			want:       FailureInfo{Class: FailureClassAvailability, Reason: FailureReasonRegionUnavailable},
			wantExists: true,
		},
		{
			name:       "network timeout",
			outcome:    probe.Outcome{Result: engine.Result{Status: "failed", Error: "\u7f51\u7edc"}, RequestErr: context.DeadlineExceeded},
			want:       FailureInfo{Class: FailureClassNetwork, Reason: "timeout"},
			wantExists: true,
		},
		{
			name:       "network unknown",
			outcome:    probe.Outcome{Result: engine.Result{Status: "failed", Error: "\u7f51\u7edc"}},
			want:       FailureInfo{Class: FailureClassNetwork, Reason: FailureReasonUnknown},
			wantExists: true,
		},
		{
			name:       "ip blocked",
			outcome:    probe.Outcome{Result: engine.Result{Status: "failed", Error: "IP\u963b\u6b62"}},
			want:       FailureInfo{Class: FailureClassService, Reason: FailureReasonIPBlocked},
			wantExists: true,
		},
		{
			name:       "rate limited",
			outcome:    probe.Outcome{Result: engine.Result{Status: "failed", Error: "\u9650\u6d41"}},
			want:       FailureInfo{Class: FailureClassService, Reason: FailureReasonRateLimited},
			wantExists: true,
		},
		{
			name:       "parse message",
			outcome:    probe.Outcome{Result: engine.Result{Status: "unknown", Message: "\u89e3\u6790"}},
			want:       FailureInfo{Class: FailureClassProbe, Reason: FailureReasonParseError},
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, exists := ClassifyFailure(tt.outcome)
			if exists != tt.wantExists || got != tt.want {
				t.Fatalf("ClassifyFailure() = (%+v, %v), want (%+v, %v)", got, exists, tt.want, tt.wantExists)
			}
		})
	}
}
