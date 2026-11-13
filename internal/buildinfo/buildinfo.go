// Package buildinfo holds the identity of the running binary. Version and
// Commit are set at link time by the Makefile.
package buildinfo

import "fmt"

// Fixed identity. These values end up in the configuration path, the
// autostart entry and the window titles, so they must not change between
// releases.
const (
	Name = "kbswitcher"
	// ID is the reverse-DNS identifier: macOS bundle identifier, Fyne
	// preferences key, autostart entry name, instance lock name.
	ID = "com.zakonnic.kbswitcher"
	// Repository is the "owner/repo" the update check queries. A constant, so
	// no -ldflags can point the updater elsewhere.
	Repository    = "zakonnic/kbswitcher"
	RepositoryURL = "https://github.com/" + Repository
)

// Values injected with -ldflags "-X ...".
var (
	Version = "dev"
	Commit  = "unknown"
	// APIBaseURL is the GitHub API root, overridable for a GitHub Enterprise
	// instance.
	APIBaseURL = "https://api.github.com"
)

// String renders the identity for the About window and the logs.
func String() string {
	return fmt.Sprintf("%s %s (%s)", Name, Version, Commit)
}
