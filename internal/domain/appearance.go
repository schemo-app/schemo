package domain

// Accent is which family of the palette the window is drawn in. These are families the app already
// had; the choice moves the accent onto one of them and mixes nothing new.
type Accent string

const (
	AccentBlue   Accent = "blue"
	AccentPurple Accent = "purple"
	AccentGreen  Accent = "green"
	AccentOrange Accent = "orange"
	AccentRed    Accent = "red"
	AccentGrey   Accent = "grey"
)

// Valid reports whether a stored or supplied accent is one the palette has.
func (a Accent) Valid() bool {
	switch a {
	case AccentBlue, AccentPurple, AccentGreen, AccentOrange, AccentRed, AccentGrey:
		return true
	}
	return false
}

// UISize is how large the interface is drawn: the root the window is measured against, which is why
// every size in the app is written in rem.
type UISize string

const (
	SizeSmall  UISize = "small"
	SizeMedium UISize = "medium"
	SizeLarge  UISize = "large"
)

func (s UISize) Valid() bool {
	return s == SizeSmall || s == SizeMedium || s == SizeLarge
}

// SizeOf reads a scale by the name it was written down as, with the percentage steps a build
// between two of these wrote read as the nearest word: a window that suddenly drew itself at
// another size would be the app changing a setting nobody touched.
func SizeOf(name string) UISize {
	switch UISize(name) {
	case "90":
		return SizeSmall
	case "100":
		return SizeMedium
	case "110", "125":
		return SizeLarge
	}
	if UISize(name).Valid() {
		return UISize(name)
	}
	return SizeMedium
}

// MonoFont is which family the code is set in. These are the fonts' own names, spelled the same in
// every language; the system one is whatever the machine has.
type MonoFont string

const (
	MonoJetBrains MonoFont = "jetbrains"
	MonoCascadia  MonoFont = "cascadia"
	MonoFira      MonoFont = "fira"
	MonoSystem    MonoFont = "system"
)

func (m MonoFont) Valid() bool {
	switch m {
	case MonoJetBrains, MonoCascadia, MonoFira, MonoSystem:
		return true
	}
	return false
}

// Appearance is how the window is drawn. None of it reaches the wire: it is the window's clothes.
type Appearance struct {
	Accent   Accent   `json:"accent"`
	Size     UISize   `json:"size"`
	MonoFont MonoFont `json:"monoFont"`
	// MonoSize is the code's size in the units of the base the stylesheet is written against.
	MonoSize int `json:"monoSize"`
}

func DefaultAppearance() Appearance {
	return Appearance{
		Accent:   AccentBlue,
		Size:     SizeMedium,
		MonoFont: MonoSystem,
		MonoSize: 13,
	}
}

// Valid refuses an appearance with a part the window has no way of drawing.
func (a Appearance) Valid() error {
	for _, held := range []struct {
		field string
		ok    bool
	}{
		{"accent", a.Accent.Valid()},
		{"size", a.Size.Valid()},
		{"monoFont", a.MonoFont.Valid()},
	} {
		if !held.ok {
			return Refuse(CodeUnknownField, ErrNotAllowed, Args{"field": held.field})
		}
	}
	return nil
}

// The code's size is kept between a size that is still readable and one a window can hold.
const (
	MinMonoSize = 10
	MaxMonoSize = 20
)
