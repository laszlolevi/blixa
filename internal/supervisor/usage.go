package supervisor

import (
	"os/exec"
	"strconv"
	"strings"
)

// Usage: a process group's summed CPU (percent of one core) and resident memory.
type Usage struct {
	CPU   float64
	MemKB int64
}

// GroupUsage: the usage of every process group on the system, from one `ps` call.
func GroupUsage() (map[int]Usage, error) {
	out, err := exec.Command("ps", "-A", "-o", "pgid=,%cpu=,rss=").Output()
	if err != nil {
		return nil, err
	}
	return ParseGroupUsage(string(out)), nil
}

// ParseGroupUsage sums `ps -o pgid=,%cpu=,rss=` lines per process group.
func ParseGroupUsage(out string) map[int]Usage {
	usage := map[int]Usage{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		pgid, err1 := strconv.Atoi(f[0])
		cpu, err2 := strconv.ParseFloat(strings.ReplaceAll(f[1], ",", "."), 64) // a locale may use a decimal comma
		rss, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		u := usage[pgid]
		u.CPU += cpu
		u.MemKB += rss
		usage[pgid] = u
	}
	return usage
}
