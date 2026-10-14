package render

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
)

// ErrRemoteAddressNotAllowed is returned when an image server that was not
// shown to the user as a local one is at an address that is not public: its
// name resolves to this machine, the local network or another reserved range.
// It also matches ErrRemoteHostNotAllowed.
var ErrRemoteAddressNotAllowed = fmt.Errorf("%w: the server is at a non-public address", ErrRemoteHostNotAllowed)

// checkRemoteHost refuses a URL that is not an http(s) address on one of the
// servers the user agreed to.
func (o Options) checkRemoteHost(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %q is not an http(s) address", ErrRemoteHostNotAllowed, u.Scheme)
	}
	host := strings.ToLower(u.Host)
	if host == "" || !slices.ContainsFunc(o.RemoteHosts, func(h string) bool { return strings.ToLower(h) == host }) {
		return fmt.Errorf("%w: %s", ErrRemoteHostNotAllowed, u.Host)
	}
	return nil
}

// fetchRemoteImage downloads the image at rawURL, if the user agreed to
// downloads from its server. A redirect is followed only to a server the user
// agreed to as well: a server that was shown to the user must not be able to
// send the download to an address on the local network that was not.
//
// The name of a server says little about where it is, so the address the
// download connects to is checked as well; see newImageClient.
func fetchRemoteImage(rawURL string, opts Options) ([]byte, error) {
	if !opts.RemoteImages {
		return nil, ErrRemoteImageSkipped
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if err := opts.checkRemoteHost(req.URL); err != nil {
		return nil, err
	}
	client := newImageClient()
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return opts.checkRemoteHost(next.URL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image: status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes))
}

// dialTarget gives the address a connection to addr ("host:port") is made to.
// Tests replace it to point a name at a local server; it is the identity
// otherwise, so the system resolver decides.
var dialTarget = func(addr string) string { return addr }

// newImageClient makes the HTTP client an image download uses. Its dialer
// looks at the address each connection is really made to, right before the
// connect, so a name that resolves to a non-public address, or to a different
// one on a later lookup, or a redirect that does, is refused whatever the
// name looks like. A server the user was shown as local (IsLocalHost, which
// is what the list of servers marks) is exempt: that is the case where the
// user agreed to a download from this machine or the local network.
//
// The environment proxy (HTTP_PROXY and the like) is not used. With one, the
// connection goes to the proxy and the proxy resolves the server, so the
// address could not be checked here, and the download would reach whatever
// the proxy can reach. Image downloads connect directly instead.
func newImageClient() *http.Client {
	checked := &net.Dialer{Timeout: 10 * time.Second, Control: refuseNonPublic}
	local := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := checked
			if IsLocalHost(addr) {
				d = local
			}
			return d.DialContext(ctx, network, dialTarget(addr))
		},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        2,
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport}
}

// refuseNonPublic is a net.Dialer Control function: it sees the resolved
// address of each connection attempt and refuses one that is not public.
func refuseNonPublic(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrRemoteAddressNotAllowed, address)
	}
	if !isPublicAddr(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrRemoteAddressNotAllowed, ap.Addr())
	}
	return nil
}

var (
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")

	// Ranges that the netip Is* methods do not cover and that are not public.
	nonPublicPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
		netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT
		netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
		netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
		netip.MustParsePrefix("240.0.0.0/4"),    // reserved, with the broadcast address
		netip.MustParsePrefix("fec0::/10"),      // site-local, deprecated
		netip.MustParsePrefix("100::/64"),       // discard-only
		netip.MustParsePrefix("2001::/32"),      // Teredo, which carries an IPv4 address
		netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
	}
)

// isPublicAddr reports whether a is an address on the public internet: not
// loopback, private, link-local, unique-local, carrier-grade NAT, unspecified,
// multicast or otherwise reserved. An IPv4 address carried in an IPv6 one
// (IPv4-mapped, NAT64, 6to4) counts as the IPv4 address.
func isPublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() {
		return false
	}
	if a.Is6() {
		b := a.As16()
		switch {
		case nat64Prefix.Contains(a):
			return isPublicAddr(netip.AddrFrom4([4]byte(b[12:16])))
		case sixToFour.Contains(a):
			return isPublicAddr(netip.AddrFrom4([4]byte(b[2:6])))
		}
	}
	if a.IsUnspecified() || a.IsLoopback() || a.IsPrivate() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() {
		return false
	}
	return !slices.ContainsFunc(nonPublicPrefixes, func(p netip.Prefix) bool { return p.Contains(a) })
}

// IsLocalHost reports whether a URL host, as RemoteHostOf names it, is this
// machine or an address that is only reachable on the local network. It looks
// at the name alone and never resolves it: a literal address is local if it
// is not public, and a name is local if it has no dot or ends in a suffix
// that only the local network uses. This is what the user is shown as a local
// network address before agreeing to a download, and it is also the only kind
// of server an image download may connect to at a non-public address.
func IsLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i] // the zone of a link-local address
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		return !isPublicAddr(ip)
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true // a bare name resolves on the local network only
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".lan", ".home", ".home.arpa", ".intranet", ".corp"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
