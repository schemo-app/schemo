// Package inspect answers what a position in a response body is. The window sends a body once and
// then asks by handle.
package inspect

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	"schemo/internal/domain"
	"schemo/internal/lens"
)

const (
	// kept is how many documents are held at once.
	kept = 8
	// largest is the biggest body read, in bytes.
	largest = 8 << 20
)

type UseCase struct {
	tree  Tree
	scope domain.Scope

	mu    sync.Mutex
	docs  map[string]*lens.Document
	order []string
}

func NewUseCase(tree Tree, scope domain.Scope) *UseCase {
	return &UseCase{tree: tree, scope: scope, docs: map[string]*lens.Document{}}
}

// Open reads a body and returns its handle. A body that is not a document (or is too large) gets
// no handle and no error.
func (u *UseCase) Open(text, contentType string) (domain.Opened, error) {
	if len(text) > largest {
		return domain.Opened{}, nil
	}
	sum := sha256.Sum256([]byte(contentType + "\x00" + text))
	id := hex.EncodeToString(sum[:8])

	u.mu.Lock()
	defer u.mu.Unlock()
	if doc, ok := u.docs[id]; ok {
		u.touch(id)
		return domain.Opened{ID: id, Kind: doc.Kind()}, nil
	}
	doc, err := lens.Open(contentType, text)
	if errors.Is(err, domain.ErrUnreadable) {
		return domain.Opened{}, nil
	}
	if err != nil {
		return domain.Opened{}, err
	}
	u.docs[id] = doc
	u.order = append(u.order, id)
	if len(u.order) > kept {
		delete(u.docs, u.order[0])
		u.order = u.order[1:]
	}
	return domain.Opened{ID: id, Kind: doc.Kind()}, nil
}

// touch moves a document to the end of the eviction order.
func (u *UseCase) touch(id string) {
	for i, held := range u.order {
		if held == id {
			u.order = append(append(u.order[:i:i], u.order[i+1:]...), id)
			return
		}
	}
}

// document is the one a handle names; an evicted handle is ErrNotFound.
func (u *UseCase) document(id string) (*lens.Document, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	doc, ok := u.docs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	u.touch(id)
	return doc, nil
}

// At is the node at an offset in UTF-16 units.
func (u *UseCase) At(id string, offset int) (domain.NodeView, error) {
	doc, err := u.document(id)
	if err != nil {
		return domain.NodeView{}, err
	}
	return doc.At(offset), nil
}

// Node is the node an address names.
func (u *UseCase) Node(id, path string) (domain.NodeView, error) {
	doc, err := u.document(id)
	if err != nil {
		return domain.NodeView{}, err
	}
	return doc.Node(path)
}

// Text is what a copy of the node holds.
func (u *UseCase) Text(id, path string) (string, error) {
	doc, err := u.document(id)
	if err != nil {
		return "", err
	}
	return doc.Text(path)
}

// Diagnose reads an error response; a body that is not one gives an empty diagnosis.
func (u *UseCase) Diagnose(id string, status int) (domain.Diagnosis, error) {
	doc, err := u.document(id)
	if err != nil {
		return domain.Diagnosis{}, err
	}
	found, _ := doc.Diagnose(status)
	return found, nil
}

// Locate finds the request body member a JSON Pointer names, or nil.
func (u *UseCase) Locate(contentType, text, pointer string) *domain.Span {
	span, ok := lens.Locate(contentType, text, pointer)
	if !ok {
		return nil
	}
	return &span
}

// Body writes a node as a request body, without server-owned members when bare.
func (u *UseCase) Body(id, path string, bare bool) (string, error) {
	doc, err := u.document(id)
	if err != nil {
		return "", err
	}
	return doc.Body(path, bare)
}

// Types writes the type of a node's subtree in a notation.
func (u *UseCase) Types(id, path string, format domain.TypeFormat) (string, error) {
	doc, err := u.document(id)
	if err != nil {
		return "", err
	}
	return doc.Types(path, format)
}
