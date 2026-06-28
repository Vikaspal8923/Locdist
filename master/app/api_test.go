package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Vikaspal8923/Locdist/master/discovery"
	"github.com/Vikaspal8923/Locdist/master/jobs"
	"github.com/Vikaspal8923/Locdist/master/workers"
)

func TestSessionTokenMiddleware(t *testing.T) {
	server := &Server{token: "secret"}
	handler := server.authenticate(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) }))
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/state", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", unauthorized.Code)
	}
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/state", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer secret")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("status = %d", authorized.Code)
	}
}

func TestSecureServerRejectsNonLoopbackHost(t *testing.T) {
	controller := NewController(discovery.NewRegistry(), nil)
	if _, err := NewSecureServer("0.0.0.0", "0", "secret", controller); err == nil {
		t.Fatal("expected non-loopback host to fail")
	}
}

func TestUnifiedStateAndEvents(t *testing.T) {
	controller := NewController(discovery.NewRegistry(), nil)
	controller.AttachBackend(Backend{Jobs: jobs.New(), Workers: workers.New(), ResultsRoot: t.TempDir()})
	state := controller.State()
	if state.Master["version"] != Version || state.Job != nil {
		t.Fatalf("unexpected state: %+v", state)
	}
	events, unsubscribe := controller.Events().Subscribe()
	defer unsubscribe()
	controller.Events().Publish("job.prepared", map[string]string{"job_id": "job-1"})
	select {
	case event := <-events:
		if event.Type != "job.prepared" {
			t.Fatalf("event = %s", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("event was not delivered")
	}
}
