package convert

import (
	"cmp"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Fetcher reads URLs. Zero MaxBytes means domain.MaxBodyBytes.
type Fetcher struct {
	client   *http.Client
	MaxBytes int64
}

// NewFetcher is a fetcher over the engine's client, which is what makes everything the user configured
// about how this app reaches the network apply to a URL imported here too.
func NewFetcher(client *http.Client) *Fetcher {
	return &Fetcher{client: client}
}

// Fetch reads a URL whole. Every failure before the document arrives is one refusal naming the address.
func (f *Fetcher) Fetch(ctx context.Context, address string) ([]byte, error) {
	fail := func(reason string) error {
		return domain.Refuse(domain.CodeURLUnreachable, domain.ErrNotAllowed,
			domain.Args{"url": address, "reason": reason})
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fail(err.Error())
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, fail(reasonOf(err))
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fail("HTTP " + strconv.Itoa(response.StatusCode))
	}

	limit := cmp.Or(f.MaxBytes, domain.MaxBodyBytes)
	// One byte past the limit tells "ends exactly at the limit" from "was cut": a truncated document
	// is refused, not imported.
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fail(err.Error())
	}
	if int64(len(data)) > limit {
		return nil, platform.TooLarge(address, limit)
	}
	return data, nil
}

// reasonOf is the error without the address in it: the sentence already names the URL, and the
// client's own wrapper says it a second time.
func reasonOf(err error) string {
	var failed *url.Error
	if errors.As(err, &failed) {
		return failed.Err.Error()
	}
	return err.Error()
}
