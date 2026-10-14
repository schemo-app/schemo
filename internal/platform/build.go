package platform

type BuildInfo struct {
	Version     string
	Build       string
	Name        string
	Slug        string
	Identifier  string
	Description string
}

func NewBuildInfo() BuildInfo {
	return BuildInfo{
		Version:     version,
		Build:       build,
		Name:        Name,
		Slug:        Slug,
		Identifier:  Identifier,
		Description: Description,
	}
}

// UserAgent is the name the app calls itself by: one reader of the rule, because the same string is
// what the requests carry when the settings hold no other.
func (b BuildInfo) UserAgent() string {
	return b.Name + "/" + b.Version
}
