[![ContainerSSH - Launch Containers on Demand](https://containerssh.github.io/images/logo-for-embedding.svg)](https://containerssh.github.io/)

<!--suppress HtmlDeprecatedAttribute -->
<h1 align="center">ContainerSSH DockerRun Backend Library</h1>

[![Go Report Card](https://goreportcard.com/badge/github.com/containerssh/dockerrun?style=for-the-badge)](https://goreportcard.com/report/github.com/containerssh/library-template)
[![LGTM Alerts](https://img.shields.io/lgtm/alerts/github/ContainerSSH/dockerrun?style=for-the-badge)](https://lgtm.com/projects/g/ContainerSSH/library-template/)

This library implements a backend that connects to a Docker socket and launches a new container for each connection, then runs executes a separate command per channel.

<p align="center"><strong>Note: This is a developer documentation.</strong><br />The user documentation for ContainerSSH is located at <a href="https://containerssh.github.io">containerssh.github.io</a>.</p>

## Using this library

This library implements a `NetworkConnectionHandler` from the [sshserver library](https://github.com/containerssh/sshserver). This can be embedded into a connection handler.

The network connection handler can be created with the `New()` method:

```go
var client net.TCPAddr
connectionID := []byte("asdf")
config := dockerrun.Config{
    //...
}
dr, err := dockerrun.New(client, connectionID, config)
if err != nil {
    // Handle error
}
// Use DR
```