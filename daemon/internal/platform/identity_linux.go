//go:build linux

package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessIdentity returns a persistent identity made from the Linux boot ID,
// PID, and /proc starttime tick. Together they reject stale or recycled PIDs.
func ProcessIdentity(pid int) (string, error) {
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read boot ID: %w", err)
	}
	boot := strings.TrimSpace(string(bootID))
	if boot == "" {
		return "", fmt.Errorf("read boot ID: empty value")
	}

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", fmt.Errorf("read process start time: %w", err)
	}
	startTime, err := linuxProcessStartTime(string(stat))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("linux:%s:%d:%s", boot, pid, startTime), nil
}

func linuxProcessStartTime(stat string) (string, error) {
	closingParenthesis := strings.LastIndex(stat, ")")
	if closingParenthesis < 0 {
		return "", fmt.Errorf("parse process start time: malformed stat")
	}
	fields := strings.Fields(stat[closingParenthesis+1:])
	// Field 3 is the first field after comm; starttime is field 22.
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return "", fmt.Errorf("parse process start time: missing starttime")
	}
	startTime := fields[startTimeIndex]
	if _, err := strconv.ParseUint(startTime, 10, 64); err != nil {
		return "", fmt.Errorf("parse process start time: %w", err)
	}
	return startTime, nil
}
