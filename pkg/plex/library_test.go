package plex

import "testing"

func TestIsLibraryDirectoryType(t *testing.T) {
	trues := []string{"movie", "show", "artist"}
	for _, v := range trues {
		if !isLibraryDirectoryType(v) {
			t.Fatalf("expected true for %s", v)
		}
	}
	if isLibraryDirectoryType("unknown") {
		t.Fatalf("expected false for unknown")
	}
}

func TestServerLibraryLookup(t *testing.T) {
	s := &Server{}
	l := &Library{ID: "l1", Name: "L1", Type: "movie", Server: s}
	s.libraries = []*Library{l}
	got := s.Library("l1")
	if got == nil || got.ID != "l1" {
		t.Fatalf("expected library l1 found: %v", got)
	}
	if s.Library("nope") != nil {
		t.Fatalf("expected nil for missing library")
	}
}
