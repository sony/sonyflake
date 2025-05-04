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

var sf *Sonyflake

var startTime int64
var machineID int

func init() {
	var st Settings
	st.StartTime = time.Now()

	var err error
	sf, err = New(st)
	if err != nil {
		panic(err)
	}

	startTime = toSonyflakeTime(st.StartTime)

	ip, _ := lower16BitPrivateIP(defaultInterfaceAddrs)
	machineID = int(ip)
}

func nextID(t *testing.T) int64 {
	id, err := sf.NextID()
	if err != nil {
		t.Fatal("id not generated")
	}
	return id
}

func TestNew(t *testing.T) {
	genError := fmt.Errorf("an error occurred while generating ID")

	tests := []struct {
		name     string
		settings Settings
		err      error
	}{
		{
			name: "failure: time ahead",
			settings: Settings{
				StartTime: time.Now().Add(time.Minute),
			},
			err: ErrStartTimeAhead,
		},
		{
			name: "failure: machine ID",
			settings: Settings{
				MachineID: func() (int, error) {
					return 0, genError
				},
			},
			err: genError,
		},
		{
			name: "failure: invalid machine ID",
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

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sonyflake, err := New(test.settings)

			if !errors.Is(err, test.err) {
				t.Fatalf("unexpected value, want %#v, got %#v", test.err, err)
			}

			if sonyflake == nil && err == nil {
				t.Fatal("unexpected value, sonyflake should not be nil")
			}
		})
	}
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
	if int(actualMachineID) != machineID {
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
	var lastID int64
	var maxSequence int64

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

		actualMachineID := parts["machine"]
		if int(actualMachineID) != machineID {
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

	consumer := make(chan int64)

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

			if net.IP.Equal(actual, tc.expected) {
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
		expected       int
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
