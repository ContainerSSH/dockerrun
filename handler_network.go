package dockerrun

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/containerssh/log"
	"github.com/containerssh/sshserver"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/qdm12/reprint"
)

type networkHandler struct {
	mutex        *sync.Mutex
	client       net.TCPAddr
	username     string
	connectionID []byte
	config       Config
	containerID  string
	dockerClient *client.Client
	logger       log.Logger
}

type containerError struct {
	ConnectionID []byte `json:"connectionId"`
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

func (n *networkHandler) OnAuthPubKey(_ string, _ []byte) (response sshserver.AuthResponse, reason error) {
	return sshserver.AuthResponseUnavailable, fmt.Errorf("dockerrun does not support authentication")
}

func (n *networkHandler) OnHandshakeFailed(_ error) {}

func (n *networkHandler) OnHandshakeSuccess(username string) (
	connection sshserver.SSHConnectionHandler,
	failureReason error,
) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	ctx := context.TODO()
	n.username = username

	if err := n.setupDockerClient(ctx); err != nil {
		return nil, err
	}
	if err := n.config.pullImage(n.dockerClient, ctx); err != nil {
		return nil, err
	}
	if err := n.createContainer(ctx); err != nil {
		return nil, err
	}
	return &sshConnectionHandler{
		networkHandler: n,
		username:       username,
	}, nil
}

func (n *networkHandler) createContainer(ctx context.Context) error {
	if n.containerID == "" {
		containerConfig := n.config.Config.ContainerConfig
		newConfig := container.Config{}
		if err := reprint.FromTo(&containerConfig, &newConfig); err != nil {
			return err
		}
		if newConfig.Labels == nil {
			newConfig.Labels = map[string]string{}
		}
		newConfig.Cmd = n.config.Config.IdleCommand
		newConfig.Labels["containerssh_connection_id"] = hex.EncodeToString(n.connectionID)
		newConfig.Labels["containerssh_ip"] = n.client.IP.String()
		newConfig.Labels["containerssh_username"] = n.username
		body, err := n.dockerClient.ContainerCreate(
			ctx,
			&newConfig,
			&n.config.Config.HostConfig,
			&n.config.Config.NetworkConfig,
			n.config.Config.ContainerName,
		)
		if err != nil {
			return err
		}
		n.containerID = body.ID

		if err := n.dockerClient.ContainerStart(
			ctx,
			n.containerID,
			types.ContainerStartOptions{},
		); err != nil {
			return err
		}
	}
	return nil
}

func (n *networkHandler) setupDockerClient(ctx context.Context) error {
	if n.dockerClient == nil {
		dockerClient, err := n.config.getDockerClient()
		if err != nil {
			return fmt.Errorf("failed to create Docker client (%w)", err)
		}
		n.dockerClient = dockerClient
	}
	if _, err := n.dockerClient.Ping(ctx); err != nil {
		return err
	}
	return nil
}

func (n *networkHandler) OnDisconnect() {
	ctx := context.TODO()
	n.mutex.Lock()
	if n.containerID != "" {
		success := false
		var lastError error
		for i := 0; i < 6; i++ {
			n.mutex.Unlock()
			n.mutex.Lock()

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
				time.Sleep(10 * time.Second)
				continue
			}
			break
		}
		if !success && lastError != nil {
			n.logger.Warningd(
				n.containerError("failed to remove container on disconnect, giving up", lastError),
			)
		} else {
			n.containerID = ""
		}
		n.mutex.Unlock()
	}
}
