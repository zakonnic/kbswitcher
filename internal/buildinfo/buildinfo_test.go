package buildinfo

import (
	"net/url"
	"testing"
)

func TestRepositoryURL(t *testing.T) {
	u, err := url.Parse(RepositoryURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "/"+Repository {
		t.Fatalf("RepositoryURL %q is not an https address of %q: %v", RepositoryURL, Repository, err)
	}
}
