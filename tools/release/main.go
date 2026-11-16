// Command release follows a tag through CI and checks the release it
// publishes, as make release and make release-verify run it:
//
//	go run ./tools/release wait v1.2.3 <archive>...    wait for CI, then verify
//	go run ./tools/release verify v1.2.3 <archive>...  verify only
//
// Verifying means what the updater relies on: the tag is the latest published
// release, it carries every archive and checksums.txt, and each archive
// downloads with the checksum listed for it. GITHUB_TOKEN, when set, lifts the
// API's limit of 60 requests an hour.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zakonnic/kbswitcher/internal/buildinfo"
)

const (
	// 45 polls at most, within the API's 60 an hour without a token.
	pollEvery = time.Minute
	// A tag push starts a run within seconds; the builds take a quarter of
	// an hour.
	runAppears = 5 * time.Minute
	runEnds    = 45 * time.Minute
	// Runs older than the wait by more than this belong to an earlier push of
	// the same tag on the same commit.
	pushSlack = time.Minute
)

var client = &http.Client{Timeout: 5 * time.Minute}

func main() {
	if len(os.Args) < 4 || (os.Args[1] != "wait" && os.Args[1] != "verify") {
		fmt.Fprintln(os.Stderr, "usage: release wait|verify <tag> <archive>...")
		os.Exit(2)
	}
	tag, archives := os.Args[2], os.Args[3:]
	var err error
	if os.Args[1] == "wait" {
		err = wait(tag)
	}
	if err == nil {
		err = verify(tag, archives)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// wait follows the CI run of the tag's push until it ends.
func wait(tag string) error {
	out, err := exec.Command("git", "rev-parse", tag+"^{commit}").Output()
	if err != nil {
		return fmt.Errorf("no local tag %s: %w", tag, err)
	}
	commit := strings.TrimSpace(string(out))

	start := time.Now()
	reported := ""
	for {
		var runs struct {
			Runs []struct {
				Branch     string    `json:"head_branch"`
				Status     string    `json:"status"`
				Conclusion string    `json:"conclusion"`
				URL        string    `json:"html_url"`
				Created    time.Time `json:"created_at"`
			} `json:"workflow_runs"`
		}
		// A failed poll is retried until the deadline: the run goes on
		// without us.
		if err := api("/actions/runs?event=push&head_sha="+commit, &runs); err != nil {
			fmt.Fprintln(os.Stderr, "CI status unknown:", err)
		}
		// The newest run of this push; the list is newest first.
		for _, run := range runs.Runs {
			if run.Branch != tag || run.Created.Before(start.Add(-pushSlack)) {
				continue
			}
			if run.Status != reported {
				fmt.Printf("CI %s: %s\n", run.Status, run.URL)
				reported = run.Status
			}
			if run.Status == "completed" {
				if run.Conclusion != "success" {
					return failed(tag, run.Conclusion, run.URL)
				}
				return nil
			}
			break
		}
		switch {
		case reported == "" && time.Since(start) > runAppears:
			return fmt.Errorf("no CI run started for %s in %v", tag, runAppears)
		case time.Since(start) > runEnds:
			return fmt.Errorf("CI has not finished %s in %v", tag, runEnds)
		}
		time.Sleep(pollEvery)
	}
}

// failed says how to recover from a run that published nothing: a re-run
// when the cause needs no new commit, else a new tag on the fix.
func failed(tag, conclusion, url string) error {
	return fmt.Errorf("CI ended with %s, so %s is not published: %s\n"+
		"A flaky test or a runner outage needs no new commit: re-run the failed jobs there, then\n"+
		"  make release-verify V=%s\n"+
		"A fix does: commit it, delete the tag and release again:\n"+
		"  git push origin :refs/tags/%s\n  make untag V=%s\n  make release V=%s",
		conclusion, tag, url, tag, tag, tag, tag)
}

// verify checks the latest release as the updater will use it.
func verify(tag string, archives []string) error {
	var release struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := api("/releases/latest", &release); err != nil {
		return fmt.Errorf("no published release (drafts and pre-releases do not count): %w", err)
	}
	if release.Tag != tag {
		return fmt.Errorf("the latest release is %s, not %s", release.Tag, tag)
	}
	urls := map[string]string{}
	for _, asset := range release.Assets {
		urls[asset.Name] = asset.URL
	}

	sums, err := checksums(urls["checksums.txt"])
	if err != nil {
		return err
	}
	for _, name := range archives {
		if urls[name] == "" {
			return fmt.Errorf("%s has no %s", tag, name)
		}
		if sums[name] == "" {
			return fmt.Errorf("checksums.txt has no line for %s", name)
		}
		data, err := download(urls[name])
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != sums[name] {
			return fmt.Errorf("%s does not match its line in checksums.txt", name)
		}
		fmt.Printf("ok  %s\n", name)
	}
	fmt.Printf("%s is the latest release; checksums.txt vouches for all %d archives.\n", tag, len(archives))
	return nil
}

// checksums reads checksums.txt into file name → SHA-256, keeping the first
// line for a name, as the updater does.
func checksums(url string) (map[string]string, error) {
	if url == "" {
		return nil, errors.New("the release has no checksums.txt")
	}
	data, err := download(url)
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	lines := bufio.NewScanner(strings.NewReader(string(data)))
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if sums[name] == "" {
			sums[name] = strings.ToLower(fields[0])
		}
	}
	return sums, nil
}

// api decodes a GET of the repository's API path into v.
func api(path string, v any) error {
	url := strings.TrimSuffix(buildinfo.APIBaseURL, "/") + "/repos/" + buildinfo.Repository + path
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	body, err := do(request)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func download(url string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return do(request)
}

func do(request *http.Request) ([]byte, error) {
	request.Header.Set("User-Agent", buildinfo.Name+"-release")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	switch {
	case response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, fmt.Errorf("%s: the API's hourly limit is spent; set GITHUB_TOKEN or try later", request.URL)
	case response.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s answered %s", request.URL, response.Status)
	}
	return body, nil
}
