package fetcher

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

type NetworkLimits struct {
	ConnectTimeout         time.Duration
	ResponseHeaderTimeout  time.Duration
	TotalTimeout           time.Duration
	MaxResponseHeaderBytes int64
}

func DefaultNetworkLimits() NetworkLimits {
	return NetworkLimits{
		ConnectTimeout:         5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		TotalTimeout:           30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
}

func NewSecureHTTPClient(policy *Policy, dialer ContextDialer, limits NetworkLimits, maximumRedirects int) (*http.Client, error) {
	if policy == nil {
		return nil, fmt.Errorf("network policy is required")
	}
	if dialer == nil {
		dialer = &net.Dialer{Timeout: limits.ConnectTimeout, KeepAlive: 30 * time.Second}
	}
	if limits.ConnectTimeout <= 0 || limits.ResponseHeaderTimeout <= 0 || limits.TotalTimeout <= 0 || limits.MaxResponseHeaderBytes <= 0 {
		return nil, fmt.Errorf("network timeouts and header limit must be positive")
	}
	if maximumRedirects < 0 || maximumRedirects > 5 {
		return nil, fmt.Errorf("redirect limit must be between zero and five")
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            pinnedDialContext(policy, dialer),
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           40,
		MaxIdleConnsPerHost:    2,
		MaxConnsPerHost:        2,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    limits.ConnectTimeout,
		ResponseHeaderTimeout:  limits.ResponseHeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: limits.MaxResponseHeaderBytes,
		DisableCompression:     true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   limits.TotalTimeout,
		CheckRedirect: func(request *http.Request, previous []*http.Request) error {
			if len(previous) > maximumRedirects {
				return newFetchError(ErrorDestinationDenied, false, fmt.Errorf("redirect limit %d exceeded", maximumRedirects))
			}
			if _, err := policy.ValidateURL(request.Context(), request.URL); err != nil {
				return fmt.Errorf("validate redirect target: %w", err)
			}
			return nil
		},
	}, nil
}

func pinnedDialContext(policy *Policy, dialer ContextDialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, rawPort, err := net.SplitHostPort(address)
		if err != nil {
			return nil, newFetchError(ErrorInvalidURL, false, fmt.Errorf("split dial target: %w", err))
		}
		parsedPort, err := strconv.ParseUint(rawPort, 10, 16)
		if err != nil || parsedPort == 0 {
			return nil, newFetchError(ErrorInvalidURL, false, fmt.Errorf("invalid dial port %q", rawPort))
		}
		target, err := policy.ResolveDialTarget(ctx, host, uint16(parsedPort))
		if err != nil {
			return nil, err
		}
		var lastError error
		for _, destination := range target.Addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(destination.String(), rawPort))
			if dialErr == nil {
				return connection, nil
			}
			lastError = dialErr
		}
		return nil, newFetchError(ErrorTransport, true, fmt.Errorf("dial validated target %q: %w", target.Host, lastError))
	}
}
