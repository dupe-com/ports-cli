// Package proc terminates processes with a graceful-then-forceful strategy:
// SIGTERM first, a grace window for clean shutdown, then (optionally) SIGKILL
// for anything still alive.
package proc

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// DefaultGrace is how long a process gets to exit after SIGTERM before it is
// considered a survivor.
const DefaultGrace = 1500 * time.Millisecond

// Result reports the outcome for one pid.
type Result struct {
	PID    int32
	Exited bool  // gone by the time we finished
	Forced bool  // needed SIGKILL
	Err    error // signal delivery failed (permissions, etc.)
}

// Alive reports whether pid still exists.
func Alive(pid int32) bool {
	ok, err := process.PidExists(pid)
	return err == nil && ok
}

// GracefulKill SIGTERMs every pid, waits up to grace for them to exit, and —
// when force is true — SIGKILLs the survivors. A cancelled ctx stops the
// waiting early (survivors are then reported as not exited).
func GracefulKill(ctx context.Context, pids []int32, grace time.Duration, force bool) []Result {
	if grace <= 0 {
		grace = DefaultGrace
	}
	results := make([]Result, len(pids))
	procs := make([]*process.Process, len(pids))

	for i, pid := range pids {
		results[i] = Result{PID: pid}
		p, err := process.NewProcess(pid)
		if err != nil {
			// Already gone.
			results[i].Exited = true
			continue
		}
		procs[i] = p
		if err := p.Terminate(); err != nil {
			results[i].Err = err
		}
	}

	waitExit(ctx, procs, results, grace)

	if force {
		for i, p := range procs {
			if p == nil || results[i].Exited || results[i].Err != nil {
				continue
			}
			if err := p.Kill(); err != nil {
				results[i].Err = err
				continue
			}
			results[i].Forced = true
		}
		// Give SIGKILL a short beat to take effect, then re-check.
		waitExit(ctx, procs, results, 500*time.Millisecond)
	}
	return results
}

// waitExit polls until every signalled process is gone, the window elapses,
// or ctx is cancelled. Marks newly-exited pids in results.
func waitExit(ctx context.Context, procs []*process.Process, results []Result, window time.Duration) {
	deadline := time.Now().Add(window)
	for {
		pending := false
		for i, p := range procs {
			if p == nil || results[i].Exited {
				continue
			}
			if !Alive(p.Pid) {
				results[i].Exited = true
				continue
			}
			pending = true
		}
		if !pending || time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
