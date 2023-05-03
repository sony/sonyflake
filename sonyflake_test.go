package sonyflake

import (
	"bytes"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/sony/sonyflake/mock"
	"github.com/sony/sonyflake/types"
)

var sf *Sonyflake

var startTime int64
var machineID uint64

func init() {
	var st Settings
	st.StartTime = time.Now()

	sf = NewSonyflake(st)
	if sf == nil {
		panic("sonyflake not created")
	}

	startTime = toSonyflakeTime(st.StartTime)

	ip, _ := lower16BitPrivateIP(defaultInterfaceAddrs)
	machineID = uint64(ip)
}

func nextID(t *testing.T) uint64 {
	id, err := sf.NextID()
	if err != nil {
		t.Fatal("id not generated")
	}
	return id
}

func TestSonyflakeOnce(t *testing.T) {
	sleepTime := time.Duration(50 * sonyflakeTimeUnit)
	time.Sleep(sleepTime)

	id := nextID(t)

	actualTime := ElapsedTime(id)
	if actualTime < sleepTime || actualTime > sleepTime+sonyflakeTimeUnit {
		t.Errorf("unexpected time: %d", actualTime)
	}

	actualSequence := SequenceNumber(id)
	if actualSequence != 0 {
		t.Errorf("unexpected sequence: %d", actualSequence)
	}

	actualMachineID := MachineID(id)
	if actualMachineID != machineID {
		t.Errorf("unexpected machine id: %d", actualMachineID)
	}

	fmt.Println("sonyflake id:", id)
	fmt.Println("decompose:", Decompose(id))
}

func currentTime() int64 {
	return toSonyflakeTime(time.Now())
}

func TestSonyflakeFor10Sec(t *testing.T) {
	var numID uint32
	var lastID uint64
	var maxSequence uint64

	initial := currentTime()
	current := initial
	for current-initial < 1000 {
		id := nextID(t)
		parts := Decompose(id)
		numID++

		if id == lastID {
			t.Fatal("duplicated id")
		}
		if id < lastID {
			t.Fatal("must increase with time")
		}
		lastID = id

		current = currentTime()

		actualMSB := parts["msb"]
		if actualMSB != 0 {
			t.Errorf("unexpected msb: %d", actualMSB)
		}

		actualTime := int64(parts["time"])
		overtime := startTime + actualTime - current
		if overtime > 0 {
			t.Errorf("unexpected overtime: %d", overtime)
		}

		actualSequence := parts["sequence"]
		if maxSequence < actualSequence {
			maxSequence = actualSequence
		}

		actualMachineID := parts["machine-id"]
		if actualMachineID != machineID {
			t.Errorf("unexpected machine id: %d", actualMachineID)
		}
	}

	if maxSequence != 1<<BitLenSequence-1 {
		t.Errorf("unexpected max sequence: %d", maxSequence)
	}
	fmt.Println("max sequence:", maxSequence)
	fmt.Println("number of id:", numID)
}

func TestSonyflakeInParallel(t *testing.T) {
	numCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPU)
	fmt.Println("number of cpu:", numCPU)

	consumer := make(chan uint64)

	const numID = 10000
	generate := func() {
		for i := 0; i < numID; i++ {
			consumer <- nextID(t)
		}
	}

	const numGenerator = 10
	for i := 0; i < numGenerator; i++ {
		go generate()
	}

	set := make(map[uint64]struct{})
	for i := 0; i < numID*numGenerator; i++ {
		id := <-consumer
		if _, ok := set[id]; ok {
			t.Fatal("duplicated id")
		}
		set[id] = struct{}{}
	}
	fmt.Println("number of id:", len(set))
}

func TestNilSonyflake(t *testing.T) {
	var startInFuture Settings
	startInFuture.StartTime = time.Now().Add(time.Duration(1) * time.Minute)
	if NewSonyflake(startInFuture) != nil {
		t.Errorf("sonyflake starting in the future")
	}

	var noMachineID Settings
	noMachineID.MachineID = func() (uint16, error) {
		return 0, fmt.Errorf("no machine id")
	}
	if NewSonyflake(noMachineID) != nil {
		t.Errorf("sonyflake with no machine id")
	}

	var invalidMachineID Settings
	invalidMachineID.CheckMachineID = func(uint16) bool {
		return false
	}
	if NewSonyflake(invalidMachineID) != nil {
		t.Errorf("sonyflake with invalid machine id")
	}
}

func pseudoSleep(period time.Duration) {
	sf.startTime -= int64(period) / sonyflakeTimeUnit
}

func TestNextIDError(t *testing.T) {
	year := time.Duration(365*24) * time.Hour
	pseudoSleep(time.Duration(174) * year)
	nextID(t)

	pseudoSleep(time.Duration(1) * year)
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
		error          string
	}{
		{
			description:    "InterfaceAddrs returns an error",
			expected:       nil,
			interfaceAddrs: mock.NewFailingInterfaceAddrs(),
			error:          "test error",
		},
		{
			description:    "InterfaceAddrs returns an empty or nil list",
			expected:       nil,
			interfaceAddrs: mock.NewNilInterfaceAddrs(),
			error:          "no private ip address",
		},
		{
			description:    "InterfaceAddrs returns one or more IPs",
			expected:       net.IP{192, 168, 0, 1},
			interfaceAddrs: mock.NewSuccessfulInterfaceAddrs(),
			error:          "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual, err := privateIPv4(tc.interfaceAddrs)

			if (err != nil) && (tc.error == "") {
				t.Errorf("expected no error, but got: %s", err)
				return
			} else if (err != nil) && (tc.error != "") {
				return
			}

			if bytes.Equal(actual, tc.expected) {
				return
			} else {
				t.Errorf("error: expected: %s, but got: %s", tc.expected, actual)
			}
		})
	}
}

func TestLower16BitPrivateIP(t *testing.T) {
	testCases := []struct {
		description    string
		expected       uint16
		interfaceAddrs types.InterfaceAddrs
		error          string
	}{
		{
			description:    "InterfaceAddrs returns an empty or nil list",
			expected:       0,
			interfaceAddrs: mock.NewNilInterfaceAddrs(),
			error:          "no private ip address",
		},
		{
			description:    "InterfaceAddrs returns one or more IPs",
			expected:       1,
			interfaceAddrs: mock.NewSuccessfulInterfaceAddrs(),
			error:          "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual, err := lower16BitPrivateIP(tc.interfaceAddrs)

			if (err != nil) && (tc.error == "") {
				t.Errorf("expected no error, but got: %s", err)
				return
			} else if (err != nil) && (tc.error != "") {
				return
			}

			if actual == tc.expected {
				return
			} else {
				t.Errorf("error: expected: %v, but got: %v", tc.expected, actual)
			}
		})
	}
}

func TestSonyflakeTimeUnit(t *testing.T) {
	if time.Duration(sonyflakeTimeUnit) != 10*time.Millisecond {
		t.Errorf("unexpected time unit")
	}
}
