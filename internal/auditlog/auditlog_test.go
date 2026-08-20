package auditlog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewEmptyURLIsNoop(t *testing.T) {
	r := New("", "")
	if r.Enabled() {
		t.Error("empty URL should yield a disabled recorder")
	}
	r.Record(Event{Action: "x"}) // must not panic
	if got, err := r.List(context.Background(), 10); err != nil || got != nil {
		t.Errorf("noop List = (%v, %v), want (nil, nil)", got, err)
	}
}

func TestRecordPostsCanonicalBody(t *testing.T) {
	var mu sync.Mutex
	var got logRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/logs" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(body, &got)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	New(srv.URL, "").Record(Event{
		Action: "booking.approve", ActorID: "u1", ActorEmail: "a@b", TargetType: "booking", TargetID: "bk1", Message: "Staff approved a booking",
	})

	// Record is async; wait briefly for the POST to land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := got.App != ""
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.App != "facility-booking" || got.Message != "Staff approved a booking" {
		t.Errorf("body = %+v", got)
	}
	if got.Metadata["action"] != "booking.approve" || got.Metadata["actorEmail"] != "a@b" {
		t.Errorf("metadata = %v", got.Metadata)
	}
}

func TestListParsesAndReversesNewestFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("app") != "facility-booking" {
			t.Errorf("app filter = %q", r.URL.Query().Get("app"))
		}
		_, _ = w.Write([]byte(`{"items":[
			{"index":1,"timestamp":"2026-01-01T00:00:00Z","record":{"message":"first","metadata":{"action":"booking.approve","actorEmail":"a@b","targetId":"bk1","targetType":"booking"}}},
			{"index":2,"timestamp":"2026-01-02T00:00:00Z","record":{"message":"second","metadata":{"action":"booking.deny","actorEmail":"c@d","targetId":"bk2","targetType":"booking"}}}
		],"total":2}`))
	}))
	defer srv.Close()

	entries, err := New(srv.URL, "").List(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Index != 2 || entries[0].Action != "booking.deny" {
		t.Errorf("newest-first ordering wrong: %+v", entries[0])
	}
	if entries[1].ActorEmail != "a@b" {
		t.Errorf("entry[1] = %+v", entries[1])
	}
}
