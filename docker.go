package dockerrun

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

func (config Config) getDockerClient() (*client.Client, error) {
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

	cli, err := client.NewClient(config.Host, "", httpClient, make(map[string]string))
	if err != nil {
		return nil, err
	}

	return cli, nil
}

func (config Config) pullImage(cli *client.Client, ctx context.Context) error {
	image := config.Config.ContainerConfig.Image
	_, err := reference.ParseNamed(config.Config.ContainerConfig.Image)
	if err != nil {
		if errors.Is(err, reference.ErrNameNotCanonical) {
			if !strings.Contains(config.Config.ContainerConfig.Image, "/") {
				image = "docker.io/library/" + image
			} else {
				image = "docker.io/" + image
			}
		} else {
			return err
		}
	}

	pullReader, err := cli.ImagePull(ctx, image, types.ImagePullOptions{})
	if err != nil {
		return err
	}
	_, err = ioutil.ReadAll(pullReader)
	if err != nil {
		return err
	}
	err = pullReader.Close()
	if err != nil {
		return err
	}
	return nil
}
