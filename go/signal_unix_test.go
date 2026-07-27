//go:build unix

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestInterruptReachesChildren guards the distinction between catching and
// ignoring os.Interrupt in run: an ignored disposition is inherited across
// exec, and a child started under it would not respond to the interrupt
// this test sends. The child is this test binary re-run in a mode that
// blocks until a signal kills it.
func TestInterruptReachesChildren(t *testing.T) {
	if os.Getenv("GORC_TEST_BLOCK") == "1" {
		time.Sleep(time.Hour)
	}
	swap(t, &os.Args, []string{"go"})
	swap(t, &testHookExec, func() error { return nil })
	if err := run(t.Context()); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, exe,
		"-test.run", "TestInterruptReachesChildren")
	cmd.Env = append(os.Environ(), "GORC_TEST_BLOCK=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	status, _ := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if status.Signal() != syscall.SIGINT {
		t.Errorf("child = %v; want death by SIGINT", err)
	}
}
