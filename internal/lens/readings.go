package lens

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"schemo/internal/domain"
)

// readingsOf lists the ways a scalar reads as something else; a reading is offered only on a close
// fit.
func readingsOf(n *node) []domain.Reading {
	switch n.kind {
	case domain.NodeString, domain.NodeAttribute, domain.NodeText:
		return stringReadings(strings.TrimSpace(n.scalar))
	case domain.NodeNumber:
		return numberReadings(n.scalar)
	}
	return nil
}

func stringReadings(s string) []domain.Reading {
	if s == "" {
		return nil
	}
	var out []domain.Reading
	for _, detect := range []func(string) *domain.Reading{
		readJWT, readUUID, readULID, readObjectID, readDateTime, readURL, readColour,
		readDataURI, readIP, readEmail, readHash, readJSONText, readBase64, readEpochText,
	} {
		if r := detect(s); r != nil {
			out = append(out, *r)
		}
	}
	return out
}

func numberReadings(text string) []domain.Reading {
	var out []domain.Reading
	if r := readEpoch(text); r != nil {
		out = append(out, *r)
	}
	if r := readUnsafeInteger(text); r != nil {
		out = append(out, *r)
	}
	return out
}

func row(key, value, shape string) domain.ReadingRow {
	return domain.ReadingRow{Key: key, Value: value, Shape: shape}
}

