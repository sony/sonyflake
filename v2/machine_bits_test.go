package sonyflake

import (
	"net"
	"testing"

	"github.com/sony/sonyflake/v2/mock"
)

// TestDefaultMachineIDWithCustomBits tests that when using default machine ID
// with custom BitsMachineID, the machine ID is properly masked to fit within
// the specified bit length.
func TestDefaultMachineIDWithCustomBits(t *testing.T) {
	testCases := []struct {
		name          string
		bitsMachineID int
		mockIP        net.IP
		expectError   bool
	}{
		{
			name:          "10 bits machine ID with IP that fits",
			bitsMachineID: 10,
			mockIP:        net.IP{192, 168, 0, 1}, // lower 16 bits = 1, fits in 10 bits
			expectError:   false,
		},
		{
			name:          "10 bits machine ID with IP that exceeds without masking",
			bitsMachineID: 10,
			mockIP:        net.IP{192, 168, 255, 255}, // lower 16 bits = 65535, needs masking to fit in 10 bits
			expectError:   false,
		},
		{
			name:          "8 bits machine ID",
			bitsMachineID: 8,
			mockIP:        net.IP{192, 168, 100, 200}, // lower 16 bits = 25800
			expectError:   false,
		},
		{
			name:          "default 16 bits",
			bitsMachineID: 0, // will use default 16
			mockIP:        net.IP{192, 168, 255, 255},
			expectError:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock that returns our test IP
			mockInterfaceAddrs := mock.NewInterfaceAddrsWithIP(tc.mockIP)

			settings := Settings{
				BitsMachineID: tc.bitsMachineID,
			}

			// Temporarily replace the default interface addrs function
			oldDefaultInterfaceAddrs := defaultInterfaceAddrs
			defaultInterfaceAddrs = mockInterfaceAddrs
			defer func() { defaultInterfaceAddrs = oldDefaultInterfaceAddrs }()

			sf, err := New(settings)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if sf == nil {
					t.Error("sonyflake instance should not be nil")
				}

				// Verify the machine ID fits within the specified bits
				expectedBits := tc.bitsMachineID
				if expectedBits == 0 {
					expectedBits = defaultBitsMachine
				}
				maxMachineID := 1 << expectedBits
				if sf.machine >= maxMachineID {
					t.Errorf("machine ID %d exceeds max for %d bits (%d)", sf.machine, expectedBits, maxMachineID)
				}
			}
		})
	}
}
