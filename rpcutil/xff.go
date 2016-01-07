package rpcutil

import (
	"net"
	"net/http"
	"strings"
)

func mustGetCIDRNetwork(cidr string) *net.IPNet {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return n
}

var internalCIDRs4 = []*net.IPNet{
	mustGetCIDRNetwork("10.0.0.0/8"),
	mustGetCIDRNetwork("172.16.0.0/12"),
	mustGetCIDRNetwork("192.168.0.0/16"),
	mustGetCIDRNetwork("169.254.0.0/16"),
}

var internalCIDRs6 = []*net.IPNet{
	mustGetCIDRNetwork("fd00::/8"),
}

func ipIsPrivate(ip net.IP) bool {
	cidrs := internalCIDRs4
	if ip.To4() == nil {
		cidrs = internalCIDRs6
	}
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// RequestIP returns the string form of the original requester's IP address for
// the given request, taking into account X-Forwarded-For if applicable.
func RequestIP(r *http.Request) string {
	reqIP, _, _ := net.SplitHostPort(r.RemoteAddr)

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return reqIP
	}

	ips := strings.Split(xff, ",")
	for i := range ips {
		ip := net.ParseIP(strings.TrimSpace(ips[i]))
		if ip == nil || ip.IsLoopback() || ipIsPrivate(ip) {
			continue
		}
		reqIP = ip.String()
		break
	}

	return reqIP
}
