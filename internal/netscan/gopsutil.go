package netscan

import (
	"fmt"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// scanGopsutil reads the OS connection table via gopsutil — the portable
// path used on Linux and Windows. (On macOS, gopsutil's connection table is
// flaky without root, so darwin uses scanLsof instead.)
func scanGopsutil() ([]rawListener, error) {
	conns, err := gnet.Connections("tcp")
	if err != nil {
		return nil, fmt.Errorf("reading connection table: %w", err)
	}
	var raws []rawListener
	for _, c := range conns {
		if c.Status != "LISTEN" || c.Laddr.Port == 0 {
			continue
		}
		addr := c.Laddr.IP
		if addr == "0.0.0.0" || addr == "::" {
			addr = "*"
		}
		raws = append(raws, rawListener{
			port: c.Laddr.Port,
			addr: addr,
			pid:  c.Pid,
		})
	}
	return raws, nil
}
