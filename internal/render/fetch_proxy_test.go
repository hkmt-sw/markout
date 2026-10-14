package render

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// noProxy makes the downloads of a test go direct, whatever the environment
// of the machine says.
func noProxy(t *testing.T) {
	t.Helper()
	old := proxyFunc
	t.Cleanup(func() { proxyFunc = old })
	proxyFunc = func(*http.Request) (*url.URL, error) { return nil, nil }
}

// forwardProxy starts a proxy on 127.0.0.1 that answers every request with a
// PNG, or with a redirect for /to-<host>, and makes the downloads of the test
// go through it. It returns the number of requests it got.
func forwardProxy(t *testing.T) *atomic.Int32 {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	requests := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if name, ok := strings.CutPrefix(r.URL.Path, "/to-"); ok {
			http.Redirect(w, r, "http://"+name+"/img.png", http.StatusFound)
			return
		}
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	proxyURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	old := proxyFunc
	t.Cleanup(func() { proxyFunc = old })
	proxyFunc = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	return requests
}

// resolveAs makes the names of a test resolve to the given addresses; a name
// that is not in the map does not resolve at all.
func resolveAs(t *testing.T, names map[string][]string) {
	t.Helper()
	old := lookupAddrs
	t.Cleanup(func() { lookupAddrs = old })
	lookupAddrs = func(_ context.Context, host string) ([]netip.Addr, error) {
		list, ok := names[host]
		if !ok {
			return nil, errors.New("no such host")
		}
		var addrs []netip.Addr
		for _, s := range list {
			addrs = append(addrs, netip.MustParseAddr(s))
		}
		return addrs, nil
	}
}

// warnings collects the warnings of a conversion.
type warnings struct {
	mu   sync.Mutex
	list []string
}

func (w *warnings) add(m string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = append(w.list, m)
}

func TestProxiedFetchRefusesNameAtNonPublicAddress(t *testing.T) {
	requests := forwardProxy(t)
	resolveAs(t, map[string][]string{
		"pics.test":  {"127.0.0.1"},
		"mixed.test": {"93.184.215.14", "10.0.0.5"},
		"v6.test":    {"fd00::1"},
	})
	for _, host := range []string{"pics.test", "mixed.test", "v6.test"} {
		opts := Options{RemoteImages: true, RemoteHosts: []string{host}}
		_, err := fetchRemoteImage("http://"+host+"/img.png", opts)
		if !errors.Is(err, ErrRemoteAddressNotAllowed) {
			t.Errorf("%s: err = %v, want ErrRemoteAddressNotAllowed", host, err)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("the proxy got %d requests", n)
	}
}

func TestProxiedFetchSendsPublicNameThroughProxy(t *testing.T) {
	requests := forwardProxy(t)
	resolveAs(t, map[string][]string{"pics.test": {"93.184.215.14"}})
	var w warnings
	opts := Options{RemoteImages: true, RemoteHosts: []string{"pics.test"}, Warn: w.add}
	data, err := fetchRemoteImage("http://pics.test/img.png", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || requests.Load() != 1 {
		t.Errorf("got %d bytes and %d requests at the proxy", len(data), requests.Load())
	}
	if len(w.list) != 0 {
		t.Errorf("warnings: %q", w.list)
	}
}

// Where only the proxy can resolve names the request goes to the proxy, and
// the user is told once for the server, however many images it has.
func TestProxiedFetchWarnsOnceWhenNameCannotBeChecked(t *testing.T) {
	requests := forwardProxy(t)
	resolveAs(t, nil)
	var w warnings
	opts := Options{RemoteImages: true, RemoteHosts: []string{"pics.test", "other.test"}, Warn: w.add}.withFetchState()
	for _, u := range []string{"http://pics.test/img.png", "http://pics.test/img.png", "http://other.test/img.png"} {
		if _, err := fetchRemoteImage(u, opts); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 3 {
		t.Errorf("the proxy got %d requests, want 3", requests.Load())
	}
	want := []string{
		"the address of pics.test cannot be checked here; the proxy resolves it",
		"the address of other.test cannot be checked here; the proxy resolves it",
	}
	if !slices.Equal(w.list, want) {
		t.Errorf("warnings = %q, want %q", w.list, want)
	}
}

func TestProxiedFetchChecksRedirectTargets(t *testing.T) {
	requests := forwardProxy(t)
	resolveAs(t, map[string][]string{
		"pics.test":    {"93.184.215.14"},
		"private.test": {"10.0.0.5"},
	})
	opts := Options{RemoteImages: true, RemoteHosts: []string{"pics.test", "private.test"}}
	_, err := fetchRemoteImage("http://pics.test/to-private.test", opts)
	if !errors.Is(err, ErrRemoteAddressNotAllowed) {
		t.Errorf("err = %v, want ErrRemoteAddressNotAllowed", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("the proxy got %d requests, want 1 (the redirect only)", n)
	}
}

// A server shown as local is not looked up, and the environment decides if the
// proxy is used.
func TestProxiedFetchDoesNotCheckLocalServers(t *testing.T) {
	requests := forwardProxy(t)
	old := lookupAddrs
	t.Cleanup(func() { lookupAddrs = old })
	lookupAddrs = func(context.Context, string) ([]netip.Addr, error) {
		t.Error("a local server was looked up")
		return nil, errors.New("unexpected")
	}
	var w warnings
	opts := Options{RemoteImages: true, RemoteHosts: []string{"printer.local", "wiki"}, Warn: w.add}
	for _, u := range []string{"http://printer.local/img.png", "http://wiki/img.png"} {
		if _, err := fetchRemoteImage(u, opts); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 || len(w.list) != 0 {
		t.Errorf("requests = %d, warnings = %q", requests.Load(), w.list)
	}
}
