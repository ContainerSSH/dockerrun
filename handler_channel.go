package dockerrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/containerssh/sshserver"
	"github.com/containerssh/unixutils"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
)

type channelError struct {
	containerError

	ChannelID uint64 `json:"channelId"`
}

type channelHandler struct {
	channelID      uint64
	networkHandler *networkHandler
	username       string
	env            map[string]string
	pty            bool
	columns        uint32
	rows           uint32
	execID         string
	exitSent       bool
}

func (c *channelHandler) OnUnsupportedChannelRequest(_ uint64, _ string, _ []byte) {}

func (c *channelHandler) OnFailedDecodeChannelRequest(_ uint64, _ string, _ []byte, _ error) {}

func (c *channelHandler) OnEnvRequest(_ uint64, name string, value string) error {
	c.networkHandler.mutex.Lock()
	defer c.networkHandler.mutex.Lock()
	if c.execID != "" {
		return fmt.Errorf("program already running")
	}
	c.env[name] = value
	return nil
}

func (c *channelHandler) OnPtyRequest(
	_ uint64,
	term string,
	columns uint32,
	rows uint32,
	_ uint32,
	_ uint32,
	_ []byte,
) error {
	c.networkHandler.mutex.Lock()
	defer c.networkHandler.mutex.Lock()
	if c.execID != "" {
		return fmt.Errorf("program already running")
	}
	c.env["TERM"] = term
	c.rows = rows
	c.columns = columns
	c.pty = true
	return nil
}

func (c *channelHandler) channelError(message string, err error) channelError {
	return channelError{
		c.networkHandler.containerError(
			message,
			err,
		),
		c.channelID,
	}
}

