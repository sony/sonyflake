// Package sonyflake implements Sonyflake, a distributed unique ID generator inspired by Twitter's Snowflake.
//
// A Sonyflake ID is composed of
//
//	39 bits for time in units of 10 msec
//	 8 bits for a sequence number
//	16 bits for a machine id
package sonyflake

import (
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/sony/sonyflake/types"
)

// These constants are the bit lengths of Sonyflake ID parts.
const (
	BitLenTime      = 39                               // bit length of time
	BitLenSequence  = 8                                // bit length of sequence number
	BitLenMachineID = 63 - BitLenTime - BitLenSequence // bit length of machine id
)

// Settings configures Sonyflake:
//
// StartTime is the time since which the Sonyflake time is defined as the elapsed time.
// If StartTime is 0, the start time of the Sonyflake is set to "2014-09-01 00:00:00 +0000 UTC".
// If StartTime is ahead of the current time, Sonyflake is not created.
//
// MachineID returns the unique ID of the Sonyflake instance.
// If MachineID returns an error, Sonyflake is not created.
// If MachineID is nil, default MachineID is used.
// Default MachineID returns the lower 16 bits of the private IP address.
//
// CheckMachineID validates the uniqueness of the machine ID.
// If CheckMachineID returns false, Sonyflake is not created.
// If CheckMachineID is nil, no validation is done.
type Settings struct {
	StartTime      time.Time
	MachineID      func() (uint16, error)
	CheckMachineID func(uint16) bool
}

// Sonyflake is a distributed unique ID generator.
type Sonyflake struct {
	mutex       *sync.Mutex
	startTime   int64
	elapsedTime int64
	sequence    uint16
	machineID   uint16
}

var (
	ErrStartTimeAhead   = errors.New("start time is ahead of now")
	ErrNoPrivateAddress = errors.New("no private ip address")
	ErrOverTimeLimit    = errors.New("over the time limit")
	ErrInvalidMachineID = errors.New("invalid machine id")
	ErrInvalidSequence  = errors.New("invalid sequence number")
)

var defaultInterfaceAddrs = net.InterfaceAddrs

// New returns a new Sonyflake configured with the given Settings.
// New returns an error in the following cases:
// - Settings.StartTime is ahead of the current time.
// - Settings.MachineID returns an error.
// - Settings.CheckMachineID returns false.
func New(st Settings) (*Sonyflake, error) {
	if st.StartTime.After(time.Now()) {
		return nil, ErrStartTimeAhead
	}

	sf := new(Sonyflake)
	sf.mutex = new(sync.Mutex)
	sf.sequence = uint16(1<<BitLenSequence - 1)

	if st.StartTime.IsZero() {
		sf.startTime = toSonyflakeTime(time.Date(2014, 9, 1, 0, 0, 0, 0, time.UTC))
	} else {
		sf.startTime = toSonyflakeTime(st.StartTime)
	}

	var err error
	if st.MachineID == nil {
		sf.machineID, err = lower16BitPrivateIP(defaultInterfaceAddrs)
	} else {
		sf.machineID, err = st.MachineID()
	}
	if err != nil {
		return nil, err
	}

	if st.CheckMachineID != nil && !st.CheckMachineID(sf.machineID) {
		return nil, ErrInvalidMachineID
	}

	return sf, nil
}

// NewSonyflake returns a new Sonyflake configured with the given Settings.
// NewSonyflake returns nil in the following cases:
// - Settings.StartTime is ahead of the current time.
// - Settings.MachineID returns an error.
// - Settings.CheckMachineID returns false.
func NewSonyflake(st Settings) *Sonyflake {
	sf, _ := New(st)
	return sf
}

// NextID generates a next unique ID.
// After the Sonyflake time overflows, NextID returns an error.
func (sf *Sonyflake) NextID() (uint64, error) {
	const maskSequence = uint16(1<<BitLenSequence - 1)

	sf.mutex.Lock()
	defer sf.mutex.Unlock()

	current := currentElapsedTime(sf.startTime)
	if sf.elapsedTime < current {
		sf.elapsedTime = current
		sf.sequence = 0
	} else { // sf.elapsedTime >= current
		sf.sequence = (sf.sequence + 1) & maskSequence
		if sf.sequence == 0 {
			sf.elapsedTime++
			overtime := sf.elapsedTime - current
			time.Sleep(sleepTime((overtime)))
		}
	}

	return sf.toID()
}

