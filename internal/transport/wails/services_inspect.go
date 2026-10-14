package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/inspect"
)

// InspectService reads the body on screen: opened once, then asked by handle. It writes nothing.
type InspectService struct {
	inspect *inspect.UseCase
}

func NewInspectService(uc *inspect.UseCase) *InspectService {
	return &InspectService{inspect: uc}
}

// Open reads a body; one that is not a document gets no handle.
func (s *InspectService) Open(
	ctx context.Context, text, contentType string,
) (domain.Opened, error) {
	return s.inspect.Open(text, contentType)
}

// At describes the node at an offset in UTF-16 units.
func (s *InspectService) At(ctx context.Context, id string, offset int) (domain.NodeView, error) {
	return s.inspect.At(id, offset)
}

// Node describes the node an address names.
func (s *InspectService) Node(ctx context.Context, id, path string) (domain.NodeView, error) {
	return s.inspect.Node(id, path)
}

// Text is what a copy of the node holds.
func (s *InspectService) Text(ctx context.Context, id, path string) (string, error) {
	return s.inspect.Text(id, path)
}

// Diagnose reads an error response; a body that is not one has an empty format.
func (s *InspectService) Diagnose(
	ctx context.Context, id string, status int,
) (domain.Diagnosis, error) {
	return s.inspect.Diagnose(id, status)
}

// Locate finds the request body member a JSON Pointer names, or nil.
func (s *InspectService) Locate(
	ctx context.Context, contentType, text, pointer string,
) *domain.Span {
	return s.inspect.Locate(contentType, text, pointer)
}

// Suggest finds the collection requests a value belongs in, addresses filled in.
func (s *InspectService) Suggest(
	ctx context.Context, id, path string,
) ([]domain.Suggestion, error) {
	return s.inspect.Suggest(ctx, id, path)
}

// Body writes a node as a request body, without server-owned members when bare.
func (s *InspectService) Body(ctx context.Context, id, path string, bare bool) (string, error) {
	return s.inspect.Body(id, path, bare)
}

// Types writes the type of a node's subtree.
func (s *InspectService) Types(
	ctx context.Context, id, path string, format domain.TypeFormat,
) (string, error) {
	return s.inspect.Types(id, path, format)
}

// TypeFormats lists the notations, in the order offered.
func (s *InspectService) TypeFormats(ctx context.Context) []domain.TypeFormat {
	return domain.TypeFormats
}
