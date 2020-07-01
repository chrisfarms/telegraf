package cloudfoundry

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8"
	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/cloudfoundry-community/go-cfclient"
	"github.com/influxdata/telegraf"
)

type CloudfoundryClient interface {
	Stream(ctx context.Context, req *loggregator_v2.EgressBatchRequest) loggregator.EnvelopeStream
	ListApps() ([]cfclient.App, error)
}

type ClientConfig struct {
	GatewayAddresss string `toml:"gateway_address"`
	APIAddress      string `toml:"api_address"`
	Username        string `toml:"username"`
	Password        string `toml:"password"`
	ClientID        string `toml:"client_id"`
	ClientSecret    string `toml:"client_secret"`
	Token           string `toml:"token"`
	TLSSkipVerify   bool   `toml:"insecure_skip_verify"`
}

type Client struct {
	Log telegraf.Logger
	*loggregator.RLPGatewayClient
	*cfclient.Client
}

func NewClient(cfg ClientConfig, logger telegraf.Logger) (*Client, error) {
	errs := make(chan error)
	cfClient, err := cfclient.NewClient(&cfclient.Config{
		ApiAddress:        cfg.APIAddress,
		Username:          cfg.Username,
		Password:          cfg.Password,
		ClientID:          cfg.ClientID,
		ClientSecret:      cfg.ClientSecret,
		SkipSslValidation: cfg.TLSSkipVerify,
	})
	if err != nil {
		return nil, err
	}
	transport := http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify},
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
	}
	c := &Client{
		Client: cfClient,
		RLPGatewayClient: loggregator.NewRLPGatewayClient(
			cfg.GatewayAddresss,
			loggregator.WithRLPGatewayHTTPClient(&HTTPClient{
				tokenSource: cfClient.Config.TokenSource,
				client: &http.Client{
					Transport: &transport,
				},
			}),
			loggregator.WithRLPGatewayErrChan(errs),
		),
	}
	go func() {
		for err := range errs {
			logger.Debugf("rlp error: %s", err)
		}
	}()
	return c, nil
}