// Next generates a next unique ID as an ID.
// After the Sonyflake time overflows, Next returns an error.
func (sf *Sonyflake) Next() (ID, error) {
	id, err := sf.NextID()
	return ID(id), err
}

const sonyflakeTimeUnit = 1e7 // nsec, i.e. 10 msec

func toSonyflakeTime(t time.Time) int64 {
	return t.UTC().UnixNano() / sonyflakeTimeUnit
}

func currentElapsedTime(startTime int64) int64 {
	return toSonyflakeTime(time.Now()) - startTime
}

func sleepTime(overtime int64) time.Duration {
	return time.Duration(overtime*sonyflakeTimeUnit) -
		time.Duration(time.Now().UTC().UnixNano()%sonyflakeTimeUnit)
}

func (sf *Sonyflake) toID() (uint64, error) {
	if sf.elapsedTime >= 1<<BitLenTime {
		return 0, ErrOverTimeLimit
	}

	return uint64(sf.elapsedTime)<<(BitLenSequence+BitLenMachineID) |
		uint64(sf.sequence)<<BitLenMachineID |
		uint64(sf.machineID), nil
}

func privateIPv4(interfaceAddrs types.InterfaceAddrs) (net.IP, error) {
	as, err := interfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, a := range as {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}

		ip := ipnet.IP.To4()
		if isPrivateIPv4(ip) {
			return ip, nil
		}
	}
	return nil, ErrNoPrivateAddress
}

func isPrivateIPv4(ip net.IP) bool {
	// Allow private IP addresses (RFC1918) and link-local addresses (RFC3927)
	return ip != nil &&
		(ip[0] == 10 || ip[0] == 172 && (ip[1] >= 16 && ip[1] < 32) || ip[0] == 192 && ip[1] == 168 || ip[0] == 169 && ip[1] == 254)
}

func lower16BitPrivateIP(interfaceAddrs types.InterfaceAddrs) (uint16, error) {
	ip, err := privateIPv4(interfaceAddrs)
	if err != nil {
		return 0, err
	}

	return uint16(ip[2])<<8 + uint16(ip[3]), nil
}

// ElapsedTime returns the elapsed time when the given Sonyflake ID was generated.
func ElapsedTime(id uint64) time.Duration {
	return time.Duration(elapsedTime(id) * sonyflakeTimeUnit)
}

func elapsedTime(id uint64) uint64 {
	return id >> (BitLenSequence + BitLenMachineID)
}

// SequenceNumber returns the sequence number of a Sonyflake ID.
func SequenceNumber(id uint64) uint64 {
	const maskSequence = uint64((1<<BitLenSequence - 1) << BitLenMachineID)
	return id & maskSequence >> BitLenMachineID
}

// MachineID returns the machine ID of a Sonyflake ID.
func MachineID(id uint64) uint64 {
	const maskMachineID = uint64(1<<BitLenMachineID - 1)
	return id & maskMachineID
}

// Compose creates a Sonyflake ID from its parts.
func Compose(sf *Sonyflake, t time.Time, sequence uint16, machineID uint16) (uint64, error) {
	elapsedTime := toSonyflakeTime(t.UTC()) - sf.startTime
	if elapsedTime < 0 {
		return 0, ErrStartTimeAhead
	}
	if elapsedTime >= 1<<BitLenTime {
		return 0, ErrOverTimeLimit
	}

	if sequence >= 1<<BitLenSequence {
		return 0, ErrInvalidSequence
	}

	return uint64(elapsedTime)<<(BitLenSequence+BitLenMachineID) |
		uint64(sequence)<<BitLenMachineID |
		uint64(machineID), nil
}

