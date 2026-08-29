package fetcher

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (resolver staticResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	addresses, exists := resolver[host]
	if !exists {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

func TestPolicyRejectsUnsafeDestinations(t *testing.T) {
	resolver := staticResolver{
		"public.example":  {netip.MustParseAddr("93.184.216.34")},
		"private.example": {netip.MustParseAddr("10.0.0.5")},
		"mixed.example":   {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")},
	}
	policy, err := NewPolicy(resolver, []string{"public.example", "private.example", "mixed.example", "127.0.0.1"}, []FixtureTarget{{Host: "127.0.0.1", Port: 8090}})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	tests := []struct {
		name    string
		rawURL  string
		wantErr ErrorCode
	}{
		{name: "public HTTPS", rawURL: "https://public.example/feed", wantErr: ""},
		{name: "private DNS", rawURL: "https://private.example/feed", wantErr: ErrorDestinationDenied},
		{name: "mixed DNS", rawURL: "https://mixed.example/feed", wantErr: ErrorDestinationDenied},
		{name: "private literal", rawURL: "https://127.0.0.1/feed", wantErr: ErrorDestinationDenied},
		{name: "userinfo", rawURL: "https://owner@public.example/feed", wantErr: ErrorInvalidURL},
		{name: "fragment", rawURL: "https://public.example/feed#part", wantErr: ErrorInvalidURL},
		{name: "HTTP", rawURL: "http://public.example/feed", wantErr: ErrorDestinationDenied},
		{name: "alternate port", rawURL: "https://public.example:8443/feed", wantErr: ErrorDestinationDenied},
		{name: "explicit fixture", rawURL: "http://127.0.0.1:8090/feed", wantErr: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, parseErr := url.Parse(test.rawURL)
			if parseErr != nil {
				t.Fatalf("url.Parse() error = %v", parseErr)
			}
			_, validationErr := policy.ValidateURL(context.Background(), target)
			if test.wantErr == "" {
				if validationErr != nil {
					t.Fatalf("ValidateURL() error = %v", validationErr)
				}
				return
			}
			var fetchError *FetchError
			if !errors.As(validationErr, &fetchError) || fetchError.Code != test.wantErr {
				t.Fatalf("ValidateURL() error = %v, want code %q", validationErr, test.wantErr)
			}
		})
	}
}

func TestPolicyAppliesEndpointSpecificHostAllowlist(t *testing.T) {
	resolver := staticResolver{
		"one.example": {netip.MustParseAddr("93.184.216.34")},
		"two.example": {netip.MustParseAddr("1.1.1.1")},
	}
	policy, err := NewPolicy(resolver, []string{"one.example", "two.example"}, nil)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	ctx, err := withAllowedHosts(context.Background(), []string{"one.example"})
	if err != nil {
		t.Fatalf("withAllowedHosts() error = %v", err)
	}
	target, _ := url.Parse("https://two.example/feed")
	_, err = policy.ValidateURL(ctx, target)
	var fetchError *FetchError
	if !errors.As(err, &fetchError) || fetchError.Code != ErrorDestinationDenied {
		t.Fatalf("ValidateURL() error = %v, want destination denied", err)
	}
}

type recordingDialer struct {
	address string
}

func (dialer *recordingDialer) DialContext(_ context.Context, _ string, address string) (net.Conn, error) {
	dialer.address = address
	client, server := net.Pipe()
	_ = server.Close()
	return client, nil
}

func TestPinnedDialUsesValidatedAddress(t *testing.T) {
	resolver := staticResolver{"public.example": {netip.MustParseAddr("93.184.216.34")}}
	policy, err := NewPolicy(resolver, []string{"public.example"}, nil)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	dialer := &recordingDialer{}
	dial := pinnedDialContext(policy, dialer)
	connection, err := dial(context.Background(), "tcp", "public.example:443")
	if err != nil {
		t.Fatalf("pinned dial error = %v", err)
	}
	_ = connection.Close()
	if dialer.address != "93.184.216.34:443" {
		t.Fatalf("dial address = %q, want validated IP", dialer.address)
	}
}
