package domain

// Plan is what an organisation is entitled to, and how full those entitlements are.
type Plan struct {
	Tier      string         `json:"tier"`
	Seats     int            `json:"seats"`
	PlanUntil int64          `json:"planUntil,omitempty"`
	Features  []string       `json:"features,omitempty"`
	Quotas    map[string]int `json:"quotas,omitempty"`
	// Meters are the limits with something standing against them — what the window draws as bars.
	// A quota with nothing counted against it is a number without a meaning for a person to read.
	Meters []Meter `json:"meters,omitempty"`
}

// Meter is one resource against its cap. Kind names the resource in the app's own words, and the
// quota it comes from is the server's vocabulary — the reading of the two apart is where the
// server's contract is read, not here.
type Meter struct {
	Kind  string `json:"kind"`
	Used  int    `json:"used"`
	Limit int    `json:"limit"`
}
