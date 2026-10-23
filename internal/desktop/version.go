package desktop

import (
	"strings"

	"golang.org/x/mod/semver"
)

func isNewer(candidate, current string) bool {
	c, cur := canonical(candidate), canonical(current)
	return semver.IsValid(c) && semver.IsValid(cur) && semver.Compare(c, cur) > 0
}

func canonical(version string) string {
	return "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
}
