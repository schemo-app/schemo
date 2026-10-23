// Package har writes HTTP Archive (HAR 1.2) files.
package har

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"schemo/internal/domain"
)

const Version = "1.2"

type Entry struct {
	Record       domain.Record
	RequestBody  string
	ResponseBody string
}

func Export(app, version string, entries []Entry) ([]byte, error) {
	doc := logFile{
		Log: log{
			Version: Version,
			Creator: creator{Name: app, Version: version},
			Entries: make([]entry, 0, len(entries)),
		},
	}

	for _, one := range entries {
		doc.Log.Entries = append(doc.Log.Entries, entryOf(one))
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

type logFile struct {
	Log log `json:"log"`
}

type log struct {
	Version string  `json:"version"`
	Creator creator `json:"creator"`
	Entries []entry `json:"entries"`
}

type creator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type entry struct {
	StartedDateTime string   `json:"startedDateTime"`
	Time            float64  `json:"time"`
	Request         request  `json:"request"`
	Response        response `json:"response"`
	Cache           struct{} `json:"cache"`
	Timings         timings  `json:"timings"`
	Tab             string   `json:"_tab,omitempty"`
}

type request struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	Cookies     []cookie     `json:"cookies"`
	Headers     []headerPair `json:"headers"`
	QueryString []nameValue  `json:"queryString"`
	PostData    *postData    `json:"postData,omitempty"`
	BodySize    int64        `json:"bodySize"`
}

type response struct {
	Status      int          `json:"status"`
	StatusText  string       `json:"statusText"`
	Cookies     []cookie     `json:"cookies"`
	Headers     []headerPair `json:"headers"`
	Content     content      `json:"content"`
	RedirectURL string       `json:"redirectURL,omitempty"`
	BodySize    int64        `json:"bodySize"`
}

type headerPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type nameValue struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	FileName string `json:"fileName,omitempty"`
}

type cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type postData struct {
	MimeType string      `json:"mimeType"`
	Text     string      `json:"text"`
	Params   []nameValue `json:"params,omitempty"`
	Encoding string      `json:"encoding,omitempty"`
}

type content struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

type timings struct {
	Blocked float64 `json:"blocked"`
	DNS     float64 `json:"dns"`
	Connect float64 `json:"connect"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	SSL     float64 `json:"ssl"`
}

const unknown = -1

func entryOf(one Entry) entry {
	record := one.Record

	return entry{
		StartedDateTime: time.UnixMilli(record.StartedAt).Format(time.RFC3339Nano),
		Time:            millis(record.DurationUs),
		Request:         requestOf(record, one.RequestBody),
		Response:        responseOf(record, one.ResponseBody),
		Timings:         timingsOf(record),
		Tab:             record.TabTitle,
	}
}

func requestOf(record domain.Record, body string) request {
	out := request{
		Method:      record.Method,
		URL:         record.URL,
		Cookies:     cookiesOf(headerOf(record.RequestHeaders, "Cookie")),
		Headers:     pairsOf(record.RequestHeaders),
		QueryString: queryOf(record.URL),
		BodySize:    record.RequestBytes,
	}

	if body != "" {
		if contentType := headerOf(record.RequestHeaders, "Content-Type"); contentType != "" {
			out.PostData = &postData{MimeType: contentType, Text: body}
		} else {
			out.PostData = &postData{MimeType: domain.MediaText, Text: body}
		}
	}
	return out
}

func responseOf(record domain.Record, body string) response {
	return response{
		Status:      record.Status,
		StatusText:  reasonPhrase(record.StatusText, record.Status),
		Cookies:     setCookiesOf(record.ResponseHeaders),
		Headers:     pairsOf(record.ResponseHeaders),
		Content:     content{Size: record.ResponseBytes, MimeType: record.ContentType, Text: body},
		RedirectURL: redirectOf(record),
		BodySize:    record.ResponseBytes,
	}
}

func timingsOf(record domain.Record) timings {
	return timings{
		Blocked: unknown,
		DNS:     phase(record.DNSUs),
		Connect: phase(record.ConnectUs),
		Send:    unknown,
		Wait:    phase(record.WaitUs),
		Receive: phase(record.DownloadUs),
		SSL:     phase(record.TLSUs),
	}
}

func phase(us *int64) float64 {
	if us == nil {
		return unknown
	}
	return millis(*us)
}

func millis(us int64) float64 {
	return float64(us) / 1000
}

func reasonPhrase(statusText string, status int) string {
	text := strings.TrimSpace(statusText)
	if text == "" {
		return ""
	}
	if prefix := strconv.Itoa(status); prefix != "0" && strings.HasPrefix(text, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	return text
}

func redirectOf(record domain.Record) string {
	if record.Status < 300 || record.Status >= 400 {
		return ""
	}
	return headerOf(record.ResponseHeaders, "Location")
}

func pairsOf(headers []domain.HeaderPair) []headerPair {
	out := make([]headerPair, 0, len(headers))
	for _, header := range headers {
		out = append(out, headerPair{Name: header.Name, Value: header.Value})
	}
	return out
}

func setCookiesOf(headers []domain.HeaderPair) []cookie {
	out := []cookie{}
	for _, one := range domain.ParseResponseCookies(headers, time.Now()) {
		out = append(out, cookie{Name: one.Name, Value: one.Value})
	}
	return out
}

// cookiesOf lists the cookies a Cookie header carries (RFC 6265 §4.2.1). A header that is not one
// lists none, as it names none.
func cookiesOf(header string) []cookie {
	out := []cookie{}
	sent, err := http.ParseCookie(header)
	if err != nil {
		return out
	}
	for _, one := range sent {
		out = append(out, cookie{Name: one.Name, Value: one.Value})
	}
	return out
}

// queryOf is the query string of an address as HAR 1.2 lists it: decoded, in the order it was
// written, with a repeated name once for each time.
func queryOf(raw string) []nameValue {
	_, query := domain.SplitQuery(raw)
	out := []nameValue{}
	for _, pair := range domain.QueryPairs(query) {
		decoded := pair.Decoded()
		out = append(out, nameValue{Name: decoded.Name, Value: decoded.Value})
	}
	return out
}

func headerOf(headers []domain.HeaderPair, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}
