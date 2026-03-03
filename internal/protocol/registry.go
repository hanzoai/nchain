package protocol

import "sync"

var (
	mu      sync.RWMutex
	drivers = map[string]Driver{}
)

// Register adds a protocol driver to the registry.
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	drivers[d.Name()] = d
}

// Get returns the protocol driver for the given name.
func Get(name string) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := drivers[name]
	return d, ok
}

func init() {
	Register(&LuxDriver{})
	Register(&EthereumDriver{})
	Register(&SolanaDriver{})
	Register(&BitcoinDriver{})
	Register(&CosmosDriver{})
	Register(&GenericDriver{})
}
