package dockerrun

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"

	"github.com/docker/docker/client"
)

func (config Config) getDockerClient(ctx context.Context) (*client.Client, error) {
	var httpClient *http.Client = nil
	if config.CaCert != "" && config.Key != "" && config.Cert != "" {
		tlsConfig := &tls.Config{}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM([]byte(config.CaCert))
		tlsConfig.RootCAs = caCertPool

		keyPair, err := tls.X509KeyPair([]byte(config.Cert), []byte(config.Key))
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{keyPair}
		transport := &http.Transport{TLSClientConfig: tlsConfig}
		httpClient = &http.Client{
			Transport: transport,
		}
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost(config.Host),
		client.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, err
	}
	cli.NegotiateAPIVersion(ctx)

	return cli, nil
}
