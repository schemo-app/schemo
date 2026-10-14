package publication

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// Encode writes a document as the bytes a publish carries: one JSON envelope, compressed, with the
// digest of what it says. The digest is of the uncompressed document, and the server recomputes it
// from the bytes it expands.
func Encode(snapshot snapshotv1.Snapshot) ([]byte, string, error) {
	document, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	digest, err := Digest(document)
	if err != nil {
		return nil, "", err
	}

	var packed bytes.Buffer
	sink := gzip.NewWriter(&packed)
	if _, err := sink.Write(document); err != nil {
		return nil, "", err
	}
	if err := sink.Close(); err != nil {
		return nil, "", err
	}
	return packed.Bytes(), digest, nil
}

// Digest is the identity of a document's content, as both this device and the server compute it.
// Two documents that differ only in which build wrote them, or in which language they are read in,
// are the same document: those two fields are dropped before hashing, and everything else is
// written in the one order that does not depend on who wrote it — keys sorted at every level,
// numbers as they were written. That is what lets a device say a page is behind without publishing
// to find out.
func Digest(document []byte) (string, error) {
	var fields map[string]any
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		return "", err
	}
	delete(fields, "generator")
	delete(fields, "locale")

	canonical, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
