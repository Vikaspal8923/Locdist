package pairing

import (
	"net"
	"strings"
)

func reachableHost(configured string) string {
	host := strings.TrimSpace(configured)
	if host != "" && !isAutoHost(host) && !isVirtualHost(host) {
		return host
	}
	if ip := defaultRouteIPv4(); ip != "" {
		return ip
	}
	if ip := firstLANIPv4(); ip != "" {
		return ip
	}
	return "127.0.0.1"
}

func isAutoHost(host string) bool {
	normalized := strings.ToLower(host)
	if normalized == "" || normalized == "localhost" || normalized == "0.0.0.0" || normalized == "::" || normalized == "::1" {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

func isVirtualHost(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range interfaces {
		if !isVirtualInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if addressHasIP(address, ip) {
				return true
			}
		}
	}
	return false
}

func firstLANIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			ip = ipFromAddress(address)
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func addressHasIP(address net.Addr, target net.IP) bool {
	ip := ipFromAddress(address)
	if ip == nil {
		return false
	}
	return ip.Equal(target)
}

func ipFromAddress(address net.Addr) net.IP {
	switch value := address.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}

func defaultRouteIPv4() string {
	connection, err := net.Dial("udp4", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer connection.Close()
	address, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok || address.IP == nil {
		return ""
	}
	ip := address.IP.To4()
	if ip == nil || ip.IsLoopback() {
		return ""
	}
	return ip.String()
}

func isVirtualInterface(name string) bool {
	normalized := strings.ToLower(name)
	virtualPrefixes := []string{"docker", "br-", "veth", "virbr", "vmnet", "vbox", "zt", "tailscale", "tun", "tap"}
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	virtualMarkers := []string{"virtualbox", "vmware", "hyper-v", "wsl", "npcap", "loopback", "host-only"}
	for _, marker := range virtualMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
