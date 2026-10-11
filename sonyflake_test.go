package sonyflake

import (
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
				MachineID: func() (uint16, error) {
					return 0, genError
				},
			},
			err: genError,
		},
		{
			name: "failure: invalid machine ID",
			settings: Settings{
				CheckMachineID: func(uint16) bool {
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

func TestCompose(t *testing.T) {
	var st Settings
	st.StartTime = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	sf, err := New(st)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	sequence := uint16(123)
	machineID := uint16(456)

	id, err := Compose(sf, now, sequence, machineID)
	if err != nil {
		t.Fatal(err)
	}

	parts := Decompose(id)

	actualTime := toSonyflakeTime(now) - toSonyflakeTime(st.StartTime)
	if parts["time"] != uint64(actualTime) {
		t.Errorf("unexpected time: %d", parts["time"])
	}

	if parts["sequence"] != uint64(sequence) {
		t.Errorf("unexpected sequence: %d", parts["sequence"])
	}

	if parts["machine-id"] != uint64(machineID) {
		t.Errorf("unexpected machine id: %d", parts["machine-id"])
	}
}

func TestIDInterfaces(t *testing.T) {
	var (
		_ sql.Scanner   = (*ID)(nil)
		_ driver.Valuer = ID(0)
		_ sql.Scanner   = (*SonyflakeID)(nil)
		_ driver.Valuer = SonyflakeID(0)
	)
}

func TestIDValue(t *testing.T) {
	tests := []struct {
		name     string
		id       ID
		expected driver.Value
	}{
		{
			name:     "zero value",
			id:       ID(0),
			expected: int64(0),
		},
		{
			name:     "standard id",
			id:       ID(855693107),
			expected: int64(855693107),
		},
		{
			name:     "large id within 63 bits",
			id:       ID(math.MaxInt64),
			expected: int64(math.MaxInt64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.id.Value()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if val != tt.expected {
				t.Fatalf("expected value %v (%T), got %v (%T)", tt.expected, tt.expected, val, val)
			}
		})
	}

	t.Run("from generated id", func(t *testing.T) {
		genSF, err := New(Settings{})
		if err != nil {
			t.Fatalf("failed to create Sonyflake: %v", err)
		}
		rawID, err := genSF.NextID()
		if err != nil {
			t.Fatalf("failed to generate ID: %v", err)
		}
		val, err := ID(rawID).Value()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != int64(rawID) {
			t.Fatalf("expected %d, got %v", int64(rawID), val)
		}
	})
}

func TestIDScanValid(t *testing.T) {
	binaryID := make([]byte, 8)
	binary.BigEndian.PutUint64(binaryID, 855693107)

	tests := []struct {
		name     string
		src      interface{}
		expected ID
	}{
		{
			name:     "nil src",
			src:      nil,
			expected: 0,
		},
		{
			name:     "int64 zero",
			src:      int64(0),
			expected: 0,
		},
		{
			name:     "int64 positive",
			src:      int64(855693107),
			expected: 855693107,
		},
		{
			name:     "uint64 zero",
			src:      uint64(0),
			expected: 0,
		},
		{
			name:     "uint64 positive",
			src:      uint64(855693107),
			expected: 855693107,
		},
		{
			name:     "int positive",
			src:      int(855693107),
			expected: 855693107,
		},
		{
			name:     "uint positive",
			src:      uint(855693107),
			expected: 855693107,
		},
		{
			name:     "int32 positive",
			src:      int32(12345),
			expected: 12345,
		},
		{
			name:     "uint32 positive",
			src:      uint32(12345),
			expected: 12345,
		},
		{
			name:     "int16 positive",
			src:      int16(1234),
			expected: 1234,
		},
		{
			name:     "uint16 positive",
			src:      uint16(1234),
			expected: 1234,
		},
		{
			name:     "int8 positive",
			src:      int8(123),
			expected: 123,
		},
		{
			name:     "uint8 positive",
			src:      uint8(123),
			expected: 123,
		},
		{
			name:     "empty string",
			src:      "",
			expected: 0,
		},
		{
			name:     "valid decimal string",
			src:      "855693107",
			expected: 855693107,
		},
		{
			name:     "empty byte slice",
			src:      []byte{},
			expected: 0,
		},
		{
			name:     "decimal byte slice",
			src:      []byte("855693107"),
			expected: 855693107,
		},
		{
			name:     "8-character decimal byte slice",
			src:      []byte("12345678"),
			expected: 12345678,
		},
		{
			name:     "8-byte big-endian binary",
			src:      binaryID,
			expected: 855693107,
		},
		{
			name:     "ID type direct",
			src:      ID(855693107),
			expected: 855693107,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id ID
			if err := id.Scan(tt.src); err != nil {
				t.Fatalf("unexpected scan error: %v", err)
			}
			if id != tt.expected {
				t.Fatalf("expected %d, got %d", tt.expected, id)
			}
		})
	}

	t.Run("pointer to ID src", func(t *testing.T) {
		srcID := ID(999999)
		var id ID
		if err := id.Scan(&srcID); err != nil {
			t.Fatalf("unexpected scan error: %v", err)
		}
		if id != srcID {
			t.Fatalf("expected %d, got %d", srcID, id)
		}

		var nilID *ID
		if err := id.Scan(nilID); err != nil {
			t.Fatalf("unexpected scan error: %v", err)
		}
		if id != 0 {
			t.Fatalf("expected 0, got %d", id)
		}
	})

	t.Run("resets existing value when scanning nil", func(t *testing.T) {
		id := ID(12345)
		if err := id.Scan(nil); err != nil {
			t.Fatalf("unexpected scan error: %v", err)
		}
		if id != 0 {
			t.Fatalf("expected id to reset to 0, got %d", id)
		}
	})
}

func TestIDScanInvalid(t *testing.T) {
	tests := []struct {
		name string
		src  interface{}
	}{
		{
			name: "negative int64",
			src:  int64(-1),
		},
		{
			name: "negative int",
			src:  int(-10),
		},
		{
			name: "negative int32",
			src:  int32(-100),
		},
		{
			name: "negative int16",
			src:  int16(-1000),
		},
		{
			name: "negative int8",
			src:  int8(-10),
		},
		{
			name: "non-numeric string",
			src:  "invalid-id",
		},
		{
			name: "negative string",
			src:  "-12345",
		},
		{
			name: "float string",
			src:  "123.45",
		},
		{
			name: "overflow string",
			src:  "18446744073709551616",
		},
		{
			name: "non-numeric byte slice",
			src:  []byte("invalid"),
		},
		{
			name: "invalid length byte slice",
			src:  []byte{1, 2, 3, 4},
		},
		{
			name: "float64 unsupported",
			src:  float64(123.45),
		},
		{
			name: "bool unsupported",
			src:  true,
		},
		{
			name: "struct unsupported",
			src:  struct{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id ID
			if err := id.Scan(tt.src); err == nil {
				t.Fatalf("expected error scanning %v (%T), got nil", tt.src, tt.src)
			}
		})
	}

	t.Run("nil pointer receiver returns error", func(t *testing.T) {
		var id *ID
		if err := id.Scan(int64(123)); err == nil {
			t.Fatal("expected error on nil pointer receiver, got nil")
		}
	})
}

func TestIDHelpers(t *testing.T) {
	const testRawID uint64 = 855693107
	id := ID(testRawID)

	if id.String() != "855693107" {
		t.Fatalf("unexpected String(): %s", id.String())
	}
	if id.Int64() != int64(testRawID) {
		t.Fatalf("unexpected Int64(): %d", id.Int64())
	}
	if id.Uint64() != testRawID {
		t.Fatalf("unexpected Uint64(): %d", id.Uint64())
	}
	if id.ElapsedTime() != ElapsedTime(testRawID) {
		t.Fatalf("unexpected ElapsedTime(): %v", id.ElapsedTime())
	}
	if id.SequenceNumber() != SequenceNumber(testRawID) {
		t.Fatalf("unexpected SequenceNumber(): %d", id.SequenceNumber())
	}
	if id.MachineID() != MachineID(testRawID) {
		t.Fatalf("unexpected MachineID(): %d", id.MachineID())
	}

	parts := id.Decompose()
	expectedParts := Decompose(testRawID)
	for k, v := range expectedParts {
		if parts[k] != v {
			t.Fatalf("unexpected Decompose[%s]: want %d, got %d", k, v, parts[k])
		}
	}

	t.Run("ParseID", func(t *testing.T) {
		parsed, err := ParseID("855693107")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed != id {
			t.Fatalf("expected %d, got %d", id, parsed)
		}

		if _, err := ParseID(""); err == nil {
			t.Fatal("expected error for empty string, got nil")
		}
		if _, err := ParseID("not-a-number"); err == nil {
			t.Fatal("expected error for non-number string, got nil")
		}
	})

	t.Run("Sonyflake Next method", func(t *testing.T) {
		genSF, err := New(Settings{})
		if err != nil {
			t.Fatalf("failed to create Sonyflake: %v", err)
		}
		next, err := genSF.Next()
		if err != nil {
			t.Fatalf("unexpected error from Next(): %v", err)
		}
		if next == 0 {
			t.Fatal("expected non-zero ID from Next()")
		}
	})
}
