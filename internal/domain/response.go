package domain

import (
	"cmp"
	"slices"
)

type HeaderPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func PairsFromMap(headers map[string]string) []HeaderPair {
	out := make([]HeaderPair, 0, len(headers))
	for name, value := range headers {
		out = append(out, HeaderPair{Name: name, Value: value})
	}
	slices.SortFunc(out, func(a, b HeaderPair) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

type Response struct {
	DNSUs         *int64        `json:"dnsUs,omitempty"`
	ConnectUs     *int64        `json:"connectUs,omitempty"`
	TLSUs         *int64        `json:"tlsUs,omitempty"`
	WaitUs        *int64        `json:"waitUs,omitempty"`
	DownloadUs    *int64        `json:"downloadUs,omitempty"`
	StatusText    string        `json:"statusText"`
	Body          string        `json:"body"`
	FinalURL      string        `json:"finalUrl,omitempty"`
	ContentType   string        `json:"contentType"`
	Error         string        `json:"error,omitempty"`
	Headers       []HeaderPair  `json:"headers"`
	SentHeaders   []HeaderPair  `json:"sentHeaders,omitempty"`
	CookieWrites  []CookieWrite `json:"cookieWrites,omitempty"`
	Status        int           `json:"status"`
	DurationUs    int64         `json:"durationUs"`
	Cancelled     bool          `json:"cancelled,omitempty"`
	BodyTruncated bool          `json:"bodyTruncated,omitempty"`
}
