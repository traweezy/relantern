package fetcher

import (
	"fmt"
	"net/url"
	"slices"

	"github.com/traweezy/relantern/internal/sources"
)

func EndpointFromSource(source sources.Endpoint) (Endpoint, error) {
	parsedURL, err := url.Parse(source.URL)
	if err != nil || parsedURL.Hostname() == "" {
		return Endpoint{}, fmt.Errorf("source endpoint %q has an invalid URL", source.ID)
	}
	return Endpoint{
		ID:                   source.ID,
		SourceID:             source.SourceID,
		URL:                  source.URL,
		AllowedHosts:         []string{parsedURL.Hostname()},
		ExpectedContentTypes: slices.Clone(source.ExpectedContentTypes),
		MaxBodyBytes:         source.MaxResponseBytes,
		ContentPolicy:        source.ContentLicense,
	}, nil
}
