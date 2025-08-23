package plex

import (
	"encoding/json"
	"testing"

	plexclient "github.com/jrudio/go-plex-client"
)

func TestTimelineEntryQuotedIDs(t *testing.T) {
	payload := `{"NotificationContainer": {"TimelineEntry": [{"identifier":"abc","itemID":"123","metadataState":"ok","sectionID":"15","state":2,"title":"Test","type":1,"updatedAt":1600000000}], "size":1, "type":"playing"}}`

	var notif plexclient.WebsocketNotification
	if err := json.Unmarshal([]byte(payload), &notif); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(notif.NotificationContainer.TimelineEntry) != 1 {
		t.Fatalf("expected 1 timeline entry, got %d", len(notif.NotificationContainer.TimelineEntry))
	}

	te := notif.NotificationContainer.TimelineEntry[0]
	if te.ItemID != 123 {
		t.Fatalf("expected ItemID 123, got %d", te.ItemID)
	}
	if te.SectionID != 15 {
		t.Fatalf("expected SectionID 15, got %d", te.SectionID)
	}
}

func TestActivityNotificationQuotedUserID(t *testing.T) {
	payload := `{"NotificationContainer": {"ActivityNotification": [{"Activity": {"cancellable": false, "progress": 0, "subtitle": "", "title": "Test", "type": "test", "userID": "42", "uuid": "u1"}, "event": "activity", "uuid": "u1"}], "size":1, "type":"activity"}}`

	var notif plexclient.WebsocketNotification
	if err := json.Unmarshal([]byte(payload), &notif); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(notif.NotificationContainer.ActivityNotification) != 1 {
		t.Fatalf("expected 1 activity entry, got %d", len(notif.NotificationContainer.ActivityNotification))
	}

	act := notif.NotificationContainer.ActivityNotification[0]
	if act.Activity.UserID != 42 {
		t.Fatalf("expected Activity.UserID 42, got %d", act.Activity.UserID)
	}
}

func TestPlaySessionStateNotificationQuotedIDs(t *testing.T) {
	payload := `{"NotificationContainer": {"PlaySessionStateNotification": [{"guid":"g","key":"k","playQueueItemID":"7","ratingKey":"r","sessionKey":"s","state":"playing","url":"","viewOffset":"900","transcodeSession":""}], "size":1, "type":"playing"}}`

	var notif plexclient.WebsocketNotification
	if err := json.Unmarshal([]byte(payload), &notif); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(notif.NotificationContainer.PlaySessionStateNotification) != 1 {
		t.Fatalf("expected 1 play session entry, got %d", len(notif.NotificationContainer.PlaySessionStateNotification))
	}

	ps := notif.NotificationContainer.PlaySessionStateNotification[0]
	if ps.PlayQueueItemID != 7 {
		t.Fatalf("expected PlayQueueItemID 7, got %d", ps.PlayQueueItemID)
	}
	if ps.ViewOffset != 900 {
		t.Fatalf("expected ViewOffset 900, got %d", ps.ViewOffset)
	}
}
