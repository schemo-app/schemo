package request

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"schemo/internal/domain"
)

// A binary answer reaches the window as a flag and its size, and its bytes reach the store intact.
func TestABinaryAnswerTravelsAsAFlagAndIsKeptWhole(t *testing.T) {
	history, store, executor, notifier := newHistory(t)
	video := "\x00\x00\x00\x20ftypisom\x00\x00\x02\x00\xff\xfe"
	executor.response = domain.Response{Status: 200, ContentType: "video/mp4", Body: video}

	if _, err := history.Send(context.Background(), input()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)

	body := finished.Record.ResponseBody
	if body == nil || !body.Binary || body.Inline != "" || body.Size != int64(len(video)) {
		t.Fatalf("responseBody = %+v, want a binary body with its size", body)
	}
	wire, err := json.Marshal(finished.Record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !utf8.Valid(wire) || strings.Contains(string(wire), "ftyp") || !strings.Contains(string(wire), `"binary":true`) {
		t.Errorf("the window would get %s, want the flag and none of the bytes", wire)
	}
	if listed := store.saved()[0].ResponseBody; listed == nil || !listed.Binary {
		t.Errorf("the history lists %+v, want the body marked binary", listed)
	}
	if kept, err := store.store.ReadBody(context.Background(), finished.ID, domain.SideResponse); err != nil || kept != video {
		t.Errorf("the store holds %q, %v; want the bytes as they came", kept, err)
	}
}

func TestResponsePreviewUsesStoredBytes(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"
	video := "\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42" + strings.Repeat("\xff", 300)
	pdf := "%PDF-1.7\r\n" + strings.Repeat("\x00\xff", 200)
	cases := []struct {
		name, declared, content, mediaType string
		playable                           bool
	}{
		{"image sent as bytes", "application/octet-stream", png, "image/png", true},
		{"declared image", "image/avif; charset=binary", "\x00\x00\x00\x1cftypavif", "image/avif", true},
		{"declared video", "video/mp4", video, "video/mp4", true},
		{"video sent as bytes", "application/octet-stream", video, "video/mp4", true},
		{"PDF", "application/pdf", pdf, "application/pdf", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			history, _, executor, notifier := newHistory(t)
			executor.response = domain.Response{Status: 200, ContentType: tt.declared, Body: tt.content}
			if _, err := history.Send(ctx, input()); err != nil {
				t.Fatalf("Send: %v", err)
			}
			finished := notifier.waitFor(t, TopicRequestFinished).(RequestFinished)
			preview, err := history.ResponsePreview(ctx, finished.ID)
			if err != nil {
				t.Fatalf("ResponsePreview: %v", err)
			}
			want := BinaryPreview{
				ContentType: tt.mediaType,
				Hex:         hex.Dump([]byte(tt.content[:min(len(tt.content), 256)])),
				HexBytes:    min(len(tt.content), 256),
			}
			if tt.playable {
				want.MediaURL = "data:" + tt.mediaType + ";base64," + base64.StdEncoding.EncodeToString([]byte(tt.content))
			}
			if preview != want {
				t.Errorf("ResponsePreview = %+v, want %+v", preview, want)
			}
		})
	}
}

func TestASavedResponseIsNamedAfterWhatTheServerCalledIt(t *testing.T) {
	cases := []struct {
		name   string
		record domain.Record
		want   string
	}{
		{
			"Content-Disposition",
			domain.Record{
				RecordSummary: domain.RecordSummary{URL: "https://example.com/download?id=7"},
				ResponseHeaders: []domain.HeaderPair{
					{Name: "content-disposition", Value: `attachment; filename="report 2026.pdf"`},
				},
			},
			"report 2026.pdf",
		},
		{
			"address",
			domain.Record{RecordSummary: domain.RecordSummary{URL: "https://tetiva.app/demo/request-dark.mp4"}},
			"request-dark.mp4",
		},
		{
			"media type",
			domain.Record{RecordSummary: domain.RecordSummary{URL: "https://example.com/image", ContentType: "image/png"}},
			"response.png",
		},
		{
			"nothing to go by",
			domain.Record{RecordSummary: domain.RecordSummary{URL: "https://example.com/"}},
			"response",
		},
	}
	for _, c := range cases {
		if got := responseFileName(c.record); got != c.want {
			t.Errorf("%s: name = %q, want %q", c.name, got, c.want)
		}
	}
}
