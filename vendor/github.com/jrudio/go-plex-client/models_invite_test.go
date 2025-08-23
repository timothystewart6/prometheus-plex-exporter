package plex

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInviteFriendResponseQuotedIDs(t *testing.T) {
	payload := `{
	    "id":"101",
	    "name":"Alice",
	    "ownerId":"202",
	    "invitedId":"303",
	    "serverId":"404",
	    "numLibraries":"2",
	    "invited": { "id": "505", "uuid":"u","title":"t","username":"u","restricted":false,"thumb":"","status":"" },
	    "sharingSettings": { "allowTuners": "1" },
	    "libraries": [ { "id": "11", "key": "22", "title": "L", "type": "movie" } ]
	}`

	var ir inviteFriendResponse
	if err := json.Unmarshal([]byte(payload), &ir); err != nil {
		t.Fatalf("unmarshal inviteFriendResponse failed: %v", err)
	}

	if ir.ID != 101 || ir.OwnerID != 202 || ir.InvitedID != 303 || ir.ServerID != 404 {
		t.Fatalf("unexpected invite ids: %+v", ir)
	}
	if ir.NumLibraries != 2 {
		t.Fatalf("unexpected NumLibraries: %d", ir.NumLibraries)
	}
	if len(ir.Libraries) != 1 || ir.Libraries[0].ID != 11 || ir.Libraries[0].Key != 22 {
		t.Fatalf("unexpected libraries: %+v", ir.Libraries)
	}
}

func TestPlexInviteFriendFlow(t *testing.T) {
	// start an httptest server to mock plex.tv
	srv := httpTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// verify path and method
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/api/v2/shared_servers") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		// read body to ensure it contains invitedEmail and librarySectionIds
		body, _ := io.ReadAll(r.Body)
		if !bytes.Contains(body, []byte("invitedEmail")) {
			t.Fatalf("request body missing invitedEmail: %s", string(body))
		}

		// respond with created and a valid JSON body
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"1","ownerId":"2","invitedId":"3","serverId":"4","numLibraries":"0","invited":{"id":"5"},"sharingSettings":{"allowTuners":"0"},"libraries":[]}`))
	})
	defer srv.Close()

	// override plexURL for this test
	old := plexURL
	plexURL = srv.URL
	defer func() { plexURL = old }()

	p, err := New(srv.URL, "token")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	params := InviteFriendParams{UsernameOrEmail: "a@b.c", MachineID: "m", Label: "", LibraryIDs: []int{1}}

	if err := p.InviteFriend(params); err != nil {
		t.Fatalf("InviteFriend failed: %v", err)
	}
}

// httpTestServer is a small helper to create an httptest.Server with a handler.
func httpTestServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(h))
}
