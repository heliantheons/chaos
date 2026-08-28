package chaos

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gin-gonic/gin"
)

type recordingMailEventConsumer struct {
	event cloudevents.Event
}

func (c *recordingMailEventConsumer) HandleEvent(_ context.Context, event cloudevents.Event) error {
	c.event = event
	return nil
}

func TestMailEventEndpointAcceptsSignedCloudEventWithoutHTTPAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const eventType = "com.heliantheon.chaos.mail.delivery.requested.v1"
	const eventSource = "urn:heliantheon:chaos:mail"
	const deliveryID = "5dd253af-178b-480d-a1dc-ab42cc5e4d6d"

	event := cloudevents.NewEvent()
	event.SetID(deliveryID)
	event.SetType(eventType)
	event.SetSource(eventSource)
	event.SetSubject(deliveryID)
	if err := event.SetData(cloudevents.ApplicationJSON, map[string]string{"signed": "payload"}); err != nil {
		t.Fatalf("SetData() error = %v", err)
	}
	request, err := cloudevents.NewHTTPRequestFromEvent(t.Context(), "http://chaos/internal/events/mail-delivery", event)
	if err != nil {
		t.Fatalf("NewHTTPRequestFromEvent() error = %v", err)
	}

	consumer := &recordingMailEventConsumer{}
	handler := &Handler{
		mailService: consumer,
		logger:      slog.New(slog.DiscardHandler),
	}
	router := gin.New()
	router.POST("/internal/events/mail-delivery", handler.ConsumeMailEvent)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if consumer.event.ID() != deliveryID {
		t.Fatalf("event ID = %q", consumer.event.ID())
	}
}
