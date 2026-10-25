package convert

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Canceling the context stops a download that is waiting for the server,
// and no output is written, for either format.
func TestConvertCancelStopsDownload(t *testing.T) {
	started := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(in, []byte("# Hi\n\n![x]("+srv.URL+"/img.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"doc.pdf", "doc.docx"} {
		out := filepath.Join(dir, name)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-started
			cancel()
		}()
		begin := time.Now()
		err := ConvertFileWith(in, out, FormatUnknown, Options{RemoteImages: true, RemoteHosts: []string{host}, Context: ctx})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: err = %v, want context.Canceled", name, err)
		}
		if d := time.Since(begin); d > 5*time.Second {
			t.Errorf("%s: took %v after the cancel", name, d)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: output exists after cancel: %v", name, err)
		}
	}
}
