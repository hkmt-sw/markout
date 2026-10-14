package render

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

// imageServerAt starts a server on 127.0.0.1 that serves a PNG at /img.png and
// redirects /to-<name> to http://<name>:<port>/img.png. Every name in names
// resolves to it: dialTarget points them at the server's address, the way a
// name that resolves to a loopback address does. It returns the port and the
// number of requests that reached the image.
func imageServerAt(t *testing.T, names ...string) (port string, hits *atomic.Int32) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	hits = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/img.png":
			hits.Add(1)
			_, _ = w.Write(buf.Bytes())
		case strings.HasPrefix(r.URL.Path, "/to-"):
			http.Redirect(w, r, "http://"+strings.TrimPrefix(r.URL.Path, "/to-")+"/img.png", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	_, port, _ = net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))

	noProxy(t)
	old := dialTarget
	t.Cleanup(func() { dialTarget = old })
	dialTarget = func(addr string) string {
		host, p, err := net.SplitHostPort(addr)
		if err == nil && p == port {
			for _, n := range names {
				if host == n {
					return "127.0.0.1:" + port
				}
			}
		}
		return addr
	}
	return port, hits
}

// A name that looks public but resolves to a loopback address is refused, and
// the server sees no request.
func TestFetchRefusesPublicNameAtLoopbackAddress(t *testing.T) {
	port, hits := imageServerAt(t, "public.test")
	host := "public.test:" + port
	opts := Options{RemoteImages: true, RemoteHosts: []string{host}}

	_, err := fetchRemoteImage("http://"+host+"/img.png", opts)
	if !errors.Is(err, ErrRemoteAddressNotAllowed) {
		t.Fatalf("err = %v, want ErrRemoteAddressNotAllowed", err)
	}
	if !errors.Is(err, ErrRemoteHostNotAllowed) {
		t.Errorf("err = %v does not match ErrRemoteHostNotAllowed", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("the server got %d requests", n)
	}
}

// The same server, listed to the user as local, is downloaded from: by a
// local name and by its literal address.
func TestFetchAllowsServerShownAsLocal(t *testing.T) {
	port, hits := imageServerAt(t, "printer.local", "wiki", "localhost")
	for _, host := range []string{"printer.local:" + port, "wiki:" + port, "127.0.0.1:" + port, "localhost:" + port} {
		before := hits.Load()
		opts := Options{RemoteImages: true, RemoteHosts: []string{host}}
		data, err := fetchRemoteImage("http://"+host+"/img.png", opts)
		if err != nil {
			t.Errorf("%s: %v", host, err)
			continue
		}
		if len(data) == 0 || hits.Load() != before+1 {
			t.Errorf("%s: got %d bytes, %d requests", host, len(data), hits.Load()-before)
		}
	}
}

// A redirect to an agreed server that is at a non-public address is refused
// when the server was not shown as local; one that was not agreed to is
// refused by name.
func TestFetchRefusesRedirectToNonPublicAddress(t *testing.T) {
	port, hits := imageServerAt(t, "printer.local", "other.test")
	local := "printer.local:" + port
	other := "other.test:" + port
	opts := Options{RemoteImages: true, RemoteHosts: []string{local, other}}

	before := hits.Load()
	_, err := fetchRemoteImage("http://"+local+"/to-"+other, opts)
	if !errors.Is(err, ErrRemoteAddressNotAllowed) {
		t.Errorf("redirect to a public-looking name at loopback: err = %v, want ErrRemoteAddressNotAllowed", err)
	}
	if hits.Load() != before {
		t.Error("the redirect target was reached")
	}

	_, err = fetchRemoteImage("http://"+local+"/to-127.0.0.1:"+port, opts)
	if !errors.Is(err, ErrRemoteHostNotAllowed) {
		t.Errorf("redirect to an address that was not agreed: err = %v, want ErrRemoteHostNotAllowed", err)
	}
	if hits.Load() != before {
		t.Error("the redirect target was reached")
	}

	// Agreed and shown as local, the same redirect is followed.
	opts.RemoteHosts = []string{local, "127.0.0.1:" + port}
	if _, err := fetchRemoteImage("http://"+local+"/to-127.0.0.1:"+port, opts); err != nil {
		t.Errorf("redirect to an agreed local address: %v", err)
	}
}

func TestIsPublicAddr(t *testing.T) {
	nonPublic := []string{
		"127.0.0.1", "127.255.0.1", "10.0.0.1", "172.16.5.5", "192.168.1.1", "169.254.169.254", "100.64.0.1", "100.127.255.254",
		"0.0.0.0", "0.1.2.3", "224.0.0.1", "255.255.255.255", "240.0.0.1", "192.0.0.8", "198.18.0.1",
		"::", "::1", "fe80::1", "fc00::1", "fd12:3456::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"64:ff9b::7f00:1", "64:ff9b::a00:1", "2002:7f00:1::1", "2002:c0a8:101::", "2001::1", "fec0::1",
	}
	for _, s := range nonPublic {
		if isPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("isPublicAddr(%s) = true, want false", s)
		}
		if !IsLocalHost(s) {
			t.Errorf("IsLocalHost(%s) = false, want true", s)
		}
	}
	public := []string{
		"8.8.8.8", "1.1.1.1", "172.32.0.1", "100.128.0.1", "198.20.0.1", "93.184.216.34",
		"2606:4700::1111", "2001:4860:4860::8888", "::ffff:8.8.8.8", "64:ff9b::808:808", "2002:808:808::",
	}
	for _, s := range public {
		if !isPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("isPublicAddr(%s) = false, want true", s)
		}
		if IsLocalHost(s) {
			t.Errorf("IsLocalHost(%s) = true, want false", s)
		}
	}
	if isPublicAddr(netip.Addr{}) {
		t.Error("the zero address is public")
	}
}

func TestRefuseNonPublic(t *testing.T) {
	for _, a := range []string{"127.0.0.1:80", "[::1]:443", "[fe80::1%eth0]:80", "169.254.169.254:80", "100.64.0.1:80", "[fd00::1]:80"} {
		if err := refuseNonPublic("tcp", a, nil); !errors.Is(err, ErrRemoteAddressNotAllowed) {
			t.Errorf("refuseNonPublic(%s) = %v", a, err)
		}
	}
	for _, a := range []string{"8.8.8.8:80", "[2606:4700::1111]:443"} {
		if err := refuseNonPublic("tcp", a, nil); err != nil {
			t.Errorf("refuseNonPublic(%s) = %v", a, err)
		}
	}
	if err := refuseNonPublic("tcp", "not an address", nil); err == nil {
		t.Error("an unparsable address was accepted")
	}
}
