package metrics

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Snapshot struct {
	Available bool             `json:"available"`
	VMStat    map[string]int64 `json:"vmstat,omitempty"`
	Memory    map[string]int64 `json:"memory_stat,omitempty"`
	PSI       map[string]int64 `json:"psi_total_us,omitempty"`
}

type Delta struct {
	Available bool             `json:"available"`
	VMStat    map[string]int64 `json:"vmstat,omitempty"`
	Memory    map[string]int64 `json:"memory_stat,omitempty"`
	PSI       map[string]int64 `json:"psi_total_us,omitempty"`
}

var vmKeys = map[string]bool{
	"pgmajfault":               true,
	"workingset_refault_file": true,
	"workingset_activate_file": true,
	"workingset_restore_file": true,
	"pgscan_kswapd":           true,
	"pgscan_direct":           true,
	"pgsteal_kswapd":          true,
	"pgsteal_direct":          true,
}

var memoryKeys = map[string]bool{
	"file":                    true,
	"active_file":             true,
	"inactive_file":           true,
	"workingset_refault_file": true,
	"workingset_activate_file": true,
	"workingset_restore_file": true,
	"pgscan":                  true,
	"pgsteal":                 true,
	"pgmajfault":              true,
}

func Read() Snapshot {
	if runtime.GOOS != "linux" {
		return Snapshot{}
	}

	s := Snapshot{
		Available: true,
		VMStat:    readKeyValueFile("/proc/vmstat", vmKeys),
		Memory:    map[string]int64{},
		PSI:       map[string]int64{},
	}

	if root, err := currentCgroupRoot(); err == nil {
		s.Memory = readKeyValueFile(filepath.Join(root, "memory.stat"), memoryKeys)
	}
	for _, resource := range []string{"memory", "io"} {
		if total, err := readPSITotal(filepath.Join("/proc/pressure", resource)); err == nil {
			s.PSI[resource] = total
		}
	}
	return s
}

func Between(before, after Snapshot) Delta {
	if !before.Available || !after.Available {
		return Delta{}
	}
	return Delta{
		Available: true,
		VMStat:    subtract(before.VMStat, after.VMStat),
		Memory:    subtract(before.Memory, after.Memory),
		PSI:       subtract(before.PSI, after.PSI),
	}
}

func subtract(before, after map[string]int64) map[string]int64 {
	out := make(map[string]int64)
	for key, end := range after {
		start := before[key]
		if end >= start {
			out[key] = end - start
		}
	}
	return out
}

func readKeyValueFile(path string, wanted map[string]bool) map[string]int64 {
	out := make(map[string]int64)
	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || !wanted[fields[0]] {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err == nil {
			out[fields[0]] = value
		}
	}
	return out
}

func currentCgroupRoot() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::") {
			path := strings.TrimPrefix(line, "0::")
			return filepath.Join("/sys/fs/cgroup", path), nil
		}
	}
	return "", fmt.Errorf("cgroup v2 path not found")
}

func readPSITotal(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var total int64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "total=") {
				value, err := strconv.ParseInt(strings.TrimPrefix(field, "total="), 10, 64)
				if err == nil {
					total += value
				}
			}
		}
	}
	return total, scanner.Err()
}
