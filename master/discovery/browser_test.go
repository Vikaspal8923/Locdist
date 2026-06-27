package discovery

import (
	"testing"

	"github.com/hashicorp/mdns"
)

func TestWorkerEntryFilter(t *testing.T) {
	if !isWorkerEntry(
		&mdns.ServiceEntry{
			Name: "Vikas-Laptop._ldgcc-worker._tcp.local.",
		},
	) {
		t.Fatal("LDGCC Worker entry was rejected")
	}

	if isWorkerEntry(
		&mdns.ServiceEntry{
			Name: "Nearby._nearbypresence._tcp.local.",
		},
	) {
		t.Fatal("unrelated mDNS service was accepted")
	}
}
