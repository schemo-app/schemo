package platform

// Application metadata and identification constants.
const (
	Name          = "Schemo"
	Slug          = "schemo"
	Identifier    = "app.schemo"
	Scheme        = "schemo"
	Company       = "Yuri Gerasimov"
	Copyright     = "© 2026, Yuri Gerasimov"
	Description   = "A cross-platform API client for building, testing, and sharing HTTP requests."
	RepoOwner     = "schemo-app"
	DataDirName   = "schemo"
	DefaultServer = "http://localhost:8080"
)

var (
	version = "dev"
	build   = ""
)

func Homepage() string {
	return "https://github.com/" + RepoOwner + "/" + Slug
}
