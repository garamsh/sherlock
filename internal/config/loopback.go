package config

import (
	"net"
	"net/netip"
	"strings"
)

// IsLoopbackAddr reports whether addr binds loopback only. The wildcard
// form (":8080", "0.0.0.0:8080") binds every interface and is not
// loopback; an unparseable address fails safe as non-loopback.
//
// Both listeners gate on it: the gateway refuses a non-loopback bind
// without the tls: section, and the workspace refuses one outright while
// its gRPC listener is unauthenticated (ADR 0012).
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
