package dockerrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/containerssh/sshserver"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/mattn/go-shellwords"
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
	programParts, err := shellwords.Parse(program)
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

	//TODO proper timeout handling
	ctx := context.Background()

	execConfig := types.ExecConfig{
		Tty:          c.pty,
		AttachStdin:  true,
		AttachStderr: true,
		AttachStdout: true,
		Env:          c.createEnv(),
		Cmd:          program,
	}

	response, err := c.networkHandler.dockerClient.ContainerExecCreate(
		ctx,
		c.networkHandler.containerID,
		execConfig,
	)
	if err != nil {
		//TODO retry if connection failed
		//TODO log error
		return err
	}

	c.execID = response.ID

	if c.pty {
		if err := c.networkHandler.dockerClient.ContainerExecResize(
			ctx, c.execID, types.ResizeOptions{
				Height: uint(c.rows),
				Width:  uint(c.columns),
			},
		); err != nil {
			// TODO retry handling
			return err
		}
	}

	attachResult, err := c.networkHandler.dockerClient.ContainerExecAttach(
		ctx,
		c.execID,
		execConfig,
	)
	if err != nil {
		// TODO retry handling
		return err
	}

	done := func() {
		inspectResult, err := c.networkHandler.dockerClient.ContainerExecInspect(ctx, c.execID)
		if err != nil {
			//TODO handle retry
			onExit(137)
		} else if inspectResult.ExitCode >= 0 {
			onExit(sshserver.ExitStatus(inspectResult.ExitCode))
		} else {
			onExit(137)
		}
	}

	go func() {
		wg := &sync.WaitGroup{}
		wg.Add(2)
		if c.pty {
			go func() {
				defer done()
				_, err = io.Copy(stdout, attachResult.Reader)
				if err != nil && !errors.Is(err, io.EOF) {
					c.networkHandler.logger.Warningd(
						c.channelError("failed to stream TTY output", err),
					)
				}
			}()
		} else {
			go func() {
				defer done()
				// Demultiplex Docker stream
				_, err = stdcopy.StdCopy(stdout, stderr, attachResult.Reader)
				if err != nil && !errors.Is(err, io.EOF) {
					c.networkHandler.logger.Warningd(
						c.channelError("failed to stream raw output", err),
					)
				}
			}()
		}
		go func() {
			_, err = io.Copy(attachResult.Conn, stdin)
			if err != nil && !errors.Is(err, io.EOF) {
				c.networkHandler.logger.Warningd(
					c.channelError("failed to stream input", err),
				)
			}
		}()
	}()

	return nil
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
