package publication

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/schemo-app/proto/schema"
	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

func TestGeneratedSnapshotKeepsLegacyDigestAndEndpointFingerprint(t *testing.T) {
	raw, err := schema.FS.ReadFile("fixtures/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	held, err := snapshotv1.UnmarshalSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(held)
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, compact.Bytes()) {
		t.Fatalf("v1 writer changed its representation: %s", encoded)
	}
	const digest = "677fb9254f42c21b50b828bbc97f1402de280090509481abc47f40d71ecbe9c3"
	if got, err := Digest(encoded); err != nil || got != digest {
		t.Fatalf("digest = %q, %v; want %q", got, err, digest)
	}
	outline, err := outlineOf(held)
	if err != nil || len(outline) != 1 || outline[0].Fingerprint != "983a1894f8e9" {
		t.Fatalf("legacy endpoint fingerprint changed: %+v, %v", outline, err)
	}
	packed, got, err := Encode(held)
	if err != nil || got != digest {
		t.Fatalf("Encode digest = %q, %v", got, err)
	}
	if _, err := AsDocument(packed); err != nil {
		t.Fatalf("generated writer cannot import its own v1 document: %v", err)
	}
}
