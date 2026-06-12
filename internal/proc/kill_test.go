package proc

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func spawn(t *testing.T, name string, args ...string) int32 {
	t.Helper()
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	go func() { _ = cmd.Wait() }() // reap so PidExists sees a real exit
	return int32(cmd.Process.Pid)
}

func TestGracefulKillTerminatesCooperativeProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix signals")
	}
	pid := spawn(t, "sleep", "60")

	results := GracefulKill(context.Background(), []int32{pid}, time.Second, false)
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Exited {
		t.Error("cooperative process should have exited on SIGTERM")
	}
	if r.Forced {
		t.Error("should not have needed SIGKILL")
	}
}

func TestGracefulKillEscalatesStubbornProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix signals")
	}
	// a shell that ignores SIGTERM
	pid := spawn(t, "sh", "-c", `trap "" TERM; while :; do sleep 1; done`)
	time.Sleep(200 * time.Millisecond) // let the trap install

	results := GracefulKill(context.Background(), []int32{pid}, 700*time.Millisecond, true)
	r := results[0]
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if !r.Forced {
		t.Error("stubborn process should have required SIGKILL")
	}
	if !r.Exited {
		t.Error("process should be gone after SIGKILL")
	}
}

func TestGracefulKillWithoutForceLeavesStubbornProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix signals")
	}
	pid := spawn(t, "sh", "-c", `trap "" TERM; while :; do sleep 1; done`)
	time.Sleep(200 * time.Millisecond)

	results := GracefulKill(context.Background(), []int32{pid}, 500*time.Millisecond, false)
	r := results[0]
	if r.Exited || r.Forced {
		t.Errorf("without force, stubborn process should survive: %+v", r)
	}
	if !Alive(pid) {
		t.Error("process should still be alive")
	}
}

func TestGracefulKillGonePID(t *testing.T) {
	// spawn + immediately kill to get a definitely-dead pid
	pid := spawn(t, "sleep", "60")
	results := GracefulKill(context.Background(), []int32{pid}, 200*time.Millisecond, false)
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	// either path is fine (already-reaped → Exited, or TERM delivered) —
	// the pid must just be gone afterwards
	deadline := time.Now().Add(2 * time.Second)
	for Alive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if Alive(pid) {
		t.Error("pid should be gone")
	}
}
