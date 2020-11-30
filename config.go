package dockerrun

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// ContainerConfig contains the configuration of what container to run in Docker.
type ContainerConfig struct {
	ContainerConfig container.Config         `json:"container" yaml:"container" comment:"Config configuration." default:"{\"Image\":\"containerssh/containerssh-guest-image\"}"`
	HostConfig      container.HostConfig     `json:"host" yaml:"host" comment:"Host configuration"`
	NetworkConfig   network.NetworkingConfig `json:"network" yaml:"network" comment:"Network configuration"`
	ContainerName   string                   `json:"containername" yaml:"containername" comment:"Name for the container to be launched"`
	Subsystems      map[string]string        `json:"subsystems" yaml:"subsystems" comment:"Subsystem names and binaries map." default:"{\"sftp\":\"/usr/lib/openssh/sftp-server\"}"`
	DisableCommand  bool                     `json:"disableCommand" yaml:"disableCommand" comment:"Disable command execution passed from SSH"`
	IdleCommand     []string                 `json:"idleCommand" yaml:"idleCommand" comment:"Run this command to wait for container exit" default:"[\"/bin/sh\", \"-c\", \"sleep infinity & PID=$!; trap \\\"kill $PID\\\" INT TERM; wait\"]"`
}
