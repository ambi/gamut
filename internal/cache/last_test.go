package cache_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambi/gamut/internal/cache"
)

// TestStore_KeepsTheLastRunsReport checks that a run's report stays in the cache and can be read when merging shards.
func TestStore_KeepsTheLastRunsReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if _, err := cache.ReadLast(path); err == nil {
		t.Fatal("ReadLast of a missing cache succeeded")
	}
	s, err := cache.Open(path, "env")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadLast(path); err == nil || !strings.Contains(err.Error(), "no report") {
		t.Fatalf("ReadLast of a cache without a run's report: %v, want a no-report error", err)
	}
	s.PutLast([]byte(`{"go_module":"m"}`))
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	// Reopening keeps the previous report until the next run writes one.
	s, err = cache.Open(path, "env")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := cache.ReadLast(path)
	if err != nil || string(got) != `{"go_module":"m"}` {
		t.Errorf("ReadLast = %s, %v", got, err)
	}
}
