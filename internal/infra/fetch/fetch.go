// Package fetch reads a document from a URL — a specification or a collection somebody published.
// It goes out through the engine's own client, so the proxy, the certificates and the timeout the
// user set are the ones it travels with. It is here and not in a usecase because reading a URL is
// touching the outside world, and a scenario may not do that.
package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"schemo/internal/domain"
)

// DefaultMaxBytes is what a document is read under when nothing says otherwise. It is the same
// ceiling a body and a response are read under: a document of a few megabytes is a document, and a
// hundred is a mistake.
const DefaultMaxBytes int64 = 8 << 20

// Fetcher reads URLs. Zero MaxBytes means the default.
type Fetcher struct {
	client   *http.Client
	MaxBytes int64
}

// New is a fetcher over the engine's client, which is what makes everything the user configured
// about how this app reaches the network apply to a URL imported here too.
func New(client *http.Client) *Fetcher {
	return &Fetcher{client: client, MaxBytes: DefaultMaxBytes}
}

// Fetch reads a URL whole. Everything that can go wrong before the document is in hand — a name
// that does not resolve, a refused connection, a timeout, an answer that is not a success — is one
// refusal: the sentence names the address, and which of them it was is not something the person
// acts on differently.
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

	limit := f.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	// The extra byte past the limit tells "ends exactly at the limit" apart from "was cut there",
	// the way the file reader does it: a truncated document is a document nobody can trust.
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fail(err.Error())
	}
	if int64(len(data)) > limit {
		return nil, domain.Refuse(domain.CodeFileTooLarge, domain.ErrNotAllowed, domain.Args{
			"path":  address,
			"limit": strconv.Itoa(int(limit >> 20)),
		})
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
