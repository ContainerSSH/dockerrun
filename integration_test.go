package dockerrun_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
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
	t.Parallel()

	config := dockerrun.Config{}
	structutils.Defaults(&config)

	config.Config.ContainerConfig.Image = "ubuntu:18.04"

	dr, err := dockerrun.New(
		net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 2222,
			Zone: "",
		},
		"0123456789AAAAAA",
		config,
		createLogger(t),
	)
	must(t, assert.Nil(t, err))
	_, err = dr.OnHandshakeSuccess("test")
	defer dr.OnDisconnect()
	must(t, assert.Nil(t, err))

	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(config.Host),
	)
	must(t, assert.Nil(t, err))
	dockerClient.NegotiateAPIVersion(context.Background())
	f := filters.NewArgs()
	f.Add("label", "containerssh_username=test")
	f.Add("label", "containerssh_ip=127.0.0.1")
	f.Add("label", "containerssh_connection_id=0123456789AAAAAA")
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
	must(t, assert.True(t, client.IsErrNotFound(err)))
}

func TestSingleSessionShouldRunProgram(t *testing.T) {
	t.Parallel()

	config := dockerrun.Config{}
	structutils.Defaults(&config)

	dr, err := dockerrun.New(
		net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 2222,
			Zone: "",
		},
		"0123456789AAAAAB",
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
	stdoutReader, stdout := io.Pipe()
	var stderrBytes bytes.Buffer
	stderr := bufio.NewWriter(&stderrBytes)
	done := make(chan struct{})
	status := 0
	go func() {
		assert.NoError(t, readUntil(stdoutReader, []byte("Hello world!\n")))
	}()

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
	assert.Equal(t, "", stderrBytes.String())
	assert.Equal(t, 0, status)
}

func readUntil(reader io.Reader, buffer []byte) error {
	byteBuffer := bytes.NewBuffer([]byte{})
	for {
		buf := make([]byte, 1024)
		n, err := reader.Read(buf)
		if err != nil {
			return err
		}
		byteBuffer.Write(buf[:n])
		if bytes.Equal(byteBuffer.Bytes(), buffer) {
			return nil
		}
	}
}

func TestSingleSessionShouldRunShell(t *testing.T) {
	t.Parallel()

	dr, ssh := initDockerRun(t)
	defer dr.OnDisconnect()

	var err error
	session, err := ssh.OnSessionChannel(0, []byte{})
	must(t, assert.Nil(t, err))

	stdin, stdinWriter := io.Pipe()
	stdoutReader, stdout := io.Pipe()
	_, stderr := io.Pipe()
	done := make(chan struct{})
	status := 0
	assert.NoError(t, session.OnEnvRequest(0, "foo", "bar"))
	assert.NoError(t, session.OnPtyRequest(1, "xterm", 80, 25, 800, 600, []byte{}))
	go func() {
		assert.NoError(t, readUntil(stdoutReader, []byte("# ")))

		assert.NoError(t, session.OnWindow(2, 120, 25, 800, 600))

		_, err = stdinWriter.Write([]byte("tput cols\n"))
		assert.NoError(t, readUntil(stdoutReader, []byte("tput cols\r\n120\r\n# ")))

		_, err = stdinWriter.Write([]byte("echo \"Hello world!\"\n"))
		assert.NoError(t, err)

		assert.NoError(t, readUntil(stdoutReader, []byte("echo \"Hello world!\"\r\nHello world!\r\n# ")))

		_, err = stdinWriter.Write([]byte("exit\n"))
		assert.NoError(t, err)

		assert.NoError(t, readUntil(stdoutReader, []byte("exit\r\n")))
	}()
	err = session.OnShell(
		3,
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
	assert.Equal(t, 0, status)
}

func initDockerRun(t *testing.T) (sshserver.NetworkConnectionHandler, sshserver.SSHConnectionHandler) {
	config := dockerrun.Config{}
	structutils.Defaults(&config)
	config.Config.ShellCommand = []string{"/bin/sh"}

	dr, err := dockerrun.New(
		net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 2222,
			Zone: "",
		},
		"0123456789AAAAAC",
		config,
		createLogger(t),
	)
	must(t, assert.Nil(t, err))
	ssh, err := dr.OnHandshakeSuccess("test")
	must(t, assert.Nil(t, err))
	return dr, ssh
}

func createLogger(t *testing.T) log.Logger {
	logger, err := log.New(
		log.Config{
			Level:  log.LevelDebug,
			Format: "text",
		}, "dockerrun", os.Stdout,
	)
	assert.Nil(t, err, "failed to create logger (%v)", err)
	return logger
}
