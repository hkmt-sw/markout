// Package update finds out whether a newer markout release exists. It only
// reports; installing the release is left to the user.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// LatestReleaseAPI answers with the newest published release.
	LatestReleaseAPI = "https://api.github.com/repos/hkmt-sw/markout/releases/latest"
	// ReleasesPage is where a person downloads it.
	ReleasesPage = "github.com/hkmt-sw/markout/releases"
	// Interval is how often the automatic check runs at most.
	Interval = 24 * time.Hour

	timeout = 5 * time.Second
)

// Latest asks url (normally LatestReleaseAPI) for the tag of the newest
// release, such as "v1.2.0". current is sent as the User-Agent version; no
// other information about the user or the machine is transmitted.
func Latest(ctx context.Context, url, current string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "markout/"+current)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update check: %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return "", err
	}
	if _, ok := parse(release.TagName); !ok {
		return "", fmt.Errorf("update check: unexpected release tag %q", release.TagName)
	}
	return release.TagName, nil
}

// IsRelease reports whether v is a release version: a plain one (v1.2.3) or
// a release candidate (v1.2.3-rc1). Development builds ("dev",
// "v1.2.3-4-gabc123-dirty") are not, and are never told to update.
func IsRelease(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer reports whether release version latest is higher than current. A
// release is newer than its own candidates.
func Newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// final stands for "not a candidate" in the place of a candidate's number,
// which puts a release after all of its candidates.
const final = 1 << 30

// parse reads "v1.2.3" or "1.2.3" into its three numbers, followed by the
// number of the release candidate for "v1.2.3-rc2".
func parse(v string) ([4]int, bool) {
	out := [4]int{3: final}
	v = strings.TrimPrefix(v, "v")
	if base, candidate, ok := strings.Cut(v, "-rc"); ok {
		n, err := strconv.Atoi(candidate)
		if err != nil || n < 1 || candidate[0] == '0' {
			return out, false
		}
		v, out[3] = base, n
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p == "" || (len(p) > 1 && p[0] == '0') {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
