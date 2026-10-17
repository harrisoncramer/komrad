package postgres

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// readBootTime is when the machine last booted.
func readBootTime() (*time.Time, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return nil, fmt.Errorf("failed to read the boot time: %w", err)
	}

	bootTime := time.Unix(boot.Unix())

	return &bootTime, nil
}
