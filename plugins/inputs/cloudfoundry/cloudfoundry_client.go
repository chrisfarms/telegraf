package cloudfoundry

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"code.cloudfoundry.org/go-loggregator/v8"
	"code.cloudfoundry.org/go-loggregator/v8/rpc/loggregator_v2"
	"github.com/cloudfoundry-community/go-cfclient"
	"github.com/influxdata/telegraf"
	"golang.org/x/oauth2"
)

type CloudfoundryClient interface {
	Stream(ctx context.Context, req *loggregator_v2.EgressBatchRequest) loggregator.EnvelopeStream
	ListApps() ([]cfclient.App, error)
}

type ClientConfig struct {
	GatewayAddress string `toml:"gateway_address"`
	APIAddress     string `toml:"api_address"`
	Username       string `toml:"username"`
	Password       string `toml:"password"`
	ClientID       string `toml:"client_id"`
	ClientSecret   string `toml:"client_secret"`
	Token          string `toml:"token"`
	TLSSkipVerify  bool   `toml:"insecure_skip_verify"`
}

type Client struct {
	Log telegraf.Logger
	*loggregator.RLPGatewayClient
	*cfclient.Client
}

func NewClient(cfg ClientConfig, logger telegraf.Logger) (CloudfoundryClient, error) {
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
			cfg.GatewayAddress,
			loggregator.WithRLPGatewayHTTPClient(&HTTPClient{
				tokenSource: cfClient.Config.TokenSource,
				client: &http.Client{
					Transport: &transport,
				},
			}),
		),
	}
	return c, nil
}

type HTTPClient struct {
	tokenSource oauth2.TokenSource
	client      *http.Client
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	token, err := c.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %s", err)
	}

	authHeader := fmt.Sprintf("bearer %s", token.AccessToken)
	req.Header.Set("Authorization", authHeader)

	return c.client.Do(req)
}
