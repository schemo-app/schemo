package update

type Status struct {
	Current     string   `json:"current"`
	Latest      string   `json:"latest"`
	Notes       []string `json:"notes,omitempty"`
	CheckedAt   int64    `json:"checkedAt,omitempty"`
	Available   bool     `json:"available"`
	Development bool     `json:"development,omitempty"`
}