// Decompose returns a set of Sonyflake ID parts.
func Decompose(id uint64) map[string]uint64 {
	msb := id >> 63
	time := elapsedTime(id)
	sequence := SequenceNumber(id)
	machineID := MachineID(id)
	return map[string]uint64{
		"id":         id,
		"msb":        msb,
		"time":       time,
		"sequence":   sequence,
		"machine-id": machineID,
	}
}

// ID represents a Sonyflake ID.
type ID uint64

// SonyflakeID is an alias for ID.
type SonyflakeID = ID

var (
	_ sql.Scanner   = (*ID)(nil)
	_ driver.Valuer = ID(0)
)

// String returns the decimal string representation of the ID.
func (id ID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Int64 returns the ID as an int64.
func (id ID) Int64() int64 {
	return int64(id)
}

// Uint64 returns the ID as a uint64.
func (id ID) Uint64() uint64 {
	return uint64(id)
}

// ElapsedTime returns the elapsed time when the ID was generated.
func (id ID) ElapsedTime() time.Duration {
	return ElapsedTime(uint64(id))
}

// SequenceNumber returns the sequence number of the ID.
func (id ID) SequenceNumber() uint64 {
	return SequenceNumber(uint64(id))
}

// MachineID returns the machine ID of the ID.
func (id ID) MachineID() uint64 {
	return MachineID(uint64(id))
}

// Decompose returns a set of ID parts.
func (id ID) Decompose() map[string]uint64 {
	return Decompose(uint64(id))
}

// ParseID parses a decimal string representation into an ID.
func ParseID(s string) (ID, error) {
	if s == "" {
		return 0, errors.New("sonyflake: empty ID string")
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sonyflake: invalid ID %q: %w", s, err)
	}
	return ID(n), nil
}

// Value implements the database/sql/driver.Valuer interface.
// It returns the ID as an int64 suitable for SQL BIGINT storage.
func (id ID) Value() (driver.Value, error) {
	return int64(id), nil
}

// Scan implements the database/sql.Scanner interface.
// It supports scanning from int64, uint64, int, string, []byte, and nil.
func (id *ID) Scan(src interface{}) error {
	if id == nil {
		return errors.New("sonyflake: Scan called on nil pointer")
	}

	switch v := src.(type) {
	case nil:
		*id = 0
		return nil

	case int64:
		if v < 0 {
			return fmt.Errorf("sonyflake: cannot scan negative value %d into ID", v)
		}
		*id = ID(v)
		return nil

	case uint64:
		*id = ID(v)
		return nil

	case int:
		if v < 0 {
			return fmt.Errorf("sonyflake: cannot scan negative value %d into ID", v)
		}
		*id = ID(v)
		return nil

	case uint:
		*id = ID(v)
		return nil

	case int32:
		if v < 0 {
			return fmt.Errorf("sonyflake: cannot scan negative value %d into ID", v)
		}
		*id = ID(v)
		return nil

	case uint32:
		*id = ID(v)
		return nil

	case int16:
		if v < 0 {
			return fmt.Errorf("sonyflake: cannot scan negative value %d into ID", v)
		}
		*id = ID(v)
		return nil

	case uint16:
		*id = ID(v)
		return nil

	case int8:
		if v < 0 {
			return fmt.Errorf("sonyflake: cannot scan negative value %d into ID", v)
		}
		*id = ID(v)
		return nil

	case uint8:
		*id = ID(v)
		return nil

	case string:
		if v == "" {
			*id = 0
			return nil
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("sonyflake: cannot scan string %q into ID: %w", v, err)
		}
		*id = ID(n)
		return nil

	case []byte:
		if len(v) == 0 {
			*id = 0
			return nil
		}
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			*id = ID(n)
			return nil
		}
		if len(v) == 8 {
			*id = ID(binary.BigEndian.Uint64(v))
			return nil
		}
		return fmt.Errorf("sonyflake: cannot scan byte slice of length %d into ID", len(v))

	case ID:
		*id = v
		return nil

	case *ID:
		if v == nil {
			*id = 0
			return nil
		}
		*id = *v
		return nil

	default:
		return fmt.Errorf("sonyflake: cannot scan type %T into ID", src)
	}
}
