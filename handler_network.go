package dockerrun

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/containerssh/log"
	"github.com/containerssh/sshserver"
	"github.com/containerssh/structutils"
	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type networkHandler struct {
	mutex        *sync.Mutex
	client       net.TCPAddr
	username     string
	connectionID string
	config       Config
	containerID  string
	dockerClient *client.Client
	logger       log.Logger
}

type containerError struct {
	ConnectionID string `json:"connectionId"`
	ContainerID  string `json:"containerId"`
	Message      string `json:"message"`
	Error        string `json:"error"`
}

func (n *networkHandler) containerError(message string, err error) containerError {
	return containerError{
		ConnectionID: n.connectionID,
		ContainerID:  n.containerID,
		Message:      message,
		Error:        err.Error(),
	}
}

func (n *networkHandler) OnAuthPassword(_ string, _ []byte) (response sshserver.AuthResponse, reason error) {
	return sshserver.AuthResponseUnavailable, fmt.Errorf("dockerrun does not support authentication")
}

func (n *networkHandler) OnAuthPubKey(_ string, _ string) (response sshserver.AuthResponse, reason error) {
	return sshserver.AuthResponseUnavailable, fmt.Errorf("dockerrun does not support authentication")
}

func (n *networkHandler) OnHandshakeFailed(_ error) {}

func (n *networkHandler) OnHandshakeSuccess(username string) (
	connection sshserver.SSHConnectionHandler,
	failureReason error,
) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	ctx, cancelFunc := context.WithTimeout(context.Background(), n.config.Config.Timeout)
	defer cancelFunc()
	n.username = username

	if err := n.setupDockerClient(); err != nil {
		return nil, err
	}
	if err := n.pullImage(ctx); err != nil {
		return nil, err
	}
	if err := n.createAndStartContainer(ctx); err != nil {
		return nil, err
	}
	return &sshConnectionHandler{
		networkHandler: n,
		username:       username,
	}, nil
}

func (n *networkHandler) pullNeeded(ctx context.Context) (bool, error) {
	image := n.config.Config.ContainerConfig.Image

	switch n.config.Config.ImagePullPolicy {
	case ImagePullPolicyNever:
		return false, nil
	case ImagePullPolicyAlways:
		return true, nil
	default:
		if !strings.Contains(image, ":") || strings.HasSuffix(image, ":latest") {
			return true, nil
		}

		var lastError error
	loop:
		for {
			select {
			case <-ctx.Done():
				break loop
			default:
			}

			if _, _, err := n.dockerClient.ImageInspectWithRaw(ctx, image); err != nil {
				if client.IsErrImageNotFound(err) {
					return true, nil
				}
				n.logger.Warningd(
					n.containerError("failed to list images, retrying in 10 seconds", err),
				)
				lastError = err
				time.Sleep(10 * time.Second)
				continue
			}
			return false, nil
		}
		n.logger.Errord(
			n.containerError("failed to list images, giving up", lastError),
		)

		return true, lastError
	}
}

func (n *networkHandler) pullImage(ctx context.Context) (err error) {
	pullNeeded, err := n.pullNeeded(ctx)
	if err != nil || !pullNeeded {
		return err
	}

	config := n.config
	image := config.Config.ContainerConfig.Image
	image, err = n.getCanonicalImageName(image)
	if err != nil {
		return err
	}

	var lastError error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
		}
		pullReader, err := n.dockerClient.ImagePull(ctx, image, types.ImagePullOptions{})
		if err != nil {
			lastError = err
			n.logger.Warningd(
				n.containerError(fmt.Sprintf("failed to pull image %s, retrying in 10 seconds", image), err),
			)
			time.Sleep(10 * time.Second)
			continue
		}
		_, err = ioutil.ReadAll(pullReader)
		if err != nil {
			lastError = err
			_ = pullReader.Close()
			n.logger.Warningd(
				n.containerError(fmt.Sprintf("failed to pull image %s, retrying in 10 seconds", image), err),
			)
			time.Sleep(10 * time.Second)
			continue
		}
		err = pullReader.Close()
		if err != nil {
			lastError = err
			_ = pullReader.Close()
			n.logger.Warningd(
				n.containerError(fmt.Sprintf("failed to pull image %s, retrying in 10 seconds", image), err),
			)
			time.Sleep(10 * time.Second)
			continue
		}
		break
	}
	if lastError != nil {
		n.logger.Errord(
			n.containerError(fmt.Sprintf("failed to pull image %s, giving up", image), lastError),
		)
	}
	return lastError
}

