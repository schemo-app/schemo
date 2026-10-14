package domain

// OrEmpty answers with a list that is never nil.
//
// A nil slice crosses the boundary as `null`, and the window draws lists everywhere — parameters,
// headers, cookies, form rows, captured bodies. Every one of them would otherwise have to treat
// `null` as a second kind of empty, so the rule is applied here, once, on the way out of every
// layer that writes a list down.
func OrEmpty[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
