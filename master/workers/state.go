package workers

import gradient "github.com/Vikaspal8923/Locdist/master/generated/gradient"

type State struct {
	WorkerID string
	Host     string
	GRPCPort string
	Status   gradient.WorkerStatus
	JobID    string
}
