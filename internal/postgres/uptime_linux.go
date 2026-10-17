package postgres

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// uptime is how long the machine has run since it last booted.
func uptime() (time.Duration, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, fmt.Errorf("failed to read the uptime: %w", err)
	}

	return time.Duration(info.Uptime) * time.Second, nil
}
