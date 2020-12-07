package dockerrun

import (
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// ContainerConfig contains the configuration of what container to run in Docker.
type ContainerConfig struct {
	// ContainerConfig contains container-specific configuration options.
	ContainerConfig container.Config `json:"container" yaml:"container" comment:"Config configuration." default:"{\"Image\":\"containerssh/containerssh-guest-image\"}"`
	// HostConfig contains the host-specific configuration options.
	HostConfig container.HostConfig `json:"host" yaml:"host" comment:"Host configuration"`
	// NetworkConfig contains the network settings.
	NetworkConfig network.NetworkingConfig `json:"network" yaml:"network" comment:"Network configuration"`
	// ContainerName is the name of the container to launch. It is recommended to leave this empty, otherwise
	// ContainerSSH may not be able to start the container if a container with the same name already exists.
	ContainerName string `json:"containername" yaml:"containername" comment:"Name for the container to be launched"`
	// Subsystems contains a map of subsystem names and their corresponding binaries in the container.
	Subsystems map[string]string `json:"subsystems" yaml:"subsystems" comment:"Subsystem names and binaries map." default:"{\"sftp\":\"/usr/lib/openssh/sftp-server\"}"`
	// DisableCommand disables passed command execution.
	DisableCommand bool `json:"disableCommand" yaml:"disableCommand" comment:"Disable command execution passed from SSH"`
	// IdleCommand is the command that runs as the first process in the container. The only job of this command is to
	// keep the container alive and exit when a TERM signal is sent.
	IdleCommand     []string                 `json:"idleCommand" yaml:"idleCommand" comment:"Run this command to wait for container exit" default:"[\"/bin/sh\", \"-c\", \"sleep infinity & PID=$!; trap \\\"kill $PID\\\" INT TERM; wait\"]"`
	// Timeout is the timeout for container start.
	Timeout time.Duration `json:"timeout" yaml:"timeout" comment:"Timeout for container start." default:"60s"`
}
