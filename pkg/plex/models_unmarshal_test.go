package plex

import (
	"encoding/json"
	"testing"

	plexclient "github.com/jrudio/go-plex-client"
)

func TestMetadataQuotedLibrarySectionIDAndViewCount(t *testing.T) {
	payload := `{"librarySectionID":"15","viewCount":"7"}`

	var m plexclient.Metadata
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if m.LibrarySectionID != 15 {
		t.Fatalf("expected LibrarySectionID 15, got %d", m.LibrarySectionID)
	}
	if m.ViewCount != 7 {
		t.Fatalf("expected ViewCount 7, got %d", m.ViewCount)
	}
}

func TestTaggedDataQuotedID(t *testing.T) {
	payload := `{"tag":"g","filter":"f","id":"99"}`

	var td plexclient.TaggedData
	if err := json.Unmarshal([]byte(payload), &td); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if td.ID != 99 {
		t.Fatalf("expected TaggedData.ID 99, got %d", td.ID)
	}
}

func TestPartAndStreamQuotedIDs(t *testing.T) {
	var s plexclient.Stream
	// Unmarshal as Stream object
	if err := json.Unmarshal([]byte(`{"id":"33"}`), &s); err != nil {
		t.Fatalf("unmarshal stream failed: %v", err)
	}
	if s.ID != 33 {
		t.Fatalf("expected Stream.ID 33, got %d", s.ID)
	}

	// Unmarshal Part with quoted id
	var p plexclient.Part
	if err := json.Unmarshal([]byte(`{"id":"11","Stream":[{"id":"33"}]}`), &p); err != nil {
		t.Fatalf("unmarshal part failed: %v", err)
	}
	if p.ID != 11 {
		t.Fatalf("expected Part.ID 11, got %d", p.ID)
	}
	if len(p.Stream) != 1 || p.Stream[0].ID != 33 {
		t.Fatalf("expected nested Stream.ID 33, got %+v", p.Stream)
	}
}

func TestRatingAndGenreQuotedValues(t *testing.T) {
	var r plexclient.Rating
	if err := json.Unmarshal([]byte(`{"image":"i","type":"t","value":"5"}`), &r); err != nil {
		t.Fatalf("unmarshal rating failed: %v", err)
	}
	if r.Value != 5 {
		t.Fatalf("expected Rating.Value 5, got %d", r.Value)
	}

	var g plexclient.Genre
	if err := json.Unmarshal([]byte(`{"filter":"f","id":"7","tag":"tt"}`), &g); err != nil {
		t.Fatalf("unmarshal genre failed: %v", err)
	}
	if g.ID != 7 {
		t.Fatalf("expected Genre.ID 7, got %d", g.ID)
	}
}
