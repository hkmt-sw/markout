package convert

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hkmt-sw/markout/internal/flavor"
)

// imageContainers are documents with one image each, in every kind of place an
// image can be, written with %s where the address of the image server goes.
// fetched says whether the renderers must download the image. The others are
// shown as their alt text by the parser, in quotes, alerts, footnotes and table
// cells; they may be fetched one day, but are never fetched without being
// listed.
var imageContainers = []struct {
	name    string
	md      string
	fetched bool
}{
	{"block", "![alt](%s/a.png)\n", true},
	{"inline in a paragraph", "Some text ![alt](%s/a.png) and more text.\n", true},
	{"html img", "<img src=\"%s/a.png\" alt=\"alt\">\n", true},
	{"list item text", "- item ![alt](%s/a.png)\n", true},
	{"list item block", "- item\n\n  ![alt](%s/a.png)\n", true},
	{"ordered list item block", "1. item\n\n   ![alt](%s/a.png)\n", true},
	{"nested list item block", "- item\n  - nested\n\n    ![alt](%s/a.png)\n", true},
	{"list in a quote", "> - item\n>\n>   ![alt](%s/a.png)\n", true},
	{"quote", "> quoted\n>\n> ![alt](%s/a.png)\n", false},
	{"alert", "> [!NOTE]\n> text\n>\n> ![alt](%s/a.png)\n", false},
	{"footnote", "Text[^1]\n\n[^1]: note ![alt](%s/a.png)\n", false},
	{"table cell", "| a |\n|---|\n| ![alt](%s/a.png) |\n", false},
}

