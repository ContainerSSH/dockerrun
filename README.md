[![ContainerSSH - Launch Containers on Demand](https://containerssh.io/deprecations/dockerrun.png)](https://containerssh.github.io/)

<!--suppress HtmlDeprecatedAttribute -->
<h1 align="center">⚠ The DockerRun Backend is deprecated! ⚠</h1>

[![Go Report Card](https://goreportcard.com/badge/github.com/containerssh/dockerrun?style=for-the-badge)](https://goreportcard.com/report/github.com/containerssh/library-template)
[![LGTM Alerts](https://img.shields.io/lgtm/alerts/github/ContainerSSH/dockerrun?style=for-the-badge)](https://lgtm.com/projects/g/ContainerSSH/library-template/)

<p align="center">This backend is no longer maintained and <strong>replaced by the <a href="https://github.com/containerssh/docker">docker backend</a></strong>. Please see <a href="https://containerssh.io/deprecations/dockerrun/">the deprecation notice for details</a>.</p>

This library implements a backend that connects to a Docker socket and launches a new container for each connection, then runs executes a separate command per channel.

<p align="center"><strong>⚠⚠⚠ Warning: This is a developer documentation. ⚠⚠⚠</strong><br />The user documentation for ContainerSSH is located at <a href="https://containerssh.io">containerssh.io</a>.</p>

## Using this library

This library implements a `NetworkConnectionHandler` from the [sshserver library](https://github.com/containerssh/sshserver). This can be embedded into a connection handler.

The network connection handler can be created with the `New()` method:

```go
var client net.TCPAddr
connectionID := "0123456789ABCDEF"
config := dockerrun.Config{
    //...
}
dr, err := dockerrun.New(client, connectionID, config, logger)
if err != nil {
    // Handle error
}
```

The `logger` parameter is a logger from the [ContainerSSH logger library](https://github.com/containerssh/log).

The `dr` variable can then be used to create a container on finished handshake:

```go
ssh, err := dr.OnHandshakeSuccess("provided-connection-username")
```

Conversely, on disconnect you must call `dr.OnDisconnect()`. The `ssh` variable can then be used to create session channels:

```go
var channelID uint64 = 0
extraData := []byte{}
session, err := ssh.OnSessionChannel(channelID, extraData)
```

Finally, the session can be used to launch programs:

```go
var requestID uint64 = 0
err = session.OnEnvRequest(requestID, "foo", "bar")
// ...
requestID = 1
var stdin io.Reader
var stdout, stderr io.Writer
err = session.OnShell(
    requestID,
    stdin,
    stdout,
    stderr,
    func(exitStatus ExitStatus) {
        // ...
    },
)
```

