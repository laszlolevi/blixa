package supervisor

import (
	"errors"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// ParseListenPorts: the ports from `lsof -F n` output, sorted, without duplicates.
func ParseListenPorts(out string) []int {
	seen := map[int]bool{}
	var ports []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		i := strings.LastIndex(line, ":")
		if i < 0 {
			continue
		}
		p, err := strconv.Atoi(line[i+1:])
		if err != nil || seen[p] {
			continue
		}
		seen[p] = true
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

// Owner: a process listening on a port.
type Owner struct {
	PID     int
	Command string
}

// ParseOwner: the first process from `lsof -F pc` output.
func ParseOwner(out string) (Owner, bool) {
	var o Owner
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "p") && o.PID == 0:
			o.PID, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "c") && o.Command == "":
			o.Command = line[1:]
		}
	}
	return o, o.PID > 0
}

// GroupListenPorts: the LISTEN ports of every member of the process group.
func GroupListenPorts(pgid int) ([]int, error) {
	// -a: macOS/BSD pgrep by default excludes ancestors of the calling process -- since the
	// daemon may identify even itself by pgid (e.g. in a test the current process is
	// an ancestor of pgrep), without -a this match would be missed.
	out, err := exec.Command("pgrep", "-a", "-g", strconv.Itoa(pgid)).Output()
	if err != nil {
		if isExit1(err) {
			return nil, nil
		}
		return nil, err
	}
	pids := strings.Join(strings.Fields(string(out)), ",")
	if pids == "" {
		return nil, nil
	}
	out, err = exec.Command("lsof", "-nP", "-a", "-iTCP", "-sTCP:LISTEN", "-p", pids, "-Fn").Output()
	if err != nil && !isExit1(err) {
		return nil, err
	}
	return ParseListenPorts(string(out)), nil
}

// lookupPortOwner: who is listening on the given TCP port (according to lsof).
func lookupPortOwner(port int) (Owner, bool, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		if isExit1(err) {
			return Owner{}, false, nil
		}
		return Owner{}, false, err
	}
	o, ok := ParseOwner(string(out))
	return o, ok, nil
}

// isExit1: lsof and pgrep signal no match with exit code 1.
func isExit1(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 1
}

func syscallGetpgid(pid int) (int, error) { return syscall.Getpgid(pid) }
