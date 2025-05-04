// Package types defines type signatures used throughout sonyflake.
// This provides the ability to mock out imports.
package types

import "net"

// InterfaceAddrs defines the interface used for retrieving network addresses.
type InterfaceAddrs func() ([]net.Addr, error)
