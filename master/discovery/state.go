package discovery

import "time"

type Worker struct {
	Instance        string
	Host            string
	Address         string
	GRPCPort        int
	ProtocolVersion string
	PairingStatus   string
	LastSeen        time.Time
}
