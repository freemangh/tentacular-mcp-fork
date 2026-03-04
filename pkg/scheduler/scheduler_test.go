package scheduler

import (
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegisterAndDeregister(t *testing.T) {
	s := New(nil, testLogger()) // nil client is fine — we're not triggering
	s.Start()
	defer s.Stop()

	if err := s.Register("ns", "wf", "*/5 * * * *"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if s.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", s.Entries())
	}

	// Re-register same schedule — should be no-op
	if err := s.Register("ns", "wf", "*/5 * * * *"); err != nil {
		t.Fatalf("Re-register: %v", err)
	}
	if s.Entries() != 1 {
		t.Errorf("expected 1 entry after re-register, got %d", s.Entries())
	}

	// Update schedule
	if err := s.Register("ns", "wf", "0 * * * *"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if s.Entries() != 1 {
		t.Errorf("expected 1 entry after update, got %d", s.Entries())
	}

	s.Deregister("ns", "wf")
	if s.Entries() != 0 {
		t.Errorf("expected 0 entries after deregister, got %d", s.Entries())
	}
}

func TestRegisterInvalidSchedule(t *testing.T) {
	s := New(nil, testLogger())
	s.Start()
	defer s.Stop()

	err := s.Register("ns", "wf", "not-a-cron")
	if err == nil {
		t.Fatal("expected error for invalid cron schedule")
	}
}

func TestDeregisterNonexistent(t *testing.T) {
	s := New(nil, testLogger())
	s.Start()
	defer s.Stop()

	// Should not panic
	s.Deregister("ns", "nonexistent")
}

func TestMultipleWorkflows(t *testing.T) {
	s := New(nil, testLogger())
	s.Start()
	defer s.Stop()

	_ = s.Register("ns1", "wf1", "*/5 * * * *")
	_ = s.Register("ns1", "wf2", "0 * * * *")
	_ = s.Register("ns2", "wf1", "30 2 * * *")

	if s.Entries() != 3 {
		t.Errorf("expected 3 entries, got %d", s.Entries())
	}

	s.Deregister("ns1", "wf1")
	if s.Entries() != 2 {
		t.Errorf("expected 2 entries after deregister, got %d", s.Entries())
	}
}
