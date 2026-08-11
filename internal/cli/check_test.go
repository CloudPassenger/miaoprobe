package cli

import (
	"context"
	"testing"

	"github.com/CloudPassenger/miaoprobe/internal/engine"
	"github.com/CloudPassenger/miaoprobe/internal/probe"
	"github.com/CloudPassenger/miaoprobe/internal/script"
)

func TestToRowIncludesNormalizedFailure(t *testing.T) {
	row := toRow(probe.Outcome{
		Script:     script.Script{ID: "netflix", Name: "Netflix"},
		Result:     engine.Result{Status: "failed", Error: "\u7f51\u7edc"},
		RequestErr: context.DeadlineExceeded,
	})

	if row.Status != "failed" || row.FailureClass != "network" || row.FailureReason != "timeout" {
		t.Fatalf("unexpected row: %+v", row)
	}
}