// The servers the renderers download from are exactly the servers the user is
// shown, whatever the images are in, and the same ones in both formats.
func TestRemoteImageHostsAreWhatTheRenderersFetch(t *testing.T) {
	type served struct {
		name    string
		host    string
		hits    *atomic.Int32
		fetched bool
	}
	var servers []served
	var md strings.Builder
	md.WriteString("# Images\n\n")
	for _, c := range imageContainers {
		url, hits := imageServer(t)
		servers = append(servers, served{c.name, strings.TrimPrefix(url, "http://"), hits, c.fetched})
		md.WriteString(strings.ReplaceAll(c.md, "%s", url) + "\n")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(src, []byte(md.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	hosts, err := RemoteImageHosts(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	listed := RemoteHostNames(hosts)
	for _, s := range servers {
		if n := s.hits.Load(); n != 0 {
			t.Fatalf("listing the hosts made %d requests to %s", n, s.name)
		}
		if s.fetched && !slices.Contains(listed, s.host) {
			t.Errorf("the image in %s is not listed: %v", s.name, listed)
		}
	}

	for _, name := range []string{"out.pdf", "out.docx"} {
		opts := Options{RemoteImages: true, RemoteHosts: listed}
		if err := ConvertFileWith(src, filepath.Join(dir, name), FormatUnknown, opts); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var requested []string
		for _, s := range servers {
			if s.hits.Swap(0) > 0 {
				requested = append(requested, s.host)
			} else if s.fetched {
				t.Errorf("%s: the image in %s was not downloaded", name, s.name)
			}
		}
		if !sameSet(requested, listed) {
			t.Errorf("%s: downloaded from %v, listed %v", name, requested, listed)
		}
	}
}

func sameSet(a, b []string) bool {
	a, b = slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(a, b)
}

// Consent covers the servers the user was shown and no others, even when the
// document changes between the list and the conversion.
func TestRemoteImagesRefuseUnlistedHosts(t *testing.T) {
	listedURL, listedHits := imageServer(t)
	otherURL, otherHits := imageServer(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.md")
	md := "![a](" + listedURL + "/a.png)\n\n- item\n\n  ![b](" + otherURL + "/b.png)\n"
	if err := os.WriteFile(src, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	listed := []string{strings.TrimPrefix(listedURL, "http://")}

	for _, name := range []string{"out.pdf", "out.docx"} {
		opts := Options{RemoteImages: true, RemoteHosts: listed}
		if err := ConvertFileWith(src, filepath.Join(dir, name), FormatUnknown, opts); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := listedHits.Load(); n != 2 {
		t.Errorf("the listed server got %d requests, want 2", n)
	}
	if n := otherHits.Load(); n != 0 {
		t.Errorf("the server that was not listed got %d requests", n)
	}

	// RemoteImages without any host allows nothing
	listedHits.Store(0)
	if err := ConvertFileWith(src, filepath.Join(dir, "none.pdf"), FormatUnknown, Options{RemoteImages: true}); err != nil {
		t.Fatal(err)
	}
	if n := listedHits.Load() + otherHits.Load(); n != 0 {
		t.Errorf("converting with RemoteImages and no hosts made %d requests", n)
	}
}

// A listed server cannot send the download to a server that was not listed.
func TestRemoteImagesRefuseRedirectsToUnlistedHosts(t *testing.T) {
	targetURL, targetHits := imageServer(t)
	friendlyURL, friendlyHits := imageServer(t)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-target.png":
			http.Redirect(w, r, targetURL+"/a.png", http.StatusFound)
		case "/to-friend.png":
			http.Redirect(w, r, friendlyURL+"/a.png", http.StatusFound)
		}
	}))
	t.Cleanup(redirector.Close)

	dir := t.TempDir()
	src := filepath.Join(dir, "doc.md")
	md := "![a](" + redirector.URL + "/to-target.png)\n"
	if err := os.WriteFile(src, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	redirectorHost := strings.TrimPrefix(redirector.URL, "http://")

	if err := ConvertFileWith(src, filepath.Join(dir, "out.pdf"), FormatUnknown,
		Options{RemoteImages: true, RemoteHosts: []string{redirectorHost}}); err != nil {
		t.Fatal(err)
	}
	if n := targetHits.Load(); n != 0 {
		t.Errorf("a redirect to a server that was not listed was followed (%d requests)", n)
	}

	// A redirect to a listed server is followed
	md = "![a](" + redirector.URL + "/to-friend.png)\n"
	if err := os.WriteFile(src, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	hosts := []string{redirectorHost, strings.TrimPrefix(friendlyURL, "http://")}
	if err := ConvertFileWith(src, filepath.Join(dir, "out2.pdf"), FormatUnknown,
		Options{RemoteImages: true, RemoteHosts: hosts}); err != nil {
		t.Fatal(err)
	}
	if n := friendlyHits.Load(); n != 1 {
		t.Errorf("a redirect to a listed server got %d requests, want 1", n)
	}
}

// A panic in a conversion comes back as an error that says what to do.
func TestConvertFileWithReturnsPanicsAsErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.md")
	// Material's snippets that cannot be read are reported through Warn
	if err := os.WriteFile(src, []byte("--8<-- \"missing.md\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fl, ok := flavor.ByID("mkdocs")
	if !ok {
		t.Fatal("no mkdocs flavor")
	}
	opts := Options{Flavor: fl, Warn: func(string) { panic("boom") }}
	err := ConvertFileWith(src, filepath.Join(dir, "out.pdf"), FormatUnknown, opts)

	var ce *ConversionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want a *ConversionError", err)
	}
	if !errors.Is(err, ErrInternal) {
		t.Errorf("err = %v, want ErrInternal", err)
	}
	for _, want := range []string{"internal error", "boom", "report"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q is not in %q", want, err)
		}
	}
}

// recoverInternal is what keeps RemoteImageHosts from crashing the program.
func TestRecoverInternal(t *testing.T) {
	f := func() (err error) {
		defer recoverInternal("list", &err)
		panic("a bug")
	}
	err := f()
	if !errors.Is(err, ErrInternal) || !strings.Contains(err.Error(), "a bug") {
		t.Errorf("err = %v", err)
	}
}
