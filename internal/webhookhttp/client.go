package webhookhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"

	"github.com/probehive/probehive/internal/webhook"
)

// Client is the Webhook delivery HTTP adapter. It is deliberately outside the webhook
// feature package so the feature retains no transport or persistence dependency.
type Client struct {
	client *http.Client
}

func NewClient(
	dialContext func(context.Context, string, string) (net.Conn, error),
	rootCAs *x509.CertPool,
) *Client {
	transport := &http.Transport{
		DialContext:            dialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: rootCAs},
		MaxResponseHeaderBytes: webhook.MaxResponseHeaderBytes,
		ForceAttemptHTTP2:      true,
	}
	return &Client{client: &http.Client{
		Transport: transport,
		Timeout:   webhook.DeliveryTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (client *Client) Do(
	ctx context.Context, request webhook.DeliveryRequest,
) (*webhook.DeliveryResponse, error) {
	if client == nil || client.client == nil {
		return nil, errors.New("Webhook delivery HTTP client is unavailable")
	}
	if len(request.Body) > webhook.MaxDeliveryBodyBytes {
		return nil, errors.New("Webhook delivery body exceeds the configured limit")
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx, http.MethodPost, request.URL, bytes.NewReader(request.Body),
	)
	if err != nil {
		return nil, err
	}
	for name, value := range request.Headers {
		httpRequest.Header.Set(name, value)
	}
	response, err := client.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return &webhook.DeliveryResponse{StatusCode: response.StatusCode}, nil
}
