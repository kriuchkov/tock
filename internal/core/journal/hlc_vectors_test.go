package journal_test

// hlcVectors returns the clock vectors.
func hlcVectors() []hlcVector {
	return []hlcVector{
		{
			File: "parse",
			Name: "a clock is 16 digits, a dot and 6 digits, ASCII only",
			Valid: []string{
				"0001794985200000.000000",
				"0000000000000000.000000",
				"9999999999999999.999999",
			},
			Invalid: []string{
				"",
				"1794985200000.000000",
				"0001794985200000.00000",
				"0001794985200000.0000001",
				"00001794985200000.000000",
				"0001794985200000-000000",
				"0001794985200000.00000a",
				"+001794985200000.000000",
				" 001794985200000.000000",
				"000179498520000٠.000000",
			},
		},
		{
			File: "order",
			Name: "changes are ordered by clock, then by dev; comparing the strings compares the clocks",
			Order: [][2]string{
				{"0001794985200000.000000", devB},
				{"0001794985200000.000000", devA},
				{"0001794985200000.000001", devB},
				{"0001794985200000.000010", devB},
				{"0001794985200001.000000", devB},
				{"0001794985200001.000000", devA},
				{"0001794985500000.000000", devB},
			},
		},
		{
			File: "new",
			Name: "a new change takes the current time, or counts on when time stands still or goes back",
			Steps: []hlcStep{
				{Op: "new", Now: 1794985200000, Expect: "0001794985200000.000000"},
				{Op: "new", Now: 1794985200000, Expect: "0001794985200000.000001"},
				{Op: "new", Now: 1794985199000, Expect: "0001794985200000.000002"},
				{Op: "new", Now: 1794985200005, Expect: "0001794985200005.000000"},
			},
		},
		{
			File: "counter-overflow",
			Name: "a counter past 999999 moves the wall on by 1 ms",
			Last: "0001794985200000.999998",
			Steps: []hlcStep{
				{Op: "new", Now: 1794985200000, Expect: "0001794985200000.999999"},
				{Op: "new", Now: 1794985200000, Expect: "0001794985200001.000000"},
				{Op: "new", Now: 1794985200000, Expect: "0001794985200001.000001"},
			},
		},
		{
			File: "read",
			Name: "a clock read advances the device's clock, unless it is more than 24 hours ahead",
			Last: "0001794985200000.000000",
			Steps: []hlcStep{
				{Op: "read", HLC: "0001794985500000.000003", Now: 1794985200000, Expect: "0001794985500000.000003"},
				{Op: "new", Now: 1794985200000, Expect: "0001794985500000.000004"},
				{Op: "read", HLC: "0001794985200000.000009", Now: 1794985200000, Expect: "0001794985500000.000004"},
				{Op: "read", HLC: "0001795071600001.000000", Now: 1794985200000, Expect: "0001794985500000.000004"},
				{Op: "new", Now: 1794985200000, Expect: "0001794985500000.000005"},
				{Op: "read", HLC: "0001795071600000.000000", Now: 1794985200000, Expect: "0001795071600000.000000"},
				{Op: "new", Now: 1794985200000, Expect: "0001795071600000.000001"},
			},
		},
	}
}
