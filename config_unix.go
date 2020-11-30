// +build linux freebsd openbsd darwin

package dockerrun

// Config is the base configuration structure of the DockerRun backend.
type Config struct {
	Host   string          `json:"host" yaml:"host" comment:"Docker connect URL" default:"unix:///var/run/docker.sock"`
	CaCert string          `json:"cacert" yaml:"cacert" comment:"CA certificate for Docker connection embedded in the configuration in PEM format."`
	Cert   string          `json:"cert" yaml:"cert" comment:"Client certificate in PEM format embedded in the configuration."`
	Key    string          `json:"key" yaml:"key" comment:"Client key in PEM format embedded in the configuration."`
	Config ContainerConfig `json:"config" yaml:"config" comment:"Config configuration"`
}
