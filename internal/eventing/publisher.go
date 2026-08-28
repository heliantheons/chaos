// Package eventing contains Chaos' HTTP boundary to Knative Eventing.
package eventing

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

const maxResponseBytes = 8 * 1024

// Publisher sends CloudEvents to a fixed Knative Broker ingress endpoint.
type Publisher struct {
	target string
	client *http.Client
}

func NewPublisher(target string) (*Publisher, error) {
	parsed, err := url.ParseRequestURI(target)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("eventing: broker URL must be an absolute HTTP URL")
	}
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("eventing: default HTTP transport has an unexpected type")
	}
	transport := baseTransport.Clone()
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 10 * time.Second
	return &Publisher{
		target: parsed.String(),
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Publish returns only after the Broker ingress acknowledges the HTTP request.
// RabbitMQ durability and downstream retries are owned by the Broker/Trigger.
func (p *Publisher) Publish(ctx context.Context, event cloudevents.Event) error {
	request, err := cloudevents.NewHTTPRequestFromEvent(ctx, p.target, event)
	if err != nil {
		return fmt.Errorf("eventing: create CloudEvent request: %w", err)
	}
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("eventing: publish CloudEvent: %w", err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes)); err != nil {
		return fmt.Errorf("eventing: read Broker response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("eventing: broker returned HTTP %d", response.StatusCode)
	}
	return nil
}
