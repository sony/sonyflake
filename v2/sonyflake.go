// Package sonyflake implements Sonyflake, a distributed unique ID generator inspired by Twitter's Snowflake.
//
// A Sonyflake ID is composed of
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

// These constants are the bit lengths of Sonyflake ID parts.
const (
	BitLenTime     = 39                               // bit length of time
	BitLenSequence = 8                                // bit length of sequence number
	BitLenMachine  = 63 - BitLenTime - BitLenSequence // bit length of machine id
)

// Settings configures Sonyflake:
//
// TimeUnit is the time unit of Sonyflake.
// If TimeUnit is 0, the default time unit is used, which is 10 msec.
// TimeUnit must be equal to or greater than 1 msec.
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
type Settings struct {
	TimeUnit       time.Duration
	StartTime      time.Time
	MachineID      func() (int, error)
	CheckMachineID func(int) bool
}

// Sonyflake is a distributed unique ID generator.
type Sonyflake struct {
	mutex       *sync.Mutex
	timeUnit    int64
	startTime   int64
	elapsedTime int64
	sequence    int
	machine     int
}

var (
	ErrStartTimeAhead   = errors.New("start time is ahead of now")
	ErrNoPrivateAddress = errors.New("no private ip address")
	ErrOverTimeLimit    = errors.New("over the time limit")
	ErrInvalidMachineID = errors.New("invalid machine id")
	ErrInvalidTimeUnit  = errors.New("invalid time unit")
)

const defaultTimeUnit = 1e7 // nsec, i.e. 10 msec

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
	sf.sequence = 1<<BitLenSequence - 1

	if st.TimeUnit == 0 {
		sf.timeUnit = defaultTimeUnit
	} else if st.TimeUnit >= time.Millisecond {
		sf.timeUnit = int64(st.TimeUnit)
	} else {
		return nil, ErrInvalidTimeUnit
	}

	if st.StartTime.IsZero() {
		sf.startTime = sf.toInternalTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	} else {
		sf.startTime = sf.toInternalTime(st.StartTime)
	}

	var err error
	if st.MachineID == nil {
		sf.machine, err = lower16BitPrivateIP(defaultInterfaceAddrs)
	} else {
		sf.machine, err = st.MachineID()
	}
	if err != nil {
		return nil, err
	}

	if st.CheckMachineID != nil && !st.CheckMachineID(sf.machine) {
		return nil, ErrInvalidMachineID
	}

	return sf, nil
}

// NextID generates a next unique ID as int64.
// After the Sonyflake time overflows, NextID returns an error.
func (sf *Sonyflake) NextID() (int64, error) {
	const maskSequence = 1<<BitLenSequence - 1

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
	if sf.elapsedTime >= 1<<BitLenTime {
		return 0, ErrOverTimeLimit
	}

	return sf.elapsedTime<<(BitLenSequence+BitLenMachine) |
		int64(sf.sequence)<<BitLenMachine |
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

func (sf *Sonyflake) ToTime(id int64) time.Time {
	return time.Unix(0, (sf.startTime+Time(id))*sf.timeUnit)
}

// Time returns the Sonyflake time when the given ID was generated.
func Time(id int64) int64 {
	return id >> (BitLenSequence + BitLenMachine)
}

// SequenceNumber returns the sequence number of a Sonyflake ID.
func SequenceNumber(id int64) int {
	const maskSequence = int64((1<<BitLenSequence - 1) << BitLenMachine)
	return int((id & maskSequence) >> BitLenMachine)
}

// MachineID returns the machine ID of a Sonyflake ID.
func MachineID(id int64) int {
	const maskMachine = int64(1<<BitLenMachine - 1)
	return int(id & maskMachine)
}

// Decompose returns a set of Sonyflake ID parts.
func Decompose(id int64) map[string]int64 {
	time := Time(id)
	sequence := SequenceNumber(id)
	machine := MachineID(id)
	return map[string]int64{
		"id":       id,
		"time":     time,
		"sequence": int64(sequence),
		"machine":  int64(machine),
	}
}
