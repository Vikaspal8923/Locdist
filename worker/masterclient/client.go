package masterclient

import (
	"context"
	"time"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"

	grpcclient "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpcclient.ClientConn
	client gradient.WorkerBridgeClient
}

const controlRPCTimeout = 5 * time.Second

func New(
	cfg config.Config,
) (*Client, error) {

	address :=
		cfg.MasterHost +
			":" +
			cfg.MasterPort

	conn, err := grpcclient.NewClient(
		address,
		grpcclient.WithTransportCredentials(
			insecure.NewCredentials(),
		),
	)
	if err != nil {
		return nil, err
	}

	client := gradient.NewWorkerBridgeClient(
		conn,
	)

	return &Client{
		conn:   conn,
		client: client,
	}, nil
}

func (c *Client) Register(
	request *gradient.RegisterWorkerRequest,
) (*gradient.RegisterWorkerResponse, error) {

	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlRPCTimeout,
	)
	defer cancel()

	return c.client.RegisterWorker(ctx, request)
}

func (c *Client) UpdateStatus(
	request *gradient.WorkerStatusUpdate,
) (*gradient.WorkerStatusResponse, error) {

	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlRPCTimeout,
	)
	defer cancel()

	return c.client.UpdateWorkerStatus(ctx, request)
}

func (c *Client) Unpair(
	request *gradient.UnpairWorkerRequest,
) (*gradient.UnpairWorkerResponse, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlRPCTimeout,
	)
	defer cancel()

	return c.client.UnpairWorker(ctx, request)
}

func (c *Client) Heartbeat(
	request *gradient.WorkerHeartbeat,
) (*gradient.WorkerHeartbeatResponse, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlRPCTimeout,
	)
	defer cancel()
	return c.client.Heartbeat(ctx, request)
}

func (c *Client) GoingOffline(
	request *gradient.WorkerOfflineRequest,
) (*gradient.WorkerOfflineResponse, error) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		controlRPCTimeout,
	)
	defer cancel()
	return c.client.GoingOffline(ctx, request)
}

func (c *Client) Synchronize(
	request *gradient.GradientSubmission,
) (*gradient.AggregatedGradientResponse, error) {

	return c.client.SynchronizeGradients(
		context.Background(),
		request,
	)
}

func (c *Client) Close() error {
	return c.conn.Close()
}
