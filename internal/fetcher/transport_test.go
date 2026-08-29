package fetcher

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func TestSecureHTTPClientDisablesProxyAndRevalidatesRedirect(t *testing.T) {
	resolver := staticResolver{
		"source.example": {netip.MustParseAddr("93.184.216.34")},
		"other.example":  {netip.MustParseAddr("1.1.1.1")},
	}
	policy, err := NewPolicy(resolver, []string{"source.example", "other.example"}, nil)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	client, err := NewSecureHTTPClient(policy, &net.Dialer{}, DefaultNetworkLimits(), 5)
	if err != nil {
		t.Fatalf("NewSecureHTTPClient() error = %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || !transport.DisableCompression || transport.MaxConnsPerHost != 2 {
		t.Fatalf("unexpected secure transport = %#v", client.Transport)
	}
	ctx, err := withAllowedHosts(context.Background(), []string{"source.example"})
	if err != nil {
		t.Fatalf("withAllowedHosts() error = %v", err)
	}
	redirect, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://other.example/feed", nil)
	previous, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://source.example/feed", nil)
	if err := client.CheckRedirect(redirect, []*http.Request{previous}); err == nil {
		t.Fatal("CheckRedirect() accepted a cross-endpoint host")
	}
}

func TestSecureHTTPClientRejectsInvalidLimits(t *testing.T) {
	policy, err := NewPolicy(staticResolver{"source.example": {netip.MustParseAddr("93.184.216.34")}}, []string{"source.example"}, nil)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	tests := []struct {
		limits    NetworkLimits
		redirects int
	}{
		{limits: NetworkLimits{}, redirects: 5},
		{limits: DefaultNetworkLimits(), redirects: 6},
		{limits: DefaultNetworkLimits(), redirects: -1},
	}
	for _, test := range tests {
		if _, err := NewSecureHTTPClient(policy, &net.Dialer{Timeout: time.Second}, test.limits, test.redirects); err == nil {
			t.Fatalf("NewSecureHTTPClient(%+v, %d) succeeded", test.limits, test.redirects)
		}
	}
	if _, err := NewSecureHTTPClient(nil, nil, DefaultNetworkLimits(), 5); err == nil {
		t.Fatal("NewSecureHTTPClient() accepted a nil policy")
	}
}

func TestPinnedDialReportsInvalidAndFailedTargets(t *testing.T) {
	policy, err := NewPolicy(staticResolver{"source.example": {netip.MustParseAddr("93.184.216.34")}}, []string{"source.example"}, nil)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	dial := pinnedDialContext(policy, errorDialer{})
	for _, target := range []string{"missing-port", "source.example:0", "source.example:443"} {
		if _, err := dial(context.Background(), "tcp", target); err == nil {
			t.Fatalf("dial(%q) succeeded", target)
		}
	}
}

type errorDialer struct{}

func (errorDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("fixture dial failure")
}
