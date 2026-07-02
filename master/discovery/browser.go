package discovery

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const ServiceName = "_ldgcc-worker._tcp"
const localWorkerAppPort = 5050

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
		worker := workerFromEntry(entry)
		if worker.Address == "" {
			continue
		}
		workers = append(workers, worker)
	}

	err := <-queryDone
	return mergeWorkers(workers, localWorkerFallback(ctx)...), err
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
	instance := strings.TrimSuffix(
		entry.Name,
		"."+ServiceName+".local.",
	)
	return Worker{
		ID:              ID(instance, address, entry.Port),
		Instance:        instance,
		Host:            strings.TrimSuffix(entry.Host, "."),
		Address:         address,
		GRPCPort:        entry.Port,
		ProtocolVersion: txt["protocol_version"],
		PairingStatus:   txt["pairing_status"],
		LastSeen:        time.Now(),
	}
}

func firstAddress(entry *mdns.ServiceEntry) string {
	if entry.AddrV4 != nil && isUsableDiscoveryIP(entry.AddrV4) {
		return entry.AddrV4.String()
	}
	if ip := resolveEntryHost(entry.Host); ip != "" {
		return ip
	}
	if entry.AddrV6IPAddr != nil && isUsableDiscoveryIP(entry.AddrV6IPAddr.IP) {
		return entry.AddrV6IPAddr.String()
	}
	if entry.AddrV6 != nil {
		ip := net.IP(entry.AddrV6)
		if isUsableDiscoveryIP(ip) {
			return ip.String()
		}
	}
	return ""
}

func resolveEntryHost(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" {
		return ""
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil && isUsableDiscoveryIP(ipv4) {
			return ipv4.String()
		}
	}
	for _, ip := range ips {
		if isUsableDiscoveryIP(ip) {
			return ip.String()
		}
	}
	return ""
}

func isUsableDiscoveryIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast()
}

func Address(worker Worker) string {
	return net.JoinHostPort(
		worker.Address,
		strconv.Itoa(worker.GRPCPort),
	)
}

func ID(instance, address string, port int) string {
	return instance + "@" + net.JoinHostPort(address, strconv.Itoa(port))
}

type localWorkerAppState struct {
	Running    bool   `json:"running"`
	Connection string `json:"connection"`
	Config     struct {
		WorkerName string `json:"worker_name"`
		GRPCPort   string `json:"grpc_port"`
	} `json:"config"`
}

func localWorkerFallback(ctx context.Context) []Worker {
	worker, ok := localWorkerFromApp(ctx, "127.0.0.1", localWorkerAppPort)
	if !ok {
		return nil
	}
	return []Worker{worker}
}

func localWorkerFromApp(ctx context.Context, host string, appPort int) (Worker, bool) {
	requestCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	url := "http://" + net.JoinHostPort(host, strconv.Itoa(appPort)) + "/api/state"
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return Worker{}, false
	}

	response, err := (&http.Client{Timeout: 300 * time.Millisecond}).Do(request)
	if err != nil {
		return Worker{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Worker{}, false
	}

	var state localWorkerAppState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		return Worker{}, false
	}
	return localWorkerFromState(state, host)
}

func localWorkerFromState(state localWorkerAppState, host string) (Worker, bool) {
	if !state.Running {
		return Worker{}, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(state.Config.GRPCPort))
	if err != nil || port <= 0 || port > 65535 {
		return Worker{}, false
	}
	instance := strings.TrimSpace(state.Config.WorkerName)
	if instance == "" {
		instance = "LDGCC Local Worker"
	}
	return Worker{
		ID:            ID(instance, host, port),
		Instance:      instance,
		Host:          "localhost",
		Address:       host,
		GRPCPort:      port,
		PairingStatus: localPairingStatus(state.Connection),
		LastSeen:      time.Now(),
	}, true
}

func localPairingStatus(connection string) string {
	if strings.HasPrefix(connection, "PAIRED_") {
		return "paired"
	}
	return "unpaired"
}

func mergeWorkers(workers []Worker, extras ...Worker) []Worker {
	seen := make(map[string]struct{}, len(workers)+len(extras))
	merged := make([]Worker, 0, len(workers)+len(extras))
	for _, worker := range workers {
		key := worker.Instance + "|" + strconv.Itoa(worker.GRPCPort)
		seen[key] = struct{}{}
		merged = append(merged, worker)
	}
	for _, worker := range extras {
		key := worker.Instance + "|" + strconv.Itoa(worker.GRPCPort)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, worker)
	}
	return merged
}
