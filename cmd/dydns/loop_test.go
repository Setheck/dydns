package main

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestUpdateOnIntervalMarksSuccess(t *testing.T) {
	f := newFake()
	client, cfg := f.start(t)
	cfg.interval = time.Hour // one iteration, then block until cancelled

	ready := &readiness{}
	ctx, cancel := context.WithCancel(context.Background())

	cyclesBefore := testutil.ToFloat64(updateCyclesTotal)
	done := make(chan struct{})
	go func() {
		updateOnInterval(ctx, client, cfg, ready)
		close(done)
	}()

	waitFor(t, ready.ready, "readiness after first successful cycle")
	cancel()
	<-done

	if d := testutil.ToFloat64(updateCyclesTotal) - cyclesBefore; d < 1 {
		t.Errorf("update_cycles_total delta = %v, want at least 1", d)
	}
	if testutil.ToFloat64(lastSuccessTimestamp) == 0 {
		t.Error("last_success_timestamp_seconds was never set")
	}
	if got := testutil.ToFloat64(consecutiveFailures); got != 0 {
		t.Errorf("consecutive_failures = %v, want 0 after a success", got)
	}
}

func TestUpdateOnIntervalCountsFailures(t *testing.T) {
	f := newFake()
	f.listBody = listReply(110, "invalid api key")
	client, cfg := f.start(t)
	cfg.interval = time.Millisecond

	ready := &readiness{}
	ctx, cancel := context.WithCancel(context.Background())

	errorsBefore := stageErrors(stageListRecordsReply)
	done := make(chan struct{})
	go func() {
		updateOnInterval(ctx, client, cfg, ready)
		close(done)
	}()

	waitFor(t, func() bool {
		return stageErrors(stageListRecordsReply)-errorsBefore >= 2
	}, "at least two attributed failures")
	cancel()
	<-done

	if got := testutil.ToFloat64(consecutiveFailures); got < 2 {
		t.Errorf("consecutive_failures = %v, want at least 2", got)
	}
	if ready.ready() {
		t.Error("readiness must stay false while every cycle fails")
	}
}

// A cycle aborted by SIGTERM is a shutdown, not a failure: it must not be
// attributed to an error stage or inflate consecutive_failures.
func TestUpdateOnIntervalIgnoresShutdownCancellation(t *testing.T) {
	f := newFake()
	blocked := make(chan struct{})
	f.beforeList = func() { <-blocked }
	client, cfg := f.start(t)
	cfg.interval = time.Hour
	cfg.timeout = time.Hour

	consecutiveFailures.Set(0)
	errorsBefore := totalStageErrors()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		updateOnInterval(ctx, client, cfg, &readiness{})
		close(done)
	}()

	// Let the cycle reach the in-flight list call, then cancel as SIGTERM would.
	waitFor(t, func() bool { return f.listCalls.Load() > 0 }, "list request in flight")
	cancel()
	close(blocked)
	<-done

	if d := totalStageErrors() - errorsBefore; d != 0 {
		t.Errorf("update_cycle_errors_total delta = %v, want 0 on shutdown", d)
	}
	if got := testutil.ToFloat64(consecutiveFailures); got != 0 {
		t.Errorf("consecutive_failures = %v, want 0 on shutdown", got)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
