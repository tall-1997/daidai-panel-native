package service

import "testing"

func TestParseProcMeminfoUsesMemAvailable(t *testing.T) {
	total, used, free := parseProcMeminfo([]byte(`
MemTotal:       16384256 kB
MemFree:         1024000 kB
MemAvailable:    8192000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SReclaimable:     128000 kB
Shmem:             64000 kB
`))

	const kib = 1024
	if total != 16384256*kib {
		t.Fatalf("expected total memory from MemTotal, got %d", total)
	}
	if free != 8192000*kib {
		t.Fatalf("expected free memory to follow MemAvailable, got %d", free)
	}
	if used != (16384256-8192000)*kib {
		t.Fatalf("expected used memory to be total-available, got %d", used)
	}
}

func TestParseProcMeminfoFallsBackWithoutMemAvailable(t *testing.T) {
	total, used, free := parseProcMeminfo([]byte(`
MemTotal:        1000 kB
MemFree:          100 kB
Buffers:          200 kB
Cached:           300 kB
SReclaimable:      50 kB
Shmem:             25 kB
`))

	const kib = 1024
	expectedFree := uint64(625 * kib)
	expectedUsed := uint64(375 * kib)

	if total != 1000*kib {
		t.Fatalf("expected total memory from MemTotal, got %d", total)
	}
	if free != expectedFree {
		t.Fatalf("expected fallback available memory %d, got %d", expectedFree, free)
	}
	if used != expectedUsed {
		t.Fatalf("expected used memory %d, got %d", expectedUsed, used)
	}
}
