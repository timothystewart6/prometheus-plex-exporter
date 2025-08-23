package plex

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jrplex "github.com/jrudio/go-plex-client"
)

func TestInviteFriendEndToEnd(t *testing.T) {
	// httptest server to accept the invite request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/api/v2/shared_servers") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("invitedEmail")) {
			t.Fatalf("request missing invitedEmail: %s", string(body))
		}

		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"1","ownerId":"2","invitedId":"3","serverId":"4","numLibraries":"0","invited":{"id":"5"},"sharingSettings":{"allowTuners":"0"},"libraries":[]}`))
	}))
	defer srv.Close()

	// create a client that points to the test server
	p, err := jrplex.New(srv.URL, "token")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	params := jrplex.InviteFriendParams{UsernameOrEmail: "a@b.c", MachineID: "m", Label: "", LibraryIDs: []int{1}}
	if err := p.InviteFriend(params); err != nil {
		t.Fatalf("InviteFriend failed: %v", err)
	}
}
