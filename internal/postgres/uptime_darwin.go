package postgres

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// uptime is how long the machine has run since it last booted.
func uptime() (time.Duration, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return 0, fmt.Errorf("failed to read the boot time: %w", err)
	}

	return time.Since(time.Unix(boot.Unix())), nil
}
