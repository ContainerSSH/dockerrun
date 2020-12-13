package dockerrun

import (
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
)

type ImagePullPolicy string

const (
	// ImagePullPolicyAlways means that the container image will be pulled on every connection.
	ImagePullPolicyAlways ImagePullPolicy = "Always"
	// ImagePullPolicyIfNotPresent means the image will be pulled if the image is not present locally, an empty tag, or
	// the "latest" tag was specified.
	ImagePullPolicyIfNotPresent ImagePullPolicy = "IfNotPresent"
	// ImagePullPolicyNever means that the image will be never pulled, and if the image is not available locally the
	// connection will fail.
	ImagePullPolicyNever ImagePullPolicy = "Never"
)

// Validate checks if the given image pull policy is valid.
func (p ImagePullPolicy) Validate() error {
	switch p {
	case ImagePullPolicyAlways:
		fallthrough
	case ImagePullPolicyIfNotPresent:
		fallthrough
	case ImagePullPolicyNever:
		return nil
	default:
		return fmt.Errorf("invalid image pull policy: %s", p)
	}
}

// ContainerConfig contains the configuration of what container to run in Docker.
type ContainerConfig struct {
	// ContainerConfig contains container-specific configuration options.
	ContainerConfig container.Config `json:"container" yaml:"container" comment:"Config configuration." default:"{\"Image\":\"containerssh/containerssh-guest-image\"}"`
	// HostConfig contains the host-specific configuration options.
	HostConfig container.HostConfig `json:"host" yaml:"host" comment:"Host configuration"`
	// NetworkConfig contains the network settings.
	NetworkConfig network.NetworkingConfig `json:"network" yaml:"network" comment:"Network configuration"`
	// Platform contains the platform specification.
	Platform *specs.Platform `json:"platform" yaml:"platform" comment:"Platform specification"`
	// ContainerName is the name of the container to launch. It is recommended to leave this empty, otherwise
	// ContainerSSH may not be able to start the container if a container with the same name already exists.
	ContainerName string `json:"containername" yaml:"containername" comment:"Name for the container to be launched"`
	// Subsystems contains a map of subsystem names and their corresponding binaries in the container.
	Subsystems map[string]string `json:"subsystems" yaml:"subsystems" comment:"Subsystem names and binaries map." default:"{\"sftp\":\"/usr/lib/openssh/sftp-server\"}"`
	// DisableCommand disables passed command execution.
	DisableCommand bool `json:"disableCommand" yaml:"disableCommand" comment:"Disable command execution passed from SSH"`
	// DisableShell disables shell requests.
	DisableShell bool `json:"disableShell" yaml:"disableShell" comment:"Disables shell requests."`
	// DisableSubsystem disables subsystem requests.
	DisableSubsystem bool `json:"disableSubsystem" yaml:"disableSubsystem" comment:"Disables subsystem requests."`
	// ShellCommand is the command that runs when a shell is requested. This is intentionally left empty because populating it would mean a potential security issue.
	ShellCommand []string `json:"shellCommand" yaml:"shellCommand" comment:"Run this command when a new shell is requested." default:"[]"`
	// IdleCommand is the command that runs as the first process in the container. The only job of this command is to
	// keep the container alive and exit when a TERM signal is sent.
	IdleCommand []string `json:"idleCommand" yaml:"idleCommand" comment:"Run this command to wait for container exit" default:"[\"/bin/sh\", \"-c\", \"sleep infinity & PID=$!; trap \\\"kill $PID\\\" INT TERM; wait\"]"`
	// ImagePullPolicy controls when to pull container images.
	ImagePullPolicy ImagePullPolicy `json:"imagePullPolicy" yaml:"imagePullPolicy" comment:"Image pull policy" default:"IfNotPresent"`
	// Timeout is the timeout for container start.
	Timeout time.Duration `json:"timeout" yaml:"timeout" comment:"Timeout for container start." default:"60s"`
}

// Validate validates the dockerrun config structure.
func (c ContainerConfig) Validate() error {
	if len(c.IdleCommand) == 0 {
		return fmt.Errorf("empty idle command provided")
	}
	if len(c.ShellCommand) == 0 && !c.DisableShell {
		return fmt.Errorf("empty shell command provided and shell is not disabled")
	}
	if err := c.ImagePullPolicy.Validate(); err != nil {
		return err
	}
	return nil
}
