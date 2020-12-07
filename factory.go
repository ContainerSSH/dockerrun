package dockerrun

import (
	"net"
	"sync"

	"github.com/containerssh/log"
	"github.com/containerssh/sshserver"
)

// New creates a new NetworkConnectionHandler for a specific client.
//goland:noinspection GoUnusedExportedFunction
func New(client net.TCPAddr, connectionID string, config Config, logger log.Logger) (sshserver.NetworkConnectionHandler, error) {
	return &networkHandler{
		mutex:        &sync.Mutex{},
		client:       client,
		connectionID: connectionID,
		config:       config,
		logger:       logger,
	}, nil
}
