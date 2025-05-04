// Package mock offers mock implementations of interfaces defined in types.go.
// This allows complete control over input / output for any given method that consumes a given type.
package mock

import (
	"errors"
	"net"

	"github.com/sony/sonyflake/v2/types"
)

// NewSuccessfulInterfaceAddrs returns a single private IP address.
func NewSuccessfulInterfaceAddrs() types.InterfaceAddrs {
	ifat := make([]net.Addr, 0, 1)
	ifat = append(ifat, &net.IPNet{IP: []byte{192, 168, 0, 1}, Mask: []byte{255, 0, 0, 0}})

	return func() ([]net.Addr, error) {
		return ifat, nil
	}
}

var ErrFailedToGetAddresses = errors.New("failed to get addresses")

// NewFailingInterfaceAddrs returns an error.
func NewFailingInterfaceAddrs() types.InterfaceAddrs {
	return func() ([]net.Addr, error) {
		return nil, ErrFailedToGetAddresses
	}
}

// NewNilInterfaceAddrs returns an empty slice of addresses.
func NewNilInterfaceAddrs() types.InterfaceAddrs {
	return func() ([]net.Addr, error) {
		return []net.Addr{}, nil
	}
}
