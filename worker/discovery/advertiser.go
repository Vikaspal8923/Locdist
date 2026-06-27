package discovery

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"

	"github.com/hashicorp/mdns"
)

const Service = "_ldgcc-worker._tcp"

type Metadata struct {
	Name            string
	Host            string
	Port            string
	ProtocolVersion uint32
	PairingStatus   string
}

type Advertiser interface {
	Start(metadata Metadata) error
	Stop() error
}

type MDNSAdvertiser struct {
	mu     sync.Mutex
	server *mdns.Server
}

func NewAdvertiser() *MDNSAdvertiser {
	return &MDNSAdvertiser{}
}

func (a *MDNSAdvertiser) Start(metadata Metadata) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server != nil {
		return fmt.Errorf("worker discovery is already running")
	}
	if metadata.Name == "" {
		return fmt.Errorf("worker name is required")
	}

	port, err := strconv.Atoi(metadata.Port)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid worker grpc port %q", metadata.Port)
	}

	var ips []net.IP
	if ip := net.ParseIP(metadata.Host); ip != nil {
		ips = []net.IP{ip}
	}

	service, err := mdns.NewMDNSService(
		metadata.Name,
		Service,
		"",
		"",
		port,
		ips,
		[]string{
			fmt.Sprintf("protocol_version=%d", metadata.ProtocolVersion),
			"pairing_status=" + metadata.PairingStatus,
		},
	)
	if err != nil {
		return fmt.Errorf("create discovery service: %w", err)
	}

	server, err := mdns.NewServer(
		&mdns.Config{
			Zone:   service,
			Logger: log.New(io.Discard, "", 0),
		},
	)
	if err != nil {
		return fmt.Errorf("start discovery service: %w", err)
	}

	a.server = server
	return nil
}

func (a *MDNSAdvertiser) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.server == nil {
		return nil
	}

	err := a.server.Shutdown()
	a.server = nil
	return err
}
