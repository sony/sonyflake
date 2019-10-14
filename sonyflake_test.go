package sonyflake

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/deckarep/golang-set"
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

	ip, _ := lower16BitPrivateIP()
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
	sleepTime := uint64(50)
	time.Sleep(time.Duration(sleepTime) * 10 * time.Millisecond)

	id := nextID(t)
	parts := Decompose(id)

	actualMSB := parts["msb"]
	if actualMSB != 0 {
		t.Errorf("unexpected msb: %d", actualMSB)
	}

	actualTime := parts["time"]
	if actualTime < sleepTime || actualTime > sleepTime+1 {
		t.Errorf("unexpected time: %d", actualTime)
	}

	actualSequence := parts["sequence"]
	if actualSequence != 0 {
		t.Errorf("unexpected sequence: %d", actualSequence)
	}

	actualMachineID := parts["machine-id"]
	if actualMachineID != machineID {
		t.Errorf("unexpected machine id: %d", actualMachineID)
	}

	fmt.Println("sonyflake id:", id)
	fmt.Println("decompose:", parts)
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

		if id <= lastID {
			t.Fatal("duplicated id")
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

	set := mapset.NewSet()
	for i := 0; i < numID*numGenerator; i++ {
		id := <-consumer
		if set.Contains(id) {
			t.Fatal("duplicated id")
		} else {
			set.Add(id)
		}
	}
	fmt.Println("number of id:", set.Cardinality())
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

// TestSortableID will test if the generated ID is always sortable even when you run in Go routine.
// So, if you have a system that needs to generate id in application level in thread, the generated ID
// can be sorted by those generated ids.
// Sorted by ID will resemble as sorted by time.
func TestSortableID(t *testing.T)  {
	numCPU := runtime.NumCPU()
	runtime.GOMAXPROCS(numCPU)
	fmt.Println("number of cpu:", numCPU)

	var st Settings
	st.StartTime = time.Now()

	generator := NewSonyflake(st)
	if generator == nil {
		t.Error(fmt.Errorf("generator is nil"))
		t.Fail()
		return
	}

	type uids struct {
		mutex *sync.RWMutex
		ids   map[int64]uint64
	}

	var (
		uuids = &uids{
			mutex: new(sync.RWMutex),
			ids:   make(map[int64]uint64),
		}

		N  = 10001
		wg = new(sync.WaitGroup)
	)

	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(x int, gen *Sonyflake, ids *uids) {
			ids.mutex.Lock()
			defer func() {
				ids.mutex.Unlock()
				wg.Done()
			}()


			id, err := gen.NextID()
			if err != nil {
				t.Error(err.Error())
				t.Fail()
			}

			t := time.Now().UnixNano()

			ids.ids[t] = id

		}(i, generator, uuids)
	}
	wg.Wait()

	times := make([]int64, 0)
	for k, _ := range uuids.ids {
		times = append(times, k)
	}

	// order by desc
	sort.Slice(times, func(i, j int) bool {
		return times[i] > times[j]
	})

	for i := 1; i < len(times); i++ {
		if uuids.ids[times[i-1]] < uuids.ids[times[i]] {
			t.Error("recent generated id is lower than previous generated", uuids.ids[times[i-1]], "vs", uuids.ids[times[i]])
			t.Fail()
		}
	}
}