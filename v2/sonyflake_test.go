package sonyflake

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/sony/sonyflake/v2/mock"
	"github.com/sony/sonyflake/v2/types"
)

func TestNew(t *testing.T) {
	errGetMachineID := fmt.Errorf("failed to get machine id")

	testCases := []struct {
		name     string
		settings Settings
		err      error
	}{
		{
			name: "start time ahead",
			settings: Settings{
				StartTime: time.Now().Add(time.Minute),
			},
			err: ErrStartTimeAhead,
		},
		{
			name: "cannot get machine id",
			settings: Settings{
				MachineID: func() (int, error) {
					return 0, errGetMachineID
				},
			},
			err: errGetMachineID,
		},
		{
			name: "invalid machine id",
			settings: Settings{
				CheckMachineID: func(int) bool {
					return false
				},
			},
			err: ErrInvalidMachineID,
		},
		{
			name:     "success",
			settings: Settings{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sf, err := New(tc.settings)

			if !errors.Is(err, tc.err) {
				t.Fatalf("unexpected error: %v", err)
			}

			if err == nil && sf == nil {
				t.Fatal("sonyflake instance must be created")
			}
		})
	}
}

func newSonyflake(t *testing.T, st Settings) *Sonyflake {
	sf, err := New(st)
	if err != nil {
		t.Fatalf("failed to create sonyflake: %v", err)
	}
	return sf
}

func nextID(t *testing.T, sf *Sonyflake) int64 {
	id, err := sf.NextID()
	if err != nil {
		t.Fatalf("failed to generate id: %v", err)
	}
	return id
}

func defaultMachineID(t *testing.T) int {
	ip, err := lower16BitPrivateIP(defaultInterfaceAddrs)
	if err != nil {
		t.Fatalf("failed to get private ip address: %v", err)
	}
	return ip
}

func TestNextID(t *testing.T) {
	sf := newSonyflake(t, Settings{StartTime: time.Now()})

	sleepTime := time.Duration(50 * sonyflakeTimeUnit)
	time.Sleep(sleepTime)

	id := nextID(t, sf)

	actualTime := ElapsedTime(id)
	if actualTime < sleepTime || actualTime > sleepTime+sonyflakeTimeUnit {
		t.Errorf("unexpected time: %d", actualTime)
	}

	actualSequence := SequenceNumber(id)
	if actualSequence != 0 {
		t.Errorf("unexpected sequence: %d", actualSequence)
	}

	actualMachine := MachineID(id)
	if actualMachine != defaultMachineID(t) {
		t.Errorf("unexpected machine: %d", actualMachine)
	}

	fmt.Println("sonyflake id:", id)
	fmt.Println("decompose:", Decompose(id))
}

func TestNextID_InSequence(t *testing.T) {
	now := time.Now()
	sf := newSonyflake(t, Settings{StartTime: now})
	startTime := toSonyflakeTime(now)
	machineID := int64(defaultMachineID(t))

	var numID int
	var lastID int64
	var maxSeq int64

	currentTime := startTime
	for currentTime-startTime < 100 {
		id := nextID(t, sf)
		currentTime = toSonyflakeTime(time.Now())
		numID++

		if id == lastID {
			t.Fatal("duplicated id")
		}
		if id < lastID {
			t.Fatal("must increase with time")
		}
		lastID = id

		parts := Decompose(id)

		actualTime := parts["time"]
		overtime := startTime + actualTime - currentTime
		if overtime > 0 {
			t.Errorf("unexpected overtime: %d", overtime)
		}

		actualSequence := parts["sequence"]
		if actualSequence > maxSeq {
			maxSeq = actualSequence
		}

		actualMachine := parts["machine"]
		if actualMachine != machineID {
			t.Errorf("unexpected machine: %d", actualMachine)
		}
	}

	if maxSeq != 1<<BitLenSequence-1 {
		t.Errorf("unexpected max sequence: %d", maxSeq)
	}
	fmt.Println("max sequence:", maxSeq)
	fmt.Println("number of id:", numID)
}

func TestNextID_InParallel(t *testing.T) {
	sf1 := newSonyflake(t, Settings{MachineID: func() (int, error) { return 1, nil }})
	sf2 := newSonyflake(t, Settings{MachineID: func() (int, error) { return 2, nil }})

	numCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPU)
	fmt.Println("number of cpu:", numCPU)

	consumer := make(chan int64)

	const numID = 1000
	generate := func(sf *Sonyflake) {
		for i := 0; i < numID; i++ {
			id := nextID(t, sf)
			consumer <- id
		}
	}

	var numGenerator int
	for i := 0; i < numCPU/2; i++ {
		go generate(sf1)
		go generate(sf2)
		numGenerator += 2
	}

	set := make(map[int64]struct{})
	for i := 0; i < numID*numGenerator; i++ {
		id := <-consumer
		if _, ok := set[id]; ok {
			t.Fatal("duplicated id")
		}
		set[id] = struct{}{}
	}
	fmt.Println("number of id:", len(set))
}

func pseudoSleep(sf *Sonyflake, period time.Duration) {
	sf.startTime -= int64(period) / sonyflakeTimeUnit
}

func TestNextID_ReturnsError(t *testing.T) {
	sf := newSonyflake(t, Settings{StartTime: time.Now()})

	year := time.Duration(365*24) * time.Hour
	pseudoSleep(sf, time.Duration(174) * year)
	nextID(t, sf)

	pseudoSleep(sf, time.Duration(1) * year)
	_, err := sf.NextID()
	if err == nil {
		t.Errorf("time is not over")
	}
}

func TestPrivateIPv4(t *testing.T) {
	testCases := []struct {
		description    string
		expected       net.IP
		interfaceAddrs types.InterfaceAddrs
		error          error
	}{
		{
			description:    "returns an error",
			expected:       nil,
			interfaceAddrs: mock.NewFailingInterfaceAddrs(),
			error:          mock.ErrFailedToGetAddresses,
		},
		{
			description:    "empty address list",
			expected:       nil,
			interfaceAddrs: mock.NewNilInterfaceAddrs(),
			error:          ErrNoPrivateAddress,
		},
		{
			description:    "success",
			expected:       net.IP{192, 168, 0, 1},
			interfaceAddrs: mock.NewSuccessfulInterfaceAddrs(),
			error:          nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual, err := privateIPv4(tc.interfaceAddrs)

			if !errors.Is(err, tc.error) {
				t.Fatalf("unexpected error: %v", err)
			}

			if !net.IP.Equal(actual, tc.expected) {
				t.Errorf("unexpected ip: %s", actual)
			}
		})
	}
}

func TestLower16BitPrivateIP(t *testing.T) {
	testCases := []struct {
		description    string
		expected       int
		interfaceAddrs types.InterfaceAddrs
		error          error
	}{
		{
			description:    "returns an error",
			expected:       0,
			interfaceAddrs: mock.NewFailingInterfaceAddrs(),
			error:          mock.ErrFailedToGetAddresses,
		},
		{
			description:    "empty address list",
			expected:       0,
			interfaceAddrs: mock.NewNilInterfaceAddrs(),
			error:          ErrNoPrivateAddress,
		},
		{
			description:    "success",
			expected:       1,
			interfaceAddrs: mock.NewSuccessfulInterfaceAddrs(),
			error:          nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual, err := lower16BitPrivateIP(tc.interfaceAddrs)

			if !errors.Is(err, tc.error) {
				t.Fatalf("unexpected error: %v", err)
			}

			if actual != tc.expected {
				t.Errorf("unexpected ip: %d", actual)
			}
		})
	}
}
