//go:build linux && amd64

package protocol

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGateFDCensusAllowsStdioAndRejectsUnexpectedDescriptors(t *testing.T) {
	probe := buildGateFDCensusProbe(t)
	stdio, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stdio.Close()
	for _, scenario := range []string{"allowed-fds", "unexpected-fd", "occupied-image-fd", "numeric-names"} {
		t.Run(scenario, func(t *testing.T) {
			probeContext, cancelProbe := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancelProbe()
			command := exec.CommandContext(probeContext, probe, scenario)
			command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
			command.Stdin = stdio
			// FD 3 is closed for the proc reader; inherited gate FDs are 4–7.
			command.ExtraFiles = []*os.File{nil, stdio, stdio, stdio, stdio}
			if scenario == "unexpected-fd" {
				command.ExtraFiles = append(command.ExtraFiles, stdio)
			}
			if scenario == "occupied-image-fd" {
				command.ExtraFiles[0] = stdio
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("current gate census probe %s: %v\n%s", scenario, err, output)
			}
		})
	}
}

func buildGateFDCensusProbe(t *testing.T) string {
	t.Helper()
	compiler, err := exec.LookPath("cc")
	if err != nil {
		t.Fatalf("current-source C census regression requires cc: %v", err)
	}
	gateDirectory, err := filepath.Abs(filepath.Join("..", "c-gate"))
	if err != nil {
		t.Fatal(err)
	}
	probeDirectory := t.TempDir()
	probeSource := filepath.Join(probeDirectory, "gate-census-probe.c")
	probe := filepath.Join(probeDirectory, "gate-census-probe")
	// Include the current gate in this test-owned translation unit to exercise
	// its static helpers without adding a production mode or calling gate_run.
	const source = `#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
static char census_trace[4096];
static size_t census_trace_length;
static int census_trace_truncated;
static const char *census_phase;
static long census_last_fd = -1;
static int parse_proc_numeric_name(const char *name, long minimum, long *value_out);
static void census_record(const char *operation, int fd, const char *entry, int result, int error_number) {
  size_t remaining = sizeof(census_trace) - census_trace_length;
  int count = snprintf(census_trace + census_trace_length, remaining,
      "phase=%s operation=%s directoryfd=%d pid=%d entry=%.255s result=%d errno=%d\n",
      census_phase, operation, fd, getpid(), entry, result, error_number);
  if (count < 0 || (size_t)count >= remaining) {
    census_trace_truncated = 1;
    census_trace_length = sizeof(census_trace) - 1;
  } else {
    census_trace_length += (size_t)count;
  }
}
static DIR *census_opendir(const char *path) {
  DIR *directory = opendir(path);
  int saved_errno = errno;
  census_phase = strcmp(path, "/proc/self/task") == 0 ? "task" : "fd";
  census_record("opendir", directory == NULL ? -1 : dirfd(directory), "", directory == NULL ? -1 : 0, saved_errno);
  errno = saved_errno;
  return directory;
}
static struct dirent *census_readdir(DIR *directory) {
  struct dirent *entry = readdir(directory);
  int saved_errno = errno;
  census_record("readdir", dirfd(directory), entry == NULL ? "<end>" : entry->d_name,
      entry == NULL ? -1 : 0, saved_errno);
  long descriptor;
  if (entry != NULL && strcmp(census_phase, "fd") == 0 &&
      parse_proc_numeric_name(entry->d_name, 0, &descriptor) == 0) {
    census_last_fd = descriptor;
  }
  errno = saved_errno;
  return entry;
}
static int census_closedir(DIR *directory) {
  int fd = dirfd(directory);
  int result = closedir(directory);
  int saved_errno = errno;
  census_record("closedir", fd, "", result, saved_errno);
  errno = saved_errno;
  return result;
}
#define opendir census_opendir
#define readdir census_readdir
#define closedir census_closedir
#define main symmetry_gate_main
#include "gate.c"
#undef main

static int check_numeric_names(void) {
  const char *invalid[] = {
    "", "-1", "+1", " 1", "1 ", "1x", "2147483648",
    "999999999999999999999999999999999999999"
  };
  for (long minimum = 0; minimum <= 1; ++minimum) {
    const char *valid[] = {"1", "2147483647"};
    const long expected[] = {1, INT32_MAX};
    for (size_t index = 0; index < sizeof(valid) / sizeof(valid[0]); ++index) {
      long value = -1;
      if (parse_proc_numeric_name(valid[index], minimum, &value) != 0 || value != expected[index]) {
        fprintf(stderr, "numeric parser rejected %s with minimum %ld\n", valid[index], minimum);
        return 1;
      }
    }
    for (size_t index = 0; index < sizeof(invalid) / sizeof(invalid[0]); ++index) {
      long value = -1;
      errno = 0;
      if (parse_proc_numeric_name(invalid[index], minimum, &value) != -1 || errno != EPROTO || value != -1) {
        fprintf(stderr, "numeric parser accepted malformed/overflow name %s with minimum %ld\n", invalid[index], minimum);
        return 1;
      }
    }
  }
  long value = -1;
  if (parse_proc_numeric_name("0", 0, &value) != 0 || value != 0) {
    fprintf(stderr, "numeric parser rejected FD zero\n");
    return 1;
  }
  value = -1;
  errno = 0;
  if (parse_proc_numeric_name("0", 1, &value) != -1 || errno != EPROTO || value != -1) {
    fprintf(stderr, "numeric parser accepted task ID zero\n");
    return 1;
  }
  return 0;
}

int main(int argc, char **argv) {
  if (argc != 2) return 1;
  if (strcmp(argv[1], "numeric-names") == 0) return check_numeric_names();
  int extra_open = strcmp(argv[1], "unexpected-fd") == 0;
  int image_open = strcmp(argv[1], "occupied-image-fd") == 0;
  if (!extra_open && !image_open && strcmp(argv[1], "allowed-fds") != 0) return 1;
  for (int descriptor = 0; descriptor <= EXEC_STATUS_FD + 1; ++descriptor) {
    int expected_open = descriptor == IMAGE_FD ? image_open :
        (descriptor <= EXEC_STATUS_FD || extra_open);
    errno = 0;
    int flags = fcntl(descriptor, F_GETFD);
    if ((expected_open && flags < 0) || (!expected_open && (flags != -1 || errno != EBADF))) {
      fprintf(stderr, "probe inherited wrong shape at FD %d\n", descriptor);
      return 1;
    }
  }
  errno = 0;
  int result = reject_unexpected_inherited_fds();
  int error_number = errno;
  int expected_errno = image_open ? EPROTO : (extra_open ? EPERM : 0);
  if (result != (expected_errno == 0 ? 0 : -1) ||
      (expected_errno != 0 && error_number != expected_errno) ||
      (extra_open && census_last_fd != EXEC_STATUS_FD + 1)) {
    fprintf(stderr, "FD census result=%d errno=%d; expected result=%d errno=%d\n",
        result, error_number, expected_errno == 0 ? 0 : -1, expected_errno);
    if (extra_open) {
      fprintf(stderr, "FD census rejected entry %ld; intentional unexpected FD is %d\n",
          census_last_fd, EXEC_STATUS_FD + 1);
    }
    fprintf(stderr, "%s%s", census_trace, census_trace_truncated ? "<trace truncated>\n" : "");
    return 1;
  }
  return 0;
}
`
	if err := os.WriteFile(probeSource, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	compileContext, cancelCompile := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCompile()
	compile := exec.CommandContext(compileContext, compiler, "-std=c11", "-O2", "-I", gateDirectory, "-o", probe, probeSource)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile current gate census probe: %v\n%s", err, output)
	}
	return probe
}
