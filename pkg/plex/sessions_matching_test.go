package plex

import (
	"context"
	"testing"
	"time"

	ttPlex "github.com/timothystewart6/go-plex-client"
)

func TestSetAndTrySetTranscodeType_MatchingVariants(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSessions(ctx, &Server{})

	// Exact map key
	m1 := ttPlex.Metadata{SessionKey: "sess-1"}
	s.mtx.Lock()
	s.sessions["key1"] = session{session: m1}
	s.mtx.Unlock()

	s.SetTranscodeType("key1", "video")
	s.mtx.Lock()
	if s.sessions["key1"].transcodeType != "video" {
		t.Fatalf("expected transcodeType=video for key1, got %q", s.sessions["key1"].transcodeType)
	}
	s.mtx.Unlock()

	// Inner Metadata.SessionKey match
	s.SetTranscodeType("sess-1", "audio")
	s.mtx.Lock()
	if s.sessions["key1"].transcodeType != "audio" {
		t.Fatalf("expected transcodeType=audio for key1 after inner-key set, got %q", s.sessions["key1"].transcodeType)
	}
	s.mtx.Unlock()

	// Substring match: store under a different map key with SessionKey set
	m2 := ttPlex.Metadata{SessionKey: "abc"}
	s.mtx.Lock()
	s.sessions["k-abc"] = session{session: m2}
	s.mtx.Unlock()

	s.SetTranscodeType("prefix-abc-suffix", "both")
	s.mtx.Lock()
	if s.sessions["k-abc"].transcodeType != "both" {
		t.Fatalf("expected transcodeType=both for k-abc via substring match, got %q", s.sessions["k-abc"].transcodeType)
	}
	s.mtx.Unlock()
}

func TestTrySetTranscodeType_HeuristicFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSessions(ctx, &Server{})

	// Create a session that has Media.Part[0].Decision == "transcode"
	media := ttPlex.Media{Part: []ttPlex.Part{{Decision: "transcode"}}}
	md := ttPlex.Metadata{Media: []ttPlex.Media{media}}

	s.mtx.Lock()
	s.sessions["other"] = session{session: md}
	s.mtx.Unlock()

	applied := s.TrySetTranscodeType("nonmatching", "video")
	if !applied {
		t.Fatalf("expected heuristic fallback to apply transcode type")
	}

	s.mtx.Lock()
	if s.sessions["other"].transcodeType != "video" {
		t.Fatalf("expected transcodeType=video applied by heuristic, got %q", s.sessions["other"].transcodeType)
	}
	s.mtx.Unlock()
}

func TestSetAndTrySetSubtitleAction_MatchingVariants(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSessions(ctx, &Server{})

	// Exact map key
	m1 := ttPlex.Metadata{SessionKey: "sess-sub"}
	s.mtx.Lock()
	s.sessions["subkey"] = session{session: m1}
	s.mtx.Unlock()

	s.SetSubtitleAction("subkey", "copy")
	s.mtx.Lock()
	if s.sessions["subkey"].subtitleAction != "copy" {
		t.Fatalf("expected subtitleAction=copy for subkey, got %q", s.sessions["subkey"].subtitleAction)
	}
	s.mtx.Unlock()

	// Inner key match
	ok := s.TrySetSubtitleAction("sess-sub", "burn")
	if !ok {
		t.Fatalf("expected TrySetSubtitleAction to return true for inner-key match")
	}
	s.mtx.Lock()
	if s.sessions["subkey"].subtitleAction != "burn" {
		t.Fatalf("expected subtitleAction=burn for subkey after inner-key TrySet, got %q", s.sessions["subkey"].subtitleAction)
	}
	s.mtx.Unlock()

	// Substring match
	m2 := ttPlex.Metadata{SessionKey: "def"}
	s.mtx.Lock()
	s.sessions["x-def-y"] = session{session: m2}
	s.mtx.Unlock()

	ok2 := s.TrySetSubtitleAction("prefix-def", "copy")
	if !ok2 {
		t.Fatalf("expected TrySetSubtitleAction to return true for substring match")
	}
	s.mtx.Lock()
	if s.sessions["x-def-y"].subtitleAction != "copy" {
		t.Fatalf("expected subtitleAction=copy for x-def-y after substring TrySet, got %q", s.sessions["x-def-y"].subtitleAction)
	}
	s.mtx.Unlock()

	// Ensure prune goroutine is running and doesn't race
	time.Sleep(10 * time.Millisecond)
}

func TestSetSubtitleAction_InnerSubstringAndFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSessions(ctx, &Server{})

	// Inner-key match
	inner := ttPlex.Metadata{SessionKey: "inner-1"}
	s.mtx.Lock()
	s.sessions["kinner"] = session{session: inner}
	s.mtx.Unlock()

	s.SetSubtitleAction("inner-1", "copy")
	s.mtx.Lock()
	if s.sessions["kinner"].subtitleAction != "copy" {
		t.Fatalf("expected subtitleAction=copy for kinner after inner-key Set, got %q", s.sessions["kinner"].subtitleAction)
	}
	s.mtx.Unlock()

	// Substring match
	sub := ttPlex.Metadata{SessionKey: "subid"}
	s.mtx.Lock()
	s.sessions["pre-subid-post"] = session{session: sub}
	s.mtx.Unlock()

	s.SetSubtitleAction("prefix-subid", "burn")
	s.mtx.Lock()
	if s.sessions["pre-subid-post"].subtitleAction != "burn" {
		t.Fatalf("expected subtitleAction=burn for pre-subid-post after substring Set, got %q", s.sessions["pre-subid-post"].subtitleAction)
	}
	s.mtx.Unlock()

	// Fallback: create new entry under provided key
	s.SetSubtitleAction("new-session-key", "copy")
	s.mtx.Lock()
	if s.sessions["new-session-key"].subtitleAction != "copy" {
		t.Fatalf("expected subtitleAction=copy for newly created session entry, got %q", s.sessions["new-session-key"].subtitleAction)
	}
	s.mtx.Unlock()
}
