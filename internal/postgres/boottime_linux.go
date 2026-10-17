package postgres

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// readBootTime is when the machine last booted.
func readBootTime() (*time.Time, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return nil, fmt.Errorf("failed to read the uptime: %w", err)
	}

	bootTime := time.Now().Add(-time.Duration(info.Uptime) * time.Second)

	return &bootTime, nil
}
