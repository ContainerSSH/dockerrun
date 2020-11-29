package dockerrun

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/containerssh/log"
	"github.com/containerssh/sshserver"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type networkHandler struct {
	mutex        *sync.Mutex
	client       net.TCPAddr
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
	Error        error  `json:"error"`
}

func (n *networkHandler) containerError(message string, err error) containerError {
	return containerError{
		ConnectionID: n.connectionID,
		ContainerID:  n.containerID,
		Message:      message,
		Error:        err,
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
	ctx := context.Background()

	if err := n.setupDockerClient(ctx); err != nil {
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
		body, err := n.dockerClient.ContainerCreate(
			ctx,
			&n.config.Config.ContainerConfig,
			&n.config.Config.HostConfig,
			&n.config.Config.NetworkConfig,
			n.config.Config.ContainerName,
		)
		if err != nil {
			return err
		}
		n.containerID = body.ID
	}
	return nil
}

func (n *networkHandler) setupDockerClient(ctx context.Context) error {
	if n.dockerClient == nil {
		dockerClient, err := n.config.getDockerClient()
		if err != nil {
			return fmt.Errorf("failed to create Docker client (%v)", err)
		}
		n.dockerClient = dockerClient
	}
	if _, err := n.dockerClient.Ping(ctx); err != nil {
		return err
	}
	return nil
}

func (n *networkHandler) OnDisconnect() {
	ctx := context.Background()
	n.mutex.Lock()
	if n.containerID != "" {
		success := false
		var lastError error
		for i := 0; i < 6; i++ {
			n.mutex.Unlock()
			n.mutex.Lock()
			if err := n.setupDockerClient(ctx); err != nil {
				n.logger.Noticed(
					n.containerError(
						"failed to create Docker client for container removal on disconnect",
						err,
					),
				)
				lastError = err
				time.Sleep(10 * time.Second)
				continue
			}

			if err := n.dockerClient.ContainerRemove(
				ctx, n.containerID, types.ContainerRemoveOptions{
					Force: true,
				},
			); err != nil {
				if client.IsErrContainerNotFound(err) {
					success = true
					break
				}
				n.logger.Noticed(
					containerError{
						ConnectionID: n.connectionID,
						ContainerID:  n.containerID,
						Message:      "failed to remove container on disconnect",
						Error:        err,
					},
				)
				lastError = err
				time.Sleep(10 * time.Second)
				continue
			}
			break
		}
		n.mutex.Unlock()
		if !success {
			n.logger.Warningd(
				containerError{
					ConnectionID: n.connectionID,
					ContainerID:  n.containerID,
					Message:      "failed to remove container on client disconnect",
					Error:        lastError,
				},
			)
		}
	}
}
