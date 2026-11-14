package db

import (
	"context"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// A binary answer is kept byte for byte, and the list says it is binary without sending its bytes.
func TestABinaryBodyIsKeptWholeAndListedAsBinary(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	video := "\x00\x00\x00\x20ftypisom" + strings.Repeat("\xff\x00", domain.BodySniffBytes)
	saved := sampleRecord("rec-1", domain.SourceManual)
	body := domain.BodyOf(video, false)
	saved.ResponseBody = &body
	if err := store.SaveRecord(ctx, ws, saved); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	list, err := store.Records(ctx, ws, domain.SourceManual, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("Records = %d, %v", len(list), err)
	}
	if ref := list[0].ResponseBody; ref == nil || !ref.Binary || ref.Inline != "" || ref.Size != int64(len(video)) {
		t.Errorf("responseBody = %+v, want it listed as binary with its size", ref)
	}
	if ref := list[0].RequestBody; ref == nil || ref.Binary {
		t.Errorf("requestBody = %+v, want the JSON request listed as text", ref)
	}

	read, err := store.ReadBody(ctx, "rec-1", domain.SideResponse)
	if err != nil || read != video {
		t.Errorf("ReadBody = %d bytes, %v; want the %d bytes saved", len(read), err, len(video))
	}
}
