package masterclient

import (
	"context"

	gradient "github.com/Vikaspal8923/Locdist/worker/generated/gradient"
	"github.com/Vikaspal8923/Locdist/worker/internal/config"

	grpcclient "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpcclient.ClientConn
	client gradient.WorkerBridgeClient
}

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
