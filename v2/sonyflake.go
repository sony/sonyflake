// Package sonyflake implements Sonyflake, a distributed unique ID generator inspired by Twitter's Snowflake.
//
// By default, a Sonyflake ID is composed of
//
//	39 bits for time in units of 10 msec
//	 8 bits for a sequence number
//	16 bits for a machine id
package sonyflake

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/sony/sonyflake/v2/types"
)

// Settings configures Sonyflake:
//
// BitsSequence is the bit length of a sequence number.
// If BitsSequence is 0, the default bit length is used, which is 8.
// If BitsSequence is 31 or more, an error is returned.
//
// BitsMachineID is the bit length of a machine ID.
// If BitsMachineID is 0, the default bit length is used, which is 16.
// If BitsMachineID is 31 or more, an error is returned.
//
// TimeUnit is the time unit of Sonyflake.
// If TimeUnit is 0, the default time unit is used, which is 10 msec.
// TimeUnit must be 1 msec or longer.
//
// StartTime is the time since which the Sonyflake time is defined as the elapsed time.
// If StartTime is 0, the start time of the Sonyflake instance is set to "2025-01-01 00:00:00 +0000 UTC".
// StartTime must be before the current time.
//
// MachineID returns the unique ID of a Sonyflake instance.
// If MachineID returns an error, the instance will not be created.
// If MachineID is nil, the default MachineID is used, which returns the lower 16 bits of the private IP address.
//
// CheckMachineID validates the uniqueness of a machine ID.
// If CheckMachineID returns false, the instance will not be created.
// If CheckMachineID is nil, no validation is done.
//
// The bit length of time is calculated by 63 - BitsSequence - BitsMachineID.
// If it is less than 32, an error is returned.
type Settings struct {
	BitsSequence   int
	BitsMachineID  int
	TimeUnit       time.Duration
	StartTime      time.Time
	MachineID      func() (int, error)
	CheckMachineID func(int) bool
}

// Sonyflake is a distributed unique ID generator.
type Sonyflake struct {
	mutex *sync.Mutex

	bitsTime     int
	bitsSequence int
	bitsMachine  int

	timeUnit    int64
	startTime   int64
	elapsedTime int64

	sequence int
	machine  int
}

var (
	ErrInvalidBitsTime      = errors.New("bit length for time must be 32 or more")
	ErrInvalidBitsSequence  = errors.New("invalid bit length for sequence number")
	ErrInvalidBitsMachineID = errors.New("invalid bit length for machine id")
	ErrInvalidTimeUnit      = errors.New("invalid time unit")
	ErrInvalidSequence      = errors.New("invalid sequence number")
	ErrInvalidMachineID     = errors.New("invalid machine id")
	ErrStartTimeAhead       = errors.New("start time is ahead")
	ErrOverTimeLimit        = errors.New("over the time limit")
	ErrNoPrivateAddress     = errors.New("no private ip address")
)

const (
	defaultTimeUnit = 1e7 // nsec, i.e. 10 msec

	defaultBitsTime     = 39
	defaultBitsSequence = 8
	defaultBitsMachine  = 16
)

var defaultInterfaceAddrs = net.InterfaceAddrs

// New returns a new Sonyflake configured with the given Settings.
// New returns an error in the following cases:
// - Settings.BitsSequence is less than 0 or greater than 30.
// - Settings.BitsMachineID is less than 0 or greater than 30.
// - Settings.BitsSequence + Settings.BitsMachineID is 32 or more.
// - Settings.TimeUnit is less than 1 msec.
// - Settings.StartTime is ahead of the current time.
// - Settings.MachineID returns an error.
// - Settings.CheckMachineID returns false.
func New(st Settings) (*Sonyflake, error) {
	if st.BitsSequence < 0 || st.BitsSequence > 30 {
		return nil, ErrInvalidBitsSequence
	}
	if st.BitsMachineID < 0 || st.BitsMachineID > 30 {
		return nil, ErrInvalidBitsMachineID
	}
	if st.TimeUnit < 0 || (st.TimeUnit > 0 && st.TimeUnit < time.Millisecond) {
		return nil, ErrInvalidTimeUnit
	}
	if st.StartTime.After(time.Now()) {
		return nil, ErrStartTimeAhead
	}

	sf := new(Sonyflake)
	sf.mutex = new(sync.Mutex)

	if st.BitsSequence == 0 {
		sf.bitsSequence = defaultBitsSequence
	} else {
		sf.bitsSequence = st.BitsSequence
	}

	if st.BitsMachineID == 0 {
		sf.bitsMachine = defaultBitsMachine
	} else {
		sf.bitsMachine = st.BitsMachineID
	}

	sf.bitsTime = 63 - sf.bitsSequence - sf.bitsMachine
	if sf.bitsTime < 32 {
		return nil, ErrInvalidBitsTime
	}

	if st.TimeUnit == 0 {
		sf.timeUnit = defaultTimeUnit
	} else {
		sf.timeUnit = int64(st.TimeUnit)
	}

	if st.StartTime.IsZero() {
		sf.startTime = sf.toInternalTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	} else {
		sf.startTime = sf.toInternalTime(st.StartTime)
	}

	sf.sequence = 1<<sf.bitsSequence - 1

	var err error
	if st.MachineID == nil {
		sf.machine, err = lower16BitPrivateIP(defaultInterfaceAddrs)
	} else {
		sf.machine, err = st.MachineID()
	}
	if err != nil {
		return nil, err
	}

	if sf.machine < 0 || sf.machine >= 1<<sf.bitsMachine {
		return nil, ErrInvalidMachineID
	}

	if st.CheckMachineID != nil && !st.CheckMachineID(sf.machine) {
		return nil, ErrInvalidMachineID
	}

	return sf, nil
}