func moment(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

var jwtShape = regexp.MustCompile(`^[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]*$`)

// readJWT decodes a token's header and claims. The signature is not verified: there is no key.
func readJWT(s string) *domain.Reading {
	if !jwtShape.MatchString(s) {
		return nil
	}
	parts := strings.Split(s, ".")
	var header, claims map[string]any
	if decodeSegment(parts[0], &header) != nil || decodeSegment(parts[1], &claims) != nil {
		return nil
	}
	if _, ok := header["alg"]; !ok {
		return nil
	}

	r := &domain.Reading{Kind: "jwt"}
	for _, key := range []string{"alg", "typ", "kid"} {
		if v, ok := header[key]; ok {
			r.Rows = append(r.Rows, row(key, fmt.Sprint(v), "code"))
		}
	}
	for _, key := range []string{"iss", "sub", "aud", "jti", "scope"} {
		if v, ok := claims[key]; ok {
			r.Rows = append(r.Rows, row(key, scalarText(v), "code"))
		}
	}
	for _, key := range []string{"iat", "nbf", "exp"} {
		if v, ok := claims[key].(float64); ok {
			r.Rows = append(r.Rows, row(key, moment(time.Unix(int64(v), 0)), "time"))
		}
	}
	pretty, _ := json.MarshalIndent(claims, "", "  ")
	r.Nested = string(pretty)
	return r
}

func decodeSegment(part string, into any) error {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(part, "="))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func scalarText(v any) string {
	if list, ok := v.([]any); ok {
		parts := make([]string, len(list))
		for i, item := range list {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

var uuidShape = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// readUUID reads a UUID's version, and its time for versions 1, 6 and 7.
func readUUID(s string) *domain.Reading {
	if !uuidShape.MatchString(s) {
		return nil
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil {
		return nil
	}
	version := int(raw[6] >> 4)
	r := &domain.Reading{Kind: "uuid"}
	r.Rows = append(r.Rows, row("version", strconv.Itoa(version), "text"))
	switch version {
	case 1, 6:
		var ticks uint64
		if version == 1 {
			ticks = uint64(binary.BigEndian.Uint16(raw[6:8])&0x0fff)<<48 |
				uint64(binary.BigEndian.Uint16(raw[4:6]))<<32 |
				uint64(binary.BigEndian.Uint32(raw[0:4]))
		} else {
			ticks = uint64(binary.BigEndian.Uint32(raw[0:4]))<<28 |
				uint64(binary.BigEndian.Uint16(raw[4:6]))<<12 |
				uint64(binary.BigEndian.Uint16(raw[6:8])&0x0fff)
		}
		const gregorianToUnix = 122192928000000000 // 100 ns ticks between 1582 and 1970
		if ticks >= gregorianToUnix {
			at := time.Unix(0, int64(ticks-gregorianToUnix)*100)
			r.Rows = append(r.Rows, row("time", moment(at), "time"))
		}
	case 7:
		ms := binary.BigEndian.Uint64(append([]byte{0, 0}, raw[0:6]...))
		r.Rows = append(r.Rows, row("time", moment(time.UnixMilli(int64(ms))), "time"))
	}
	return r
}

var ulidShape = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

// readULID reads a ULID's time.
func readULID(s string) *domain.Reading {
	if !ulidShape.MatchString(s) {
		return nil
	}
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var ms uint64
	for _, c := range s[:10] {
		ms = ms<<5 | uint64(strings.IndexRune(alphabet, c))
	}
	at := time.UnixMilli(int64(ms))
	if at.Year() < 2000 || at.Year() > 2100 {
		return nil
	}
	return &domain.Reading{Kind: "ulid", Rows: []domain.ReadingRow{row("time", moment(at), "time")}}
}

var objectIDShape = regexp.MustCompile(`^[0-9a-f]{24}$`)

// readObjectID reads a MongoDB ObjectId's time.
func readObjectID(s string) *domain.Reading {
	if !objectIDShape.MatchString(s) {
		return nil
	}
	seconds, err := strconv.ParseUint(s[:8], 16, 32)
	if err != nil {
		return nil
	}
	at := time.Unix(int64(seconds), 0)
	if at.Year() < 2005 || at.Year() > 2100 {
		return nil
	}
	return &domain.Reading{Kind: "objectid", Rows: []domain.ReadingRow{
		row("time", moment(at), "time"),
	}}
}

var hashLengths = map[int]string{32: "MD5", 40: "SHA-1", 64: "SHA-256", 128: "SHA-512"}
var hexShape = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// readHash names the digest a hex string's length suggests.
func readHash(s string) *domain.Reading {
	name, ok := hashLengths[len(s)]
	if !ok || !hexShape.MatchString(s) || objectIDShape.MatchString(s) {
		return nil
	}
	return &domain.Reading{Kind: "hash", Rows: []domain.ReadingRow{row("algorithm", name, "text")}}
}

var timeLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04",
	"2006-01-02", time.RFC1123, time.RFC1123Z, time.RFC850, time.ANSIC,
}

// readDateTime reads the date formats APIs use.
func readDateTime(s string) *domain.Reading {
	if len(s) < 8 || len(s) > 40 {
		return nil
	}
	for _, layout := range timeLayouts {
		if at, err := time.Parse(layout, s); err == nil {
			return &domain.Reading{Kind: "datetime", Rows: []domain.ReadingRow{
				row("time", moment(at), "time"),
				row("written", s, "code"),
			}}
		}
	}
	return nil
}

var digitsShape = regexp.MustCompile(`^\d+$`)

// readEpochText is readEpoch for a number written as a string.
func readEpochText(s string) *domain.Reading {
	if !digitsShape.MatchString(s) {
		return nil
	}
	return readEpoch(s)
}

// readEpoch reads a number as Unix seconds or milliseconds when it falls in 2000–2100.
func readEpoch(text string) *domain.Reading {
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil
	}
	unit, at := "", time.Time{}
	switch {
	case v >= 946684800 && v <= 4102444800:
		unit, at = "seconds", time.Unix(int64(v), int64((v-math.Floor(v))*1e9))
	case v >= 946684800000 && v <= 4102444800000:
		unit, at = "milliseconds", time.UnixMilli(int64(v))
	default:
		return nil
	}
	return &domain.Reading{Kind: "timestamp", Rows: []domain.ReadingRow{
		row("unit", unit, "text"), row("time", moment(at), "time"),
	}}
}

// readUnsafeInteger flags an integer beyond 2^53, which JavaScript would round.
func readUnsafeInteger(text string) *domain.Reading {
	if strings.ContainsAny(text, ".eE") {
		return nil
	}
	digits := strings.TrimPrefix(text, "-")
	if len(digits) < 16 {
		return nil
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil || math.Abs(v) <= 9007199254740991 {
		return nil
	}
	return &domain.Reading{Kind: "bigint", Rows: []domain.ReadingRow{
		row("exact", text, "code"),
		row("asJavaScript", strconv.FormatFloat(v, 'f', -1, 64), "code"),
	}}
}

// readURL splits an address into its parts.
func readURL(s string) *domain.Reading {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return nil
	}
	r := &domain.Reading{Kind: "url", Rows: []domain.ReadingRow{
		row("scheme", u.Scheme, "text"), row("host", u.Host, "code"),
	}}
	if p := u.EscapedPath(); p != "" && p != "/" {
		r.Rows = append(r.Rows, row("path", p, "code"))
	}
	query := u.Query()
	shown := 0
	for key, values := range query {
		if shown == 12 {
			break
		}
		r.Rows = append(r.Rows, row("param:"+key, strings.Join(values, ","), "code"))
		shown++
	}
	if u.Fragment != "" {
		r.Rows = append(r.Rows, row("fragment", u.Fragment, "code"))
	}
	return r
}

var emailShape = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]{2,}$`)

func readEmail(s string) *domain.Reading {
	if !emailShape.MatchString(s) {
		return nil
	}
	at := strings.LastIndexByte(s, '@')
	return &domain.Reading{Kind: "email", Rows: []domain.ReadingRow{
		row("local", s[:at], "code"), row("domain", s[at+1:], "code"),
	}}
}

// readIP reads an address's version and range.
func readIP(s string) *domain.Reading {
	ip := net.ParseIP(s)
	if ip == nil {
		return nil
	}
	version := "6"
	if ip.To4() != nil {
		version = "4"
	}
	scope := "public"
	switch {
	case ip.IsLoopback():
		scope = "loopback"
	case ip.IsPrivate():
		scope = "private"
	case ip.IsLinkLocalUnicast():
		scope = "linkLocal"
	case ip.IsUnspecified():
		scope = "unspecified"
	case ip.IsMulticast():
		scope = "multicast"
	}
	return &domain.Reading{Kind: "ip", Rows: []domain.ReadingRow{
		row("version", version, "text"), row("range", scope, "text"),
	}}
}

var hexColour = regexp.MustCompile(`^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
var funcColour = regexp.MustCompile(`^(?i)(rgba?|hsla?)\([^)]{5,}\)$`)

// readColour recognises CSS colours.
func readColour(s string) *domain.Reading {
	if !hexColour.MatchString(s) && !funcColour.MatchString(s) {
		return nil
	}
	return &domain.Reading{Kind: "colour", Rows: []domain.ReadingRow{row("colour", s, "colour")}}
}

var dataURI = regexp.MustCompile(`^data:([a-zA-Z0-9.+/-]*)((?:;[a-zA-Z0-9=.+_-]+)*),(.*)$`)

// readDataURI reads a data URI's type and size.
func readDataURI(s string) *domain.Reading {
	m := dataURI.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	mime := m[1]
	if mime == "" {
		mime = "text/plain"
	}
	size := len(m[3])
	if strings.Contains(m[2], ";base64") {
		size = base64.StdEncoding.DecodedLen(len(m[3]))
	}
	return &domain.Reading{Kind: "datauri", Rows: []domain.ReadingRow{
		row("mime", mime, "code"), row("size", strconv.Itoa(size), "text"),
	}}
}

var base64Shape = regexp.MustCompile(`^[A-Za-z0-9+/_-]{16,}={0,2}$`)

var magic = []struct {
	prefix []byte
	mime   string
}{
	{[]byte("\x89PNG\r\n\x1a\n"), "image/png"},
	{[]byte("\xff\xd8\xff"), "image/jpeg"},
	{[]byte("GIF8"), "image/gif"},
	{[]byte("%PDF-"), "application/pdf"},
	{[]byte("PK\x03\x04"), "application/zip"},
	{[]byte("\x1f\x8b"), "application/gzip"},
}

// readBase64 decodes only what is surely base64: a known file signature, or text. Anything looser
// would read identifiers as encodings.
func readBase64(s string) *domain.Reading {
	if !base64Shape.MatchString(s) || hexShape.MatchString(s) || uuidShape.MatchString(s) {
		return nil
	}
	var raw []byte
	var err error
	if strings.ContainsAny(s, "-_") {
		raw, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	} else {
		raw, err = base64.StdEncoding.DecodeString(padded(s))
	}
	if err != nil || len(raw) < 4 {
		return nil
	}
	r := &domain.Reading{Kind: "base64"}
	r.Rows = append(r.Rows, row("size", strconv.Itoa(len(raw)), "text"))
	for _, m := range magic {
		if bytes.HasPrefix(raw, m.prefix) {
			r.Rows = append(r.Rows, row("mime", m.mime, "code"))
			return r
		}
	}
	if !utf8.Valid(raw) || !printable(raw) {
		return nil
	}
	text := string(raw)
	if nested := prettyJSON(text); nested != "" {
		r.Nested = nested
	} else {
		r.Rows = append(r.Rows, row("text", cut(text), "code"))
	}
	return r
}

func padded(s string) string {
	if extra := len(s) % 4; extra != 0 {
		return s + strings.Repeat("=", 4-extra)
	}
	return s
}

// printable reports whether nearly all bytes are text.
func printable(raw []byte) bool {
	odd := 0
	for _, r := range string(raw) {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			odd++
		}
	}
	return odd*20 < len(raw)
}

// readJSONText finds a JSON document written inside a string.
func readJSONText(s string) *domain.Reading {
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		return nil
	}
	pretty := prettyJSON(s)
	if pretty == "" {
		return nil
	}
	return &domain.Reading{Kind: "json", Nested: pretty}
}

// prettyJSON formats text as JSON, or returns empty.
func prettyJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	var out bytes.Buffer
	if json.Indent(&out, []byte(trimmed), "", "  ") != nil {
		return ""
	}
	return out.String()
}
