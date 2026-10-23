package agent

import (
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
)

//go:embed browser_ids.json
var browserIDsJSON []byte

type browserIDs struct {
	Chrome  string `json:"chrome"`
	Edge    string `json:"edge"`
	Firefox string `json:"firefox"`
}

var chromiumID = regexp.MustCompile(`^[a-p]{32}$`)

func loadBrowserIDs() (browserIDs, error) {
	var ids browserIDs
	if err := json.Unmarshal(browserIDsJSON, &ids); err != nil {
		return ids, err
	}
	for _, id := range []string{ids.Chrome, ids.Edge} {
		if id != "" && !chromiumID.MatchString(id) {
			return ids, errors.New("invalid Chromium extension ID in browser_ids.json")
		}
	}
	if ids.Firefox == "" || strings.ContainsAny(ids.Firefox, "*/\\ \r\n\t") {
		return ids, errors.New("invalid Firefox extension ID in browser_ids.json")
	}
	return ids, nil
}

func (ids browserIDs) origins() []string {
	origins := make([]string, 0, 2)
	for _, id := range []string{ids.Chrome, ids.Edge} {
		origin := "chrome-extension://" + id + "/"
		if id != "" && !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}
	return origins
}
