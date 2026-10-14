package domain

// UpdateChannel is which releases the app is willing to be offered.
//
// It is a question rather than a constant because a pre-release is the right answer for a user who
// wants one and a wrecked install for a user who does not — so the release source cannot pick.
type UpdateChannel string

const (
	ChannelStable UpdateChannel = "stable"
	ChannelBeta   UpdateChannel = "beta"
)

// Valid reports whether a stored or supplied channel is one the release source can serve.
func (c UpdateChannel) Valid() bool {
	return c == ChannelStable || c == ChannelBeta
}

// Release is one version offered for installation, as the release source describes it.
//
// Notes is sentences rather than the source's own markup. A release body is written for a person to
// read, and what the window holds of it is a list of lines — so the shaping happens once, next to
// the fetch, instead of in every window.
type Release struct {
	Version string   `json:"version"`
	Notes   []string `json:"notes,omitempty"`
}

// UpdateState is what the last check saw, and what the user has since said about it. It is written
// and read whole, which is why it is one value: the version, its notes and the time of the check
// are one fact — the answer to "what did we find" — and a half-written answer would have the window
// offering a version it cannot describe.
//
// It is not a preference and does not live with them (that half is Settings); both are kept in the
// same table, and the difference is which use case understands the value.
type UpdateState struct {
	// LastCheckAt is when a check last succeeded, in ms since the epoch; zero if none ever has.
	LastCheckAt int64 `json:"lastCheckAt,omitempty"`
	// Latest is the newest release the source reported, whether or not it was offered.
	Latest string   `json:"latest,omitempty"`
	Notes  []string `json:"notes,omitempty"`
	// Skipped is the version the user asked not to be told about again. It answers for itself alone:
	// a later release is still offered, and that is the whole difference between skipping a version
	// and turning updates off.
	Skipped string `json:"skipped,omitempty"`
}

// SettingUpdateState is where that value is kept: one key holding it as JSON, in the same table as
// the preferences (see settings.go).
//
// A single key rather than one per field because the store's job here is to hand the value back
// unchanged — it has no reason to know that a version and its notes belong together, and splitting
// them would make a half-written state expressible.
const SettingUpdateState = "update.state"
