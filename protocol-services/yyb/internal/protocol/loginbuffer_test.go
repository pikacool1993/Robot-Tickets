package protocol

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type renewalTransport func(*http.Request) (*http.Response, error)

func (f renewalTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRenewalDistinguishesProxyFailureFromCredentialRejection(t *testing.T) {
	failure := errors.New("proxy connection failed")
	client := NewLoginBufferClient(0)
	client.httpClient.Transport = renewalTransport(func(*http.Request) (*http.Response, error) { return nil, failure })
	_, err := client.RefreshCredentials(context.Background(), LoginBufferCredentials{RefreshToken: "test"})
	var rejected *CredentialRejection
	if err == nil || errors.As(err, &rejected) {
		t.Fatal("proxy failure was treated as expired credentials")
	}
	client.httpClient.Transport = renewalTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":401}`)), Header: make(http.Header)}, nil
	})
	_, err = client.RefreshCredentials(context.Background(), LoginBufferCredentials{RefreshToken: "test"})
	if !errors.As(err, &rejected) || rejected.Code != 401 {
		t.Fatalf("credential rejection not recognized: %v", err)
	}
}
