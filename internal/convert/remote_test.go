package convert

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// imageServer serves a small PNG and counts the requests it receives.
func imageServer(t *testing.T) (url string, hits *atomic.Int32) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	hits = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv.URL, hits
}

func TestRemoteImagesNeedConsent(t *testing.T) {
	url, hits := imageServer(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "doc.md")
	md := "# Doc\n\n![one](" + url + "/a.png)\n\ntext\n\n![two](" + url + "/b.png)\n\n![three](https://img.example.com/c.png)\n\n![local](pic.png)\n"
	if err := os.WriteFile(src, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	// Listing the hosts names them without contacting anyone.
	hosts, err := RemoteImageHosts(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v, want the test server and img.example.com", hosts)
	}
	if hosts[0].Images != 2 || !hosts[0].Local {
		t.Errorf("test server entry = %+v, want 2 images on a local address", hosts[0])
	}
	if hosts[1].Host != "img.example.com" || hosts[1].Images != 1 || hosts[1].Local {
		t.Errorf("second entry = %+v", hosts[1])
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("listing the hosts made %d requests", n)
	}

	// Without consent a conversion fetches nothing, in either format.
	for _, name := range []string{"out.docx", "out.pdf"} {
		if err := ConvertFileWith(src, filepath.Join(dir, name), FormatUnknown, Options{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("converting without consent made %d requests", n)
	}

	// With consent the images on the test server are downloaded.
	if err := ConvertFileWith(src, filepath.Join(dir, "with.docx"), FormatUnknown, Options{RemoteImages: true}); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 2 {
		t.Fatalf("converting with consent made %d requests, want 2", n)
	}
}

func TestIsLocalHost(t *testing.T) {
	local := []string{"localhost", "localhost:8080", "127.0.0.1", "127.0.0.1:3000", "10.1.2.3", "192.168.1.10",
		"172.16.0.5", "169.254.169.254", "[::1]", "[::1]:8080", "[fe80::1]", "0.0.0.0", "intranet", "printer.local",
		"db.internal", "wiki.corp"}
	for _, h := range local {
		if !isLocalHost(h) {
			t.Errorf("isLocalHost(%q) = false, want true", h)
		}
	}
	public := []string{"github.com", "img.shields.io:443", "8.8.8.8", "172.32.0.1", "[2606:4700::1111]", "example.localdomain.com"}
	for _, h := range public {
		if isLocalHost(h) {
			t.Errorf("isLocalHost(%q) = true, want false", h)
		}
	}
}
