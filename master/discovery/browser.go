package discovery

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const ServiceName = "_ldgcc-worker._tcp"

type Browser interface {
	Scan(ctx context.Context) ([]Worker, error)
}

type MDNSBrowser struct {
	timeout time.Duration
}

func NewBrowser(timeout time.Duration) *MDNSBrowser {
	return &MDNSBrowser{timeout: timeout}
}

func (b *MDNSBrowser) Scan(ctx context.Context) ([]Worker, error) {
	entries := make(chan *mdns.ServiceEntry, 32)
	params := mdns.DefaultParams(ServiceName)
	params.Timeout = b.timeout
	params.Entries = entries
	params.Logger = log.New(io.Discard, "", 0)

	queryDone := make(chan error, 1)
	go func() {
		queryDone <- mdns.QueryContext(ctx, params)
		close(entries)
	}()

	var workers []Worker
	for entry := range entries {
		if !isWorkerEntry(entry) {
			continue
		}
		workers = append(workers, workerFromEntry(entry))
	}

	return workers, <-queryDone
}

func isWorkerEntry(entry *mdns.ServiceEntry) bool {
	return strings.HasSuffix(
		entry.Name,
		"."+ServiceName+".local.",
	)
}

func workerFromEntry(entry *mdns.ServiceEntry) Worker {
	txt := make(map[string]string)
	for _, field := range entry.InfoFields {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			txt[key] = value
		}
	}

	address := firstAddress(entry)
	return Worker{
		Instance: strings.TrimSuffix(
			entry.Name,
			"."+ServiceName+".local.",
		),
		Host:            strings.TrimSuffix(entry.Host, "."),
		Address:         address,
		GRPCPort:        entry.Port,
		ProtocolVersion: txt["protocol_version"],
		PairingStatus:   txt["pairing_status"],
		LastSeen:        time.Now(),
	}
}

func firstAddress(entry *mdns.ServiceEntry) string {
	if entry.AddrV4 != nil {
		return entry.AddrV4.String()
	}
	if entry.AddrV6IPAddr != nil {
		return entry.AddrV6IPAddr.String()
	}
	if entry.AddrV6 != nil {
		return net.IP(entry.AddrV6).String()
	}
	return ""
}

func Address(worker Worker) string {
	return net.JoinHostPort(
		worker.Address,
		strconv.Itoa(worker.GRPCPort),
	)
}