func (c *channelHandler) createEnv() (result []string) {
	for k, v := range c.env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func (c *channelHandler) parseProgram(program string) []string {
	programParts, err := unixutils.ParseCMD(program)
	if err != nil {
		return []string{"/bin/sh", "-c", program}
	} else {
		if strings.HasPrefix(programParts[0], "/") || strings.HasPrefix(
			programParts[0],
			"./",
		) || strings.HasPrefix(programParts[0], "../") {
			return programParts
		} else {
			return []string{"/bin/sh", "-c", program}
		}
	}
}

func (c *channelHandler) run(
	program []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	onExit func(exitStatus sshserver.ExitStatus),
) error {
	c.networkHandler.mutex.Lock()
	defer c.networkHandler.mutex.Unlock()
	if c.execID != "" {
		return fmt.Errorf("program already running")
	}

	startContext, cancelFunc := context.WithTimeout(context.Background(), c.networkHandler.config.Config.Timeout)
	defer cancelFunc()

	execConfig := c.getExecConfig(program)

	if err := c.createExec(startContext, execConfig); err != nil {
		return err
	}

	if c.pty {
		if err := c.resizeExec(startContext); err != nil {
			return err
		}
	}

	attachResult, err := c.attachExec(startContext, execConfig)
	if err != nil {
		return err
	}

	go c.handleRun(onExit, attachResult, stdout, stderr, stdin)

	return nil
}

func (c *channelHandler) attachExec(startContext context.Context, execConfig types.ExecConfig) (
	types.HijackedResponse,
	error,
) {
	var attachResult types.HijackedResponse
	var lastError error
loop:
	for {
		select {
		case <-startContext.Done():
			break loop
		default:
		}
		var err error
		attachResult, err = c.networkHandler.dockerClient.ContainerExecAttach(
			startContext,
			c.execID,
			execConfig,
		)
		lastError = err
		if err != nil {
			c.networkHandler.logger.Warningd(
				c.channelError(
					"failed to attach to exec, retrying in 10 seconds",
					err,
				),
			)
			time.Sleep(10 * time.Second)
			continue
		}
		break loop
	}
	if lastError != nil {
		c.networkHandler.logger.Errord(c.channelError("failed to attach to exec, giving up", lastError))
		return types.HijackedResponse{}, lastError
	}
	return attachResult, nil
}

func (c *channelHandler) resizeExec(startContext context.Context) error {
	var lastError error
loop:
	for {
		select {
		case <-startContext.Done():
			break loop
		default:
		}
		err := c.networkHandler.dockerClient.ContainerExecResize(
			startContext, c.execID, types.ResizeOptions{
				Height: uint(c.rows),
				Width:  uint(c.columns),
			},
		)
		lastError = err
		if err != nil {
			c.networkHandler.logger.Warningd(
				c.channelError(
					"failed to resize exec window, retrying in 10 seconds",
					err,
				),
			)
			time.Sleep(10 * time.Second)
			continue
		}
		break loop
	}
	if lastError != nil {
		c.networkHandler.logger.Errord(c.channelError("failed to resize exec window, giving up", lastError))
		return lastError
	}
	return nil
}

func (c *channelHandler) createExec(startContext context.Context, execConfig types.ExecConfig) error {
	var lastError error
loop:
	for {
		select {
		case <-startContext.Done():
			break loop
		default:
		}
		response, err := c.networkHandler.dockerClient.ContainerExecCreate(
			startContext,
			c.networkHandler.containerID,
			execConfig,
		)
		lastError = err
		if err != nil {
			c.networkHandler.logger.Warningd(c.channelError("failed to create exec, retrying in 10 seconds", err))
			time.Sleep(10 * time.Second)
			continue
		}
		c.execID = response.ID
		break loop
	}
	if lastError != nil {
		c.networkHandler.logger.Errord(c.channelError("failed to create exec, giving up", lastError))
		return lastError
	}
	return nil
}

func (c *channelHandler) getExecConfig(program []string) types.ExecConfig {
	return types.ExecConfig{
		Tty:          c.pty,
		AttachStdin:  true,
		AttachStderr: true,
		AttachStdout: true,
		Env:          c.createEnv(),
		Cmd:          program,
	}
}

func (c *channelHandler) done(onExit func(exitStatus sshserver.ExitStatus)) {
	c.networkHandler.mutex.Lock()
	if c.exitSent {
		c.networkHandler.mutex.Unlock()
		return
	}
	c.exitSent = true
	c.networkHandler.mutex.Unlock()

	doneContext, cancelFunc := context.WithTimeout(context.Background(), c.networkHandler.config.Config.Timeout)
	defer cancelFunc()

	var lastError error
	var exitCode = 137
loop:
	for {
		select {
		case <-doneContext.Done():
			break loop
		default:
		}
		if c.networkHandler.disconnected {
			return
		}
		inspectResult, err := c.networkHandler.dockerClient.ContainerExecInspect(doneContext, c.execID)
		lastError = err
		if err != nil {
			c.networkHandler.logger.Warningd(c.channelError("failed to fetch container exec result, retrying in 10 seconds", err))
		} else if inspectResult.ExitCode >= 0 {
			exitCode = inspectResult.ExitCode
			break loop
		} else {
			lastError = fmt.Errorf("negative exit code (%d)", inspectResult.ExitCode)
			c.networkHandler.logger.Warningd(c.channelError("negative exit code returned from exec, retrying in 10 seconds", lastError))
		}
		time.Sleep(10 * time.Second)
	}
	onExit(sshserver.ExitStatus(exitCode))
	if lastError != nil {
		c.networkHandler.logger.Warningd(c.channelError("failed to fetch container exec result, giving up", lastError))
	}
}

func (c *channelHandler) handleRun(
	onExit func(exitStatus sshserver.ExitStatus),
	attachResult types.HijackedResponse,
	stdout io.Writer,
	stderr io.Writer,
	stdin io.Reader,
) {
	wg := &sync.WaitGroup{}
	wg.Add(2)
	if c.pty {
		go func() {
			defer c.done(onExit)
			_, err := io.Copy(stdout, attachResult.Reader)
			if err != nil && !errors.Is(err, io.EOF) {
				c.networkHandler.logger.Warningd(
					c.channelError("failed to stream TTY output", err),
				)
			}
		}()
	} else {
		go func() {
			defer c.done(onExit)
			// Demultiplex Docker stream
			_, err := stdcopy.StdCopy(stdout, stderr, attachResult.Reader)
			if err != nil && !errors.Is(err, io.EOF) {
				c.networkHandler.logger.Warningd(
					c.channelError("failed to stream raw output", err),
				)
			}
		}()
	}
	go func() {
		_, err := io.Copy(attachResult.Conn, stdin)
		if err != nil && !errors.Is(err, io.EOF) {
			c.networkHandler.logger.Warningd(
				c.channelError("failed to stream input", err),
			)
		}
	}()
}

func (c *channelHandler) OnExecRequest(
	_ uint64,
	program string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	onExit func(exitStatus sshserver.ExitStatus),
) error {
	return c.run(c.parseProgram(program), stdin, stdout, stderr, onExit)
}

func (c *channelHandler) OnShell(
	_ uint64,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	onExit func(exitStatus sshserver.ExitStatus),
) error {
	return c.run(nil, stdin, stdout, stderr, onExit)
}

func (c *channelHandler) OnSubsystem(
	_ uint64,
	subsystem string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	onExit func(exitStatus sshserver.ExitStatus),
) error {
	if binary, ok := c.networkHandler.config.Config.Subsystems[subsystem]; ok {
		return c.run([]string{binary}, stdin, stdout, stderr, onExit)
	}
	return fmt.Errorf("subsystem not supported")
}

func (c *channelHandler) OnSignal(_ uint64, _ string) error {
	c.networkHandler.mutex.Lock()
	defer c.networkHandler.mutex.Lock()
	if c.execID == "" {
		return fmt.Errorf("program not running")
	}

	return nil
}

func (c *channelHandler) OnWindow(_ uint64, columns uint32, rows uint32, _ uint32, _ uint32) error {
	c.networkHandler.mutex.Lock()
	defer c.networkHandler.mutex.Lock()
	if c.execID == "" {
		return fmt.Errorf("program not running")
	}

	//TODO context handling
	ctx := context.Background()

	if err := c.networkHandler.dockerClient.ContainerExecResize(
		ctx, c.execID, types.ResizeOptions{
			Height: uint(rows),
			Width:  uint(columns),
		},
	); err != nil {
		//TODO retry handling
		//TODO logging
		return err
	}

	return nil
}