// NextID generates a next unique ID as int64.
// After the Sonyflake time overflows, NextID returns an error.
func (sf *Sonyflake) NextID() (int64, error) {
	maskSequence := 1<<sf.bitsSequence - 1

	sf.mutex.Lock()
	defer sf.mutex.Unlock()

	current := sf.currentElapsedTime()
	if sf.elapsedTime < current {
		sf.elapsedTime = current
		sf.sequence = 0
	} else {
		sf.sequence = (sf.sequence + 1) & maskSequence
		if sf.sequence == 0 {
			sf.elapsedTime++
			overtime := sf.elapsedTime - current
			sf.sleep(overtime)
		}
	}

	return sf.toID()
}

func (sf *Sonyflake) toInternalTime(t time.Time) int64 {
	return t.UTC().UnixNano() / sf.timeUnit
}

func (sf *Sonyflake) currentElapsedTime() int64 {
	return sf.toInternalTime(time.Now()) - sf.startTime
}

func (sf *Sonyflake) sleep(overtime int64) {
	sleepTime := time.Duration(overtime*sf.timeUnit) -
		time.Duration(time.Now().UTC().UnixNano()%sf.timeUnit)
	time.Sleep(sleepTime)
}

func (sf *Sonyflake) toID() (int64, error) {
	if sf.elapsedTime >= 1<<sf.bitsTime {
		return 0, ErrOverTimeLimit
	}

	return sf.elapsedTime<<(sf.bitsSequence+sf.bitsMachine) |
		int64(sf.sequence)<<sf.bitsMachine |
		int64(sf.machine), nil
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

func lower16BitPrivateIP(interfaceAddrs types.InterfaceAddrs) (int, error) {
	ip, err := privateIPv4(interfaceAddrs)
	if err != nil {
		return 0, err
	}

	return int(ip[2])<<8 + int(ip[3]), nil
}

// ToTime returns the time when the given ID was generated.
func (sf *Sonyflake) ToTime(id int64) time.Time {
	return time.Unix(0, (sf.startTime+sf.timePart(id))*sf.timeUnit)
}

// Compose creates a Sonyflake ID from its components.
// The time parameter should be the time when the ID was generated.
// The sequence parameter should be between 0 and 2^BitsSequence-1 (inclusive).
// The machineID parameter should be between 0 and 2^BitsMachineID-1 (inclusive).
func (sf *Sonyflake) Compose(t time.Time, sequence, machineID int) (int64, error) {
	elapsedTime := sf.toInternalTime(t.UTC()) - sf.startTime
	if elapsedTime < 0 {
		return 0, ErrStartTimeAhead
	}
	if elapsedTime >= 1<<sf.bitsTime {
		return 0, ErrOverTimeLimit
	}

	if sequence < 0 || sequence >= 1<<sf.bitsSequence {
		return 0, ErrInvalidSequence
	}

	if machineID < 0 || machineID >= 1<<sf.bitsMachine {
		return 0, ErrInvalidMachineID
	}

	return elapsedTime<<(sf.bitsSequence+sf.bitsMachine) |
		int64(sequence)<<sf.bitsMachine |
		int64(machineID), nil
}

// Decompose returns a set of Sonyflake ID parts.
func (sf *Sonyflake) Decompose(id int64) map[string]int64 {
	time := sf.timePart(id)
	sequence := sf.sequencePart(id)
	machine := sf.machinePart(id)
	return map[string]int64{
		"id":       id,
		"time":     time,
		"sequence": sequence,
		"machine":  machine,
	}
}

func (sf *Sonyflake) timePart(id int64) int64 {
	return id >> (sf.bitsSequence + sf.bitsMachine)
}

func (sf *Sonyflake) sequencePart(id int64) int64 {
	maskSequence := int64((1<<sf.bitsSequence - 1) << sf.bitsMachine)
	return (id & maskSequence) >> sf.bitsMachine
}

func (sf *Sonyflake) machinePart(id int64) int64 {
	maskMachine := int64(1<<sf.bitsMachine - 1)
	return id & maskMachine
}
