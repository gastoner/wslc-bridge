package execstore

import (
	"errors"
	"testing"
)

func TestExecLifecycleAndSingleStart(t *testing.T) {
	store := New()
	created, err := store.Create("container-id", []string{"sh", "-c", "exit 42"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.State != Created {
		t.Fatalf("unexpected initial record: %#v", created)
	}
	started, err := store.Start(created.ID)
	if err != nil || started.State != Running {
		t.Fatalf("Start() = (%#v, %v)", started, err)
	}
	if _, err := store.Start(created.ID); err == nil {
		t.Fatal("a running exec was started a second time")
	}
	completed, err := store.Complete(created.ID, 42)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != Exited || completed.ExitCode == nil || *completed.ExitCode != 42 {
		t.Fatalf("unexpected completed record: %#v", completed)
	}
}

func TestFailedExecReportsANonZeroExitCode(t *testing.T) {
	store := New()
	created, err := store.Create("container-id", []string{"sh", "-c", "exit 0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(created.ID); err != nil {
		t.Fatal(err)
	}
	store.Fail(created.ID, errors.New("guest transport died"))
	record, ok := store.Get(created.ID)
	if !ok {
		t.Fatal("the failed exec record disappeared")
	}
	if record.State != Failed {
		t.Fatalf("state = %q, want failed", record.State)
	}
	if record.ExitCode == nil || *record.ExitCode != FailedExitCode {
		t.Fatalf("a failed exec must not look like exit code 0: %#v", record.ExitCode)
	}
	if record.Failure != "guest transport died" {
		t.Fatalf("failure cause = %q", record.Failure)
	}
}
