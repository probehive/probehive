package webhookhttp

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/probehive/probehive/internal/webhook"
)

func TestNewClientPinsDeliveryTransportBounds(t *testing.T) {
	dialContext := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("not called")
	}
	client := NewClient(dialContext, nil)
	if client.client.Timeout != webhook.DeliveryTimeout {
		t.Fatalf("client timeout = %v", client.client.Timeout)
	}
	transport, ok := client.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.client.Transport)
	}
	if transport.MaxResponseHeaderBytes != webhook.MaxResponseHeaderBytes ||
		transport.TLSClientConfig == nil ||
		transport.TLSClientConfig.MinVersion != tls.VersionTLS12 ||
		!transport.ForceAttemptHTTP2 {
		t.Fatalf("transport bounds = %#v", transport)
	}
	if err := client.client.CheckRedirect(
		&http.Request{}, []*http.Request{{}},
	); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v", err)
	}
}

func TestClientSendsPostAndClosesUnconsumedResponse(t *testing.T) {
	responseBody := &trackingBody{reader: strings.NewReader("provider text")}
	var gotMethod, gotURL, gotHeader, gotBody string
	client := &Client{client: &http.Client{Transport: roundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			gotMethod = request.Method
			gotURL = request.URL.String()
			gotHeader = request.Header.Get("ProbeHive-Signature")
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			gotBody = string(body)
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Header:     make(http.Header),
				Body:       responseBody,
				Request:    request,
			}, nil
		},
	)}}

	response, err := client.Do(t.Context(), webhook.DeliveryRequest{
		URL:     "https://hooks.example.test/events",
		Body:    []byte("{\"schemaVersion\":\"v1\"}"),
		Headers: map[string]string{"ProbeHive-Signature": "signature"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted ||
		gotMethod != http.MethodPost ||
		gotURL != "https://hooks.example.test/events" ||
		gotHeader != "signature" ||
		gotBody != "{\"schemaVersion\":\"v1\"}" {
		t.Fatalf(
			"response/request = %#v, %s %s %q %q",
			response, gotMethod, gotURL, gotHeader, gotBody,
		)
	}
	if responseBody.read || !responseBody.closed {
		t.Fatalf(
			"response body read/closed = %v/%v",
			responseBody.read, responseBody.closed,
		)
	}
}

func TestClientRejectsOversizedBodyBeforeTransport(t *testing.T) {
	called := false
	client := &Client{client: &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected call")
		},
	)}}
	_, err := client.Do(t.Context(), webhook.DeliveryRequest{
		URL:  "https://hooks.example.test/events",
		Body: make([]byte, webhook.MaxDeliveryBodyBytes+1),
	})
	if err == nil || called {
		t.Fatalf("oversized Do() error/called = %v/%v", err, called)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return function(request)
}

type trackingBody struct {
	reader io.Reader
	read   bool
	closed bool
}

func (body *trackingBody) Read(buffer []byte) (int, error) {
	body.read = true
	return body.reader.Read(buffer)
}

func (body *trackingBody) Close() error {
	body.closed = true
	return nil
}