func (n *networkHandler) getCanonicalImageName(image string) (string, error) {
	_, err := reference.ParseNamed(image)
	if err != nil {
		if errors.Is(err, reference.ErrNameNotCanonical) {
			if !strings.Contains(image, "/") {
				image = "docker.io/library/" + image
			} else {
				image = "docker.io/" + image
			}
		} else {
			return "", err
		}
	}
	return image, nil
}

func (n *networkHandler) createAndStartContainer(ctx context.Context) error {
	if n.containerID == "" {
		containerConfig := n.config.Config.ContainerConfig
		newConfig := container.Config{}
		if err := structutils.Copy(&newConfig, &containerConfig); err != nil {
			return err
		}
		if newConfig.Labels == nil {
			newConfig.Labels = map[string]string{}
		}
		newConfig.Cmd = n.config.Config.IdleCommand
		newConfig.Labels["containerssh_connection_id"] = n.connectionID
		newConfig.Labels["containerssh_ip"] = n.client.IP.String()
		newConfig.Labels["containerssh_username"] = n.username
		err := n.createContainer(ctx, newConfig)
		if err != nil {
			return err
		}

		if err := n.startContainer(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (n *networkHandler) startContainer(ctx context.Context) error {
	var lastError error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
		}
		if err := n.dockerClient.ContainerStart(
			ctx,
			n.containerID,
			types.ContainerStartOptions{},
		); err != nil {
			lastError = err
			n.logger.Warningd(n.containerError("failed to start container, retrying in 10 seconds", err))
			time.Sleep(10 * time.Second)
			continue
		}
		break
	}
	if lastError != nil {
		n.logger.Errord(n.containerError("failed to start container, giving up", lastError))
		return lastError
	}
	return nil
}

func (n *networkHandler) createContainer(ctx context.Context, newConfig container.Config) error {
	var lastError error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		default:
		}
		body, err := n.dockerClient.ContainerCreate(
			ctx,
			&newConfig,
			&n.config.Config.HostConfig,
			&n.config.Config.NetworkConfig,
			n.config.Config.ContainerName,
		)
		if err != nil {
			lastError = err
			n.logger.Warningd(n.containerError("failed to create container, retrying in 10 seconds", err))
			time.Sleep(10 * time.Second)
			continue
		}
		n.containerID = body.ID
		break
	}
	if lastError != nil {
		n.logger.Errord(n.containerError("failed to create container, giving up", lastError))
		return lastError
	}
	return nil
}

func (n *networkHandler) setupDockerClient() error {
	if n.dockerClient == nil {
		dockerClient, err := n.config.getDockerClient()
		if err != nil {
			return fmt.Errorf("failed to create Docker client (%w)", err)
		}
		n.dockerClient = dockerClient
	}
	return nil
}

func (n *networkHandler) OnDisconnect() {
	ctx, cancelFunc := context.WithTimeout(context.Background(), n.config.Config.Timeout)
	defer cancelFunc()
	n.mutex.Lock()
	if n.containerID != "" {
		success := false
		var lastError error
	loop:
		for {
			n.mutex.Unlock()
			n.mutex.Lock()
			select {
			case <-ctx.Done():
				break loop
			default:
			}

			if _, err := n.dockerClient.ContainerInspect(ctx, n.containerID); err != nil {
				if client.IsErrContainerNotFound(err) {
					success = true
					break
				}
				lastError = err
				time.Sleep(10 * time.Second)
				continue
			}

			if err := n.dockerClient.ContainerRemove(
				ctx, n.containerID, types.ContainerRemoveOptions{
					Force: true,
				},
			); err != nil {
				lastError = err
				n.logger.Warningd(
					n.containerError("failed to remove container on disconnect, retrying in 10 seconds", lastError),
				)
				time.Sleep(10 * time.Second)
				continue
			}
			break loop
		}
		if !success && lastError != nil {
			n.logger.Errord(
				n.containerError("failed to remove container on disconnect, giving up", lastError),
			)
		} else {
			n.containerID = ""
		}
		n.mutex.Unlock()
	}
}
