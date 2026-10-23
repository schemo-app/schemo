// Package platform is what every feature reads about the app and the machine it runs on: the
// app's name and identifiers, build info, data folders, ids and file limits.
package platform

// Application metadata and identification constants.
const (
	Name          = "Schemo"
	Slug          = "schemo"
	Identifier    = "app.schemo"
	Scheme        = "schemo"
	Company       = "Yuri Gerassimov"
	Copyright     = "© 2026, Yuri Gerassimov"
	Description   = "A cross-platform API client for building, testing, and sharing HTTP requests."
	RepoOwner     = "schemo-app"
	DefaultServer = "http://localhost:8080"

	// DataFolder is the folder in the user's home directory that holds the app's files, and DataDirEnv
	// the variable that puts them somewhere else.
	DataFolder = ".schemo"
	DataDirEnv = "SCHEMO_DATA_DIR"

	// KeyringService is the name secrets are filed under in the system's credential store.
	KeyringService = "schemo"
)

var (
	version = "dev"
	build   = ""
)

func Homepage() string {
	return "https://github.com/" + RepoOwner + "/" + Slug
}
