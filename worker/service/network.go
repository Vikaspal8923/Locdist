package service

import (
	"net"
	"os"
	"strings"
)

func advertisedName(configured string) string {
	name := strings.TrimSpace(configured)
	if name != "" && name != "LDGCC Worker" {
		return name
	}
	hostname, err := os.Hostname()
	if err == nil && strings.TrimSpace(hostname) != "" {
		return "LDGCC Worker - " + strings.TrimSpace(hostname)
	}
	return "LDGCC Worker"
}

func advertisedHost(configured string) string {
	host := strings.TrimSpace(configured)
	if host != "" && !isAutoHost(host) {
		return host
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

func firstLANIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}
