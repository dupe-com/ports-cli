package netscan

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// scanLsof shells out to lsof in field-output mode. -FpcLn emits one field
// per line, keyed by its first character:
//
//	p<pid>   process set begins
//	c<name>  command name
//	L<user>  login name
//	f<fd>    file set begins (always emitted; ignored)
//	n<addr>  socket name, e.g. "127.0.0.1:3000", "[::1]:8080", "*:5432"
//
// Field output is immune to the column-drift problems of parsing plain lsof
// (command names with spaces, wide users, etc.).
func scanLsof() ([]rawListener, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-FpcLn").Output()
	if err != nil {
		// lsof exits 1 when it has nothing to report; treat "no output"
		// as an empty result rather than an error.
		if ee, ok := err.(*exec.ExitError); ok && len(out) == 0 && len(ee.Stderr) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("lsof: %w", err)
	}
	return parseLsofOutput(string(out)), nil
}

// parseLsofOutput converts -FpcLn field output into raw listeners.
func parseLsofOutput(out string) []rawListener {
	var (
		raws      []rawListener
		pid       int32
		cmd, user string
	)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		val := line[1:]
		switch line[0] {
		case 'p':
			if n, err := strconv.ParseInt(val, 10, 32); err == nil {
				pid = int32(n)
			} else {
				pid = 0
			}
			cmd, user = "", ""
		case 'c':
			cmd = val
		case 'L':
			user = val
		case 'n':
			port, addr, ok := splitHostPort(val)
			if !ok || pid == 0 {
				continue
			}
			raws = append(raws, rawListener{port: port, addr: addr, pid: pid, cmd: cmd, user: user})
		}
	}
	return raws
}

// splitHostPort splits "addr:port" on the LAST colon, so IPv6 literals like
// "[::1]:3000" parse correctly.
func splitHostPort(name string) (port uint32, addr string, ok bool) {
	i := strings.LastIndexByte(name, ':')
	if i < 0 {
		return 0, "", false
	}
	n, err := strconv.ParseUint(name[i+1:], 10, 32)
	if err != nil || n == 0 || n > 65535 {
		return 0, "", false
	}
	return uint32(n), name[:i], true
}
