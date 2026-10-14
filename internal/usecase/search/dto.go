package search

import (
	"schemo/internal/domain"
)

// The shape that exists only for the window: what a query asks for. What it answers with is drawn
// from domain types and is not here.

// Query is what the palette asks: the words typed, and the one area the user narrowed to.
//
// No area is nil rather than an empty word, because that is what it is: a kind this build does not
// know is a kind a newer window asked for, and it should search nothing rather than everything.
//
// The rows a group draws are not a number here: how many fit under one heading is the answer's own
// business, and the window should not have to know it.
type Query struct {
	Text string             `json:"text"`
	Kind *domain.SearchKind `json:"kind"`
}
