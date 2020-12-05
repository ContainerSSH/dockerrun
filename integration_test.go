package dockerrun_test

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os"
	"testing"

	"github.com/containerssh/log"
	"github.com/containerssh/sshserver"
	"github.com/containerssh/structutils"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"

	"github.com/containerssh/dockerrun"
)

func must(t *testing.T, arg bool) {
	if !arg {
		t.FailNow()
	}
}

func TestConnectAndDisconnectShouldCreateAndRemoveContainer(t *testing.T) {
	config := dockerrun.Config{
		Config: dockerrun.ContainerConfig{},
	}
	structutils.Defaults(&config)

	dr, err := dockerrun.New(
		net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 2222,
			Zone: "",
		},
		[]byte("asdf"),
		config,
		createLogger(t),
	)
	must(t, assert.Nil(t, err))
	_, err = dr.OnHandshakeSuccess("test")
	must(t, assert.Nil(t, err))
	defer dr.OnDisconnect()

	dockerClient, err := client.NewClient(config.Host, "", nil, make(map[string]string))
	must(t, assert.Nil(t, err))
	f := filters.NewArgs()
	f.Add("label", "containerssh_username=test")
	f.Add("label", "containerssh_ip=127.0.0.1")
	containers, err := dockerClient.ContainerList(
		context.Background(),
		types.ContainerListOptions{
			Filters: f,
		},
	)
	must(t, assert.Nil(t, err))
	must(t, assert.Equal(t, 1, len(containers)))
	must(t, assert.Equal(t, "running", containers[0].State))

	dr.OnDisconnect()
	_, err = dockerClient.ContainerInspect(context.Background(), containers[0].ID)
	must(t, assert.True(t, client.IsErrContainerNotFound(err)))
}

func TestSingleSessionShouldRunProgram(t *testing.T) {
	config := dockerrun.Config{
		Config: dockerrun.ContainerConfig{},
	}
	structutils.Defaults(&config)

	dr, err := dockerrun.New(
		net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 2222,
			Zone: "",
		},
		[]byte("asdf"),
		config,
		createLogger(t),
	)
	must(t, assert.Nil(t, err))
	ssh, err := dr.OnHandshakeSuccess("test")
	must(t, assert.Nil(t, err))
	defer dr.OnDisconnect()

	session, err := ssh.OnSessionChannel(0, []byte{})
	must(t, assert.Nil(t, err))

	stdin := bytes.NewReader([]byte{})
	var stdoutBytes bytes.Buffer
	stdout := bufio.NewWriter(&stdoutBytes)
	var stderrBytes bytes.Buffer
	stderr := bufio.NewWriter(&stderrBytes)
	done := make(chan struct{})
	status := 0
	err = session.OnExecRequest(
		0,
		"echo \"Hello world!\"",
		stdin,
		stdout,
		stderr,
		func(exitStatus sshserver.ExitStatus) {
			status = int(exitStatus)
			done <- struct{}{}
		},
	)
	must(t, assert.Nil(t, err))
	<-done
	must(t, assert.Nil(t, stdout.Flush()))
	assert.Equal(t, "Hello world!\n", stdoutBytes.String())
	assert.Equal(t, "", stderrBytes.String())
	assert.Equal(t, 0, status)
}

func createLogger(t *testing.T) (log.Logger) {
	logger, err := log.New(
		log.Config{
			Level:  log.LevelDebug,
			Format: "text",
		}, "dockerrun", os.Stdout,
	)
	assert.Nil(t, err, "failed to create logger (%v)", err)
	return logger
}
