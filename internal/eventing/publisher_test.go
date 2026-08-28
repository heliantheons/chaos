package eventing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

func TestPublisherSendsCloudEventToBroker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		event, err := cloudevents.NewEventFromHTTPRequest(request)
		if err != nil {
			t.Errorf("NewEventFromHTTPRequest() error = %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if event.ID() != "event-1" || event.Type() != "example.event.v1" {
			t.Errorf("event = %#v", event)
		}
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	publisher, err := NewPublisher(server.URL + "/v1/events")
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	event := cloudevents.NewEvent()
	event.SetID("event-1")
	event.SetSource("urn:test")
	event.SetType("example.event.v1")
	if err := event.SetData(cloudevents.ApplicationJSON, map[string]string{"ok": "true"}); err != nil {
		t.Fatalf("SetData() error = %v", err)
	}
	if err := publisher.Publish(t.Context(), event); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

func TestPublisherRejectsBrokerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	publisher, err := NewPublisher(server.URL)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	event := cloudevents.NewEvent()
	event.SetID("event-1")
	event.SetSource("urn:test")
	event.SetType("example.event.v1")
	if err := publisher.Publish(t.Context(), event); err == nil {
		t.Fatal("Publish() error = nil")
	}
}
