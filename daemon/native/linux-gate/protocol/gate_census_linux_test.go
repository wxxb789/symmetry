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

func compileGateProbe(t *testing.T, source, probeName string) string {
	t.Helper()
	compiler, err := exec.LookPath("cc")
	if err != nil {
		t.Fatalf("current-source C %s regression requires cc: %v", probeName, err)
	}
	gateDirectory, err := filepath.Abs(filepath.Join("..", "c-gate"))
	if err != nil {
		t.Fatal(err)
	}
	probeDirectory := t.TempDir()
	probeSource := filepath.Join(probeDirectory, "gate-"+probeName+"-probe.c")
	probe := filepath.Join(probeDirectory, "gate-"+probeName+"-probe")
	if err := os.WriteFile(probeSource, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	compileContext, cancelCompile := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelCompile()
	compile := exec.CommandContext(compileContext, compiler, "-std=c11", "-O2", "-I", gateDirectory, "-o", probe, probeSource)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile current gate %s probe: %v\n%s", probeName, err, output)
	}
	return probe
}

func buildGateFDCensusProbe(t *testing.T) string {
	t.Helper()
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
	return compileGateProbe(t, source, "census")
}

// The hosted runner's true is a symlink, which the gate's O_NOFOLLOW refuses
// with ELOOP; the exec-fence scenario must own a regular ELF image.
func censusExecImageFixture(t *testing.T) (string, string) {
	t.Helper()
	executable, err := exec.LookPath("true")
	if err != nil {
		t.Fatalf("exec-fence scenario requires true on PATH: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatalf("resolve the exec-fence image: %v", err)
	}
	contents, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("read the exec-fence image: %v", err)
	}
	directory := t.TempDir()
	image := filepath.Join(directory, "exec-fence-image")
	if err := os.WriteFile(image, contents, 0o755); err != nil {
		t.Fatalf("write the exec-fence image: %v", err)
	}
	if err := os.Chmod(image, 0o755); err != nil {
		t.Fatalf("set the exec-fence image mode: %v", err)
	}
	identity, err := os.Lstat(image)
	if err != nil {
		t.Fatalf("lstat the exec-fence image: %v", err)
	}
	if !identity.Mode().IsRegular() || identity.Mode().Perm()&0o111 == 0 {
		t.Fatalf("exec-fence image is not a regular executable: %v", identity.Mode())
	}
	symlinked := filepath.Join(directory, "exec-fence-symlink")
	if err := os.Symlink(image, symlinked); err != nil {
		t.Fatalf("symlink the exec-fence image: %v", err)
	}
	return image, symlinked
}

func TestGateReleaseFenceOrdering(t *testing.T) {
	probe := buildGateReleaseFenceProbe(t)
	image, symlinkedImage := censusExecImageFixture(t)
	for _, scenario := range []string{
		"exec-fence", "exec-symlinked-image", "exec-fence-expired", "exec-drop-denied", "clone-fence-expired", "clone-attempt-fence",
		"clone-close-failed", "clone-parent-death-failed",
		"clone-drop-denied", "clone-drop-groups", "clone-drop-bounding-set", "clone-drop-capabilities",
		"clone-drop-ambient", "clone-drop-no-new-privs", "clone-drop-status", "clone-drop-unclassified",
	} {
		t.Run(scenario, func(t *testing.T) {
			probeContext, cancelProbe := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancelProbe()
			imagePath := image
			if scenario == "exec-symlinked-image" {
				imagePath = symlinkedImage
			}
			command := exec.CommandContext(probeContext, probe, scenario, imagePath)
			command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("current gate release-fence probe %s: %v\n%s", scenario, err, output)
			}
		})
	}
}

func buildGateReleaseFenceProbe(t *testing.T) string {
	t.Helper()
	// Include the current gate in this test-owned translation unit and drive
	// exec_as_b/exec_clone_bootstrap directly, without a user namespace or a real exec.
	const source = `#define _GNU_SOURCE
#include <dirent.h>
#include <elf.h>
#include <errno.h>
#include <fcntl.h>
#include <grp.h>
#include <linux/capability.h>
#include <linux/memfd.h>
#include <poll.h>
#include <sched.h>
#include <setjmp.h>
#include <signal.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#if defined(__has_include)
#if __has_include(<sys/capability.h>)
#include <sys/capability.h>
#endif
#endif
#include <sys/mman.h>
#include <sys/prctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

/* Forks the gate's exec_as_b/exec_clone_bootstrap; interposes sendmsg/poll/prctl/syscall/open/close/getgroups,
 * the res-ID calls and _exit; answers execveat and the two proc reads; read/recvmsg, clock_gettime, fcntl, dup2 and the lease compare stay unobserved. */

enum {
  CENSUS_EVENT_CAPACITY = 512,
  CENSUS_OP_CAPACITY = 16,
  CENSUS_NAME_CAPACITY = 64,
  CENSUS_FRAME_EXEC_ATTEMPT = 7,
  CENSUS_FRAME_EXEC_FAILURE = 8,
  CENSUS_FRAME_BOOTSTRAP_ATTEMPT = 20,
  CENSUS_FRAME_BOOTSTRAP_FAILURE = 24,
};

struct census_event {
  char op[CENSUS_OP_CAPACITY];
  char name[CENSUS_NAME_CAPACITY];
  int fd;
  unsigned int version;
  unsigned int kind;
  unsigned int stage;
  unsigned int error_number;
  unsigned long long size;
  unsigned long long dev;
  unsigned long long ino;
  long long value;
};

struct census_trace {
  unsigned long long count;
  unsigned long long overflow;
  int owner_pipe_fd;
  int setup_step;
  struct census_event events[CENSUS_EVENT_CAPACITY];
};

static struct census_trace *census_trace;
static jmp_buf census_return;
static volatile int census_capture_armed;
static int census_deny_release_close;
static int census_deny_parent_death;

/* The credential fact each drop-denied scenario presents as unsatisfied; the
 * probe reports the gate's classification of it. */
enum census_drop_fact {
  CENSUS_DROP_FACT_NONE = 0,
  CENSUS_DROP_FACT_CREDENTIALS = 1,
  CENSUS_DROP_FACT_GROUPS = 2,
  CENSUS_DROP_FACT_BOUNDING_SET = 3,
  CENSUS_DROP_FACT_CAPABILITIES = 4,
  CENSUS_DROP_FACT_AMBIENT = 5,
  CENSUS_DROP_FACT_NO_NEW_PRIVS = 6,
  CENSUS_DROP_FACT_STATUS = 7,
  CENSUS_DROP_FACT_UNCLASSIFIED = 8,
};
static int census_drop_fact;
static const char *census_image_path;

static unsigned int census_load_u16(const unsigned char *value) {
  return (unsigned int)value[0] | ((unsigned int)value[1] << 8);
}

static unsigned int census_load_u32(const unsigned char *value) {
  return census_load_u16(value) | (census_load_u16(value + 2) << 16);
}

static unsigned long long census_load_u64(const unsigned char *value) {
  unsigned long long result = 0;
  for (int index = 7; index >= 0; --index) result = (result << 8) | value[index];
  return result;
}

static struct census_event *census_next(void) {
  struct census_trace *trace = census_trace;
  if (trace == NULL || trace->count >= CENSUS_EVENT_CAPACITY) {
    if (trace != NULL) trace->overflow += 1;
    return NULL;
  }
  struct census_event *event = &trace->events[trace->count++];
  memset(event, 0, sizeof(*event));
  event->fd = -1;
  return event;
}

static struct census_event *census_open_event(const char *operation) {
  struct census_event *event = census_next();
  if (event != NULL) snprintf(event->op, sizeof(event->op), "%s", operation);
  return event;
}

static void census_note(const char *operation) {
  (void)census_open_event(operation);
}

static ssize_t census_sendmsg(int fd, const struct msghdr *message, int flags) {
  (void)flags;
  unsigned long long size = 0;
  const unsigned char *payload = NULL;
  if (message != NULL && message->msg_iov != NULL && message->msg_iovlen > 0) {
    size = message->msg_iov[0].iov_len;
    payload = (const unsigned char *)message->msg_iov[0].iov_base;
  }
  struct census_event *event = census_open_event("sendmsg");
  if (event != NULL) {
    event->fd = fd;
    event->size = size;
    if (payload != NULL && size >= 12) {
      event->version = census_load_u16(payload + 8);
      event->kind = census_load_u16(payload + 10);
      if ((event->kind == CENSUS_FRAME_EXEC_ATTEMPT || event->kind == CENSUS_FRAME_BOOTSTRAP_ATTEMPT) && size >= 28) {
        event->dev = census_load_u64(payload + 12);
        event->ino = census_load_u64(payload + 20);
      } else if (event->kind == CENSUS_FRAME_EXEC_FAILURE && size >= 16) {
        event->error_number = census_load_u32(payload + 12);
      } else if (event->kind == CENSUS_FRAME_BOOTSTRAP_FAILURE && size >= 20) {
        event->stage = census_load_u32(payload + 12);
        event->error_number = census_load_u32(payload + 16);
      }
    }
  }
  return (ssize_t)size;
}

static int census_poll(struct pollfd *fds, nfds_t nfds, int timeout) {
  struct census_event *event = census_open_event("poll");
  if (event != NULL) {
    event->fd = (fds != NULL && nfds > 0) ? fds[0].fd : -1;
    event->size = (unsigned long long)nfds;
    event->value = timeout;
  }
  /* The interposition macro is defined after this point, so this is real poll. */
  return poll(fds, nfds, timeout);
}

static int census_prctl(int option, ...) {
  va_list arguments;
  va_start(arguments, option);
  /* Every gate call site passes the first argument in the integer class, so the
   * low 32 bits recorded here are exact for both int and long arguments. */
  int argument = va_arg(arguments, int);
  va_end(arguments);
  struct census_event *event = census_open_event("prctl");
  if (event != NULL) {
    event->kind = (unsigned int)option;
    event->value = argument;
  }
  if (option == PR_CAPBSET_READ) {
    if (census_drop_fact == CENSUS_DROP_FACT_BOUNDING_SET) return 1;
    if (census_drop_fact == CENSUS_DROP_FACT_UNCLASSIFIED) {
      errno = EPERM;
      return -1;
    }
    return 0;
  }
  if (option == PR_CAPBSET_DROP && census_drop_fact == CENSUS_DROP_FACT_BOUNDING_SET) {
    errno = EPERM;
    return -1;
  }
#ifdef PR_CAP_AMBIENT
  if (option == PR_CAP_AMBIENT && census_drop_fact == CENSUS_DROP_FACT_AMBIENT) {
    if (argument == PR_CAP_AMBIENT_IS_SET) return 1;
    if (argument == PR_CAP_AMBIENT_CLEAR_ALL) {
      errno = EPERM;
      return -1;
    }
  }
#endif
  if (option == PR_SET_NO_NEW_PRIVS && census_drop_fact == CENSUS_DROP_FACT_NO_NEW_PRIVS) {
    errno = EPERM;
    return -1;
  }
  if (option == PR_GET_NO_NEW_PRIVS) {
    return (census_drop_fact == CENSUS_DROP_FACT_NO_NEW_PRIVS) ? 0 : 1;
  }
  if (option == PR_SET_PDEATHSIG && census_deny_parent_death) {
    errno = EINVAL;
    return -1;
  }
  return 0;
}

static long census_syscall(long number, ...) {
  va_list arguments;
  va_start(arguments, number);
  unsigned long forwarded[6];
  for (int index = 0; index < 6; ++index) forwarded[index] = va_arg(arguments, unsigned long);
  va_end(arguments);
  if (number == SYS_execveat) {
    census_note("execveat");
    errno = ENOEXEC;
    return -1;
  }
  if (number == SYS_capget) {
    census_note("capget");
    struct __user_cap_data_struct *data = (struct __user_cap_data_struct *)forwarded[1];
    if (data != NULL) {
      memset(data, 0, 2 * sizeof(*data));
      if (census_drop_fact == CENSUS_DROP_FACT_CAPABILITIES) data[0].effective = 1;
    }
    return 0;
  }
  if (number == SYS_capset && census_drop_fact == CENSUS_DROP_FACT_CAPABILITIES) {
    census_note("capset");
    errno = EPERM;
    return -1;
  }
  /* Gate syscall sites pass at most five integer-class arguments, so draining
   * the six integer slots forwards every real call unmodified. */
  return syscall(number, forwarded[0], forwarded[1], forwarded[2],
      forwarded[3], forwarded[4], forwarded[5]);
}

static __attribute__((noreturn)) void census_exit(int status) {
  struct census_event *event = census_open_event("exit");
  if (event != NULL) event->value = status;
  if (census_capture_armed) {
    census_capture_armed = 0;
    longjmp(census_return, 1);
  }
  syscall(SYS_exit_group, status);
  __builtin_unreachable();
}

static int census_setresuid(uid_t real, uid_t effective, uid_t saved) {
  (void)real; (void)effective; (void)saved;
  census_note("setresuid");
  if (census_drop_fact == CENSUS_DROP_FACT_CREDENTIALS || census_drop_fact == CENSUS_DROP_FACT_UNCLASSIFIED) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int census_setresgid(gid_t real, gid_t effective, gid_t saved) {
  (void)real; (void)effective; (void)saved;
  census_note("setresgid");
  return 0;
}

static int census_getresuid(uid_t *real, uid_t *effective, uid_t *saved) {
  /* The ID step denies the credentials row; a bounding-set failure precedes it,
   * so that row reports the pre-drop IDs as well. */
  uid_t reported = (census_drop_fact == CENSUS_DROP_FACT_CREDENTIALS ||
      census_drop_fact == CENSUS_DROP_FACT_BOUNDING_SET) ? 0 : 1;
  if (real != NULL) *real = reported;
  if (effective != NULL) *effective = reported;
  if (saved != NULL) *saved = reported;
  census_note("getresuid");
  return 0;
}

static int census_getresgid(gid_t *real, gid_t *effective, gid_t *saved) {
  gid_t reported = (census_drop_fact == CENSUS_DROP_FACT_CREDENTIALS ||
      census_drop_fact == CENSUS_DROP_FACT_BOUNDING_SET) ? 0 : 1;
  if (real != NULL) *real = reported;
  if (effective != NULL) *effective = reported;
  if (saved != NULL) *saved = reported;
  census_note("getresgid");
  return 0;
}

static int census_getgroups(int size, gid_t list[]) {
  (void)size; (void)list;
  census_note("getgroups");
  if (census_drop_fact == CENSUS_DROP_FACT_GROUPS) {
    /* A successful getgroups leaves errno untouched; the zero-group precondition
     * fails on the count, so the frame carries whatever errno the gate held. */
    errno = 0;
    return 2;
  }
  return 0;
}

static const char census_status_content[] =
    "Uid:\t1\t1\t1\t1\n"
    "Gid:\t1\t1\t1\t1\n"
    "Groups:\n"
    "CapEff:\t0\n"
    "CapPrm:\t0\n"
    "CapInh:\t0\n"
    "CapBnd:\t0\n"
    "CapAmb:\t0\n"
    "NoNewPrivs:\t1\n";

static int census_synthetic_file(const char *content) {
  int fd = (int)syscall(SYS_memfd_create, "census-synthetic", 0);
  if (fd < 0) return -1;
  size_t length = strlen(content);
  size_t written = 0;
  while (written < length) {
    ssize_t count = write(fd, content + written, length - written);
    if (count < 0 && errno == EINTR) continue;
    if (count <= 0) {
      int saved_errno = count == 0 ? EIO : errno;
      close(fd);
      errno = saved_errno;
      return -1;
    }
    written += (size_t)count;
  }
  if (lseek(fd, 0, SEEK_SET) < 0) {
    int saved_errno = errno;
    close(fd);
    errno = saved_errno;
    return -1;
  }
  return fd;
}

static int census_open(const char *path, int flags, ...) {
  mode_t mode = 0;
  if ((flags & O_CREAT) != 0) {
    va_list arguments;
    va_start(arguments, flags);
    mode = va_arg(arguments, mode_t);
    va_end(arguments);
  }
  int status_read = path != NULL && strcmp(path, "/proc/self/status") == 0;
  int synthetic = status_read || (path != NULL && strcmp(path, "/proc/sys/kernel/cap_last_cap") == 0);
  const char *status_content = (census_drop_fact == CENSUS_DROP_FACT_STATUS) ? "" : census_status_content;
  int fd = synthetic ? census_synthetic_file(status_read ? status_content : "40\n")
                     : (int)syscall(SYS_openat, AT_FDCWD, path, flags, mode);
  struct census_event *event = census_open_event("open");
  if (event != NULL) {
    event->fd = fd;
    event->kind = (unsigned int)synthetic;
    snprintf(event->name, sizeof(event->name), "%s", path != NULL ? path : "<null>");
  }
  return fd;
}

#define CENSUS_RELEASE_FD 4

/* Only the release descriptor is deniable: the probe calls exec_as_b with a
 * prepared command, so the one-use RELEASE close is the only close of fd 4. */
static int census_close(int fd) {
  if (census_deny_release_close && fd == CENSUS_RELEASE_FD) {
    errno = EBADF;
    return -1;
  }
  return close(fd);
}

/* Gate-private calls see the probe's recorded boundary; everything else in the
 * translation unit (including the probe driver) keeps the real libc symbols. */
#define sendmsg census_sendmsg
#define poll census_poll
#define prctl census_prctl
#define open census_open
#define close census_close
#define setresuid census_setresuid
#define setresgid census_setresgid
#define getresuid census_getresuid
#define getresgid census_getresgid
#define getgroups census_getgroups
#define syscall census_syscall
#define _exit census_exit
#define main symmetry_gate_main
#include "gate.c"
#undef main
#undef _exit
#undef syscall
#undef getgroups
#undef getresgid
#undef getresuid
#undef setresgid
#undef setresuid
#undef open
#undef close
#undef prctl
#undef poll
#undef sendmsg

_Static_assert(CENSUS_FRAME_EXEC_ATTEMPT == FRAME_EXEC_ATTEMPT, "probe must track the gate exec attempt frame");
_Static_assert(CENSUS_FRAME_EXEC_FAILURE == FRAME_EXEC_FAILURE, "probe must track the gate exec failure frame");
_Static_assert(CENSUS_FRAME_BOOTSTRAP_ATTEMPT == FRAME_BOOTSTRAP_EXEC_ATTEMPT, "probe must track the gate bootstrap attempt frame");
_Static_assert(CENSUS_FRAME_BOOTSTRAP_FAILURE == FRAME_CLONE_BOOTSTRAP_FAILURE, "probe must track the gate bootstrap failure frame");
_Static_assert(CLONE_FAILURE_GATE_CLOSE_STAGE == 8 && CLONE_FAILURE_GATE_DROP_STAGE == 9 &&
    CLONE_FAILURE_GATE_PARENT_DEATH_STAGE == 10 && CLONE_FAILURE_GATE_FENCE_STAGE == 11,
    "gate pre-clone guard stages must keep their closed wire numbers");
_Static_assert(CLONE_FAILURE_GATE_DROP_GROUPS_STAGE == 12 && CLONE_FAILURE_GATE_DROP_BOUNDING_SET_STAGE == 13 &&
    CLONE_FAILURE_GATE_DROP_CREDENTIALS_STAGE == 14 && CLONE_FAILURE_GATE_DROP_CAPABILITIES_STAGE == 15 &&
    CLONE_FAILURE_GATE_DROP_AMBIENT_STAGE == 16 && CLONE_FAILURE_GATE_DROP_NO_NEW_PRIVS_STAGE == 17 &&
    CLONE_FAILURE_GATE_DROP_STATUS_STAGE == 18,
    "gate credential-fact stages must keep their closed wire numbers");
_Static_assert(CENSUS_RELEASE_FD == RELEASE_FD, "probe must track the gate release descriptor");

static const unsigned long long CENSUS_PINNED_DEV = 0x5eed01;
static const unsigned long long CENSUS_PINNED_INO = 0x5eed02030405;

struct census_child_setup {
  int clone_entry;
  int clone_mode;
  int expired_lease;
  int drop_fact;
  int deny_release_close;
  int deny_parent_death;
  int pinned_bootstrap_stat;
  unsigned short release_version;
};

struct census_run_result {
  struct census_trace *trace;
  int child_status;
};

static void census_child_abort(int step) {
  if (census_trace != NULL) census_trace->setup_step = step;
  syscall(SYS_exit_group, 2);
  __builtin_unreachable();
}

static void census_child_run(const struct census_child_setup *setup) {
  census_drop_fact = setup->drop_fact;
  census_deny_release_close = setup->deny_release_close;
  census_deny_parent_death = setup->deny_parent_death;
  pid_t owner_pid = getppid();
  int scratch = open("/dev/null", O_RDONLY);
  if (scratch < 0) census_child_abort(1);
  for (int descriptor = IMAGE_FD; descriptor <= EXEC_STATUS_FD; ++descriptor) {
    if (dup2(scratch, descriptor) < 0) census_child_abort(2);
  }
  if (scratch > EXEC_STATUS_FD) close(scratch);
  int owner_pidfd_pipe[2], owner_command_pipe[2], status_socket[2];
  int stdin_pipe[2], stdout_pipe[2], stderr_pipe[2];
  if (pipe(owner_pidfd_pipe) != 0) census_child_abort(3);
  if (pipe(owner_command_pipe) != 0) census_child_abort(4);
  if (socketpair(AF_UNIX, SOCK_SEQPACKET, 0, status_socket) != 0) census_child_abort(5);
  if (pipe(stdin_pipe) != 0) census_child_abort(6);
  if (pipe(stdout_pipe) != 0) census_child_abort(7);
  if (pipe(stderr_pipe) != 0) census_child_abort(8);
  if (dup2(owner_pidfd_pipe[0], OWNER_PIDFD) < 0) census_child_abort(9);
  if (dup2(status_socket[0], EXEC_STATUS_FD) < 0) census_child_abort(10);
  close(owner_pidfd_pipe[0]);
  close(status_socket[0]);
  static const unsigned char argv_bytes[] = "true";
  static const unsigned char cwd_bytes[] = "/";
  static const unsigned char clone_grant_bytes[] = "{}";
  struct release_command command;
  memset(&command, 0, sizeof(command));
  command.path.data = (const unsigned char *)census_image_path;
  command.path.length = (uint32_t)strlen(census_image_path);
  command.argv[0].data = argv_bytes;
  command.argv[0].length = sizeof(argv_bytes) - 1;
  command.argc = 1;
  command.cwd.data = cwd_bytes;
  command.cwd.length = sizeof(cwd_bytes) - 1;
  command.stdio_fds[0] = stdin_pipe[0];
  command.stdio_fds[1] = stdout_pipe[1];
  command.stdio_fds[2] = stderr_pipe[1];
  command.owner_pipe_fd = owner_command_pipe[0];
  command.release_version = setup->release_version;
  command.clone_mode = setup->clone_mode;
  if (setup->clone_mode) {
    command.clone_grant = clone_grant_bytes;
    command.clone_grant_size = sizeof(clone_grant_bytes) - 1;
  }
  struct timespec now;
  if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) census_child_abort(11);
  unsigned long long now_ns = (unsigned long long)now.tv_sec * 1000000000ull + (unsigned long long)now.tv_nsec;
  command.lease_deadline_ns = setup->expired_lease
      ? (now_ns > 1000000000ull ? now_ns - 1000000000ull : 1)
      : now_ns + 60ull * 1000000000ull;
  struct owner_config config = {.a_uid = 1, .a_gid = 1, .b_uid = 2, .b_gid = 2};
  census_trace->owner_pipe_fd = command.owner_pipe_fd;
  unsigned char packet[128];
  memset(packet, 0, sizeof(packet));
  if (setup->pinned_bootstrap_stat) {
    clone_bootstrap_image_fd = dup(IMAGE_FD);
    clone_git_image_fd = dup(IMAGE_FD);
    if (clone_bootstrap_image_fd < 0 || clone_git_image_fd < 0) census_child_abort(12);
    clone_bootstrap_image_stat.st_dev = (dev_t)CENSUS_PINNED_DEV;
    clone_bootstrap_image_stat.st_ino = (ino_t)CENSUS_PINNED_INO;
  }
  if (setjmp(census_return) == 0) {
    census_capture_armed = 1;
    if (setup->clone_entry) {
      exec_clone_bootstrap(&config, packet, sizeof(packet), &command, owner_pid);
    } else {
      exec_as_b(&config, packet, sizeof(packet), &command, owner_pid);
    }
    census_capture_armed = 0;
    census_note("gate-returned");
  }
  census_capture_armed = 0;
  syscall(SYS_exit_group, 0);
  __builtin_unreachable();
}

static void census_print_trace(const struct census_trace *trace, int child_status) {
  int exited = WIFEXITED(child_status);
  printf("trace: child_status=%d child_exit=%d child_signal=%d events=%llu overflow=%llu owner_pipe_fd=%d setup_step=%d\n",
      child_status, exited ? WEXITSTATUS(child_status) : -1, exited ? 0 : WTERMSIG(child_status),
      trace->count, trace->overflow, trace->owner_pipe_fd, trace->setup_step);
  for (unsigned long long index = 0; index < trace->count; ++index) {
    const struct census_event *event = &trace->events[index];
    if (strcmp(event->op, "sendmsg") == 0) {
      printf("  [%llu] sendmsg fd=%d size=%llu version=%u kind=%u", index, event->fd, event->size, event->version, event->kind);
      if (event->kind == CENSUS_FRAME_EXEC_ATTEMPT || event->kind == CENSUS_FRAME_BOOTSTRAP_ATTEMPT) {
        printf(" dev=%llu ino=%llu", event->dev, event->ino);
      } else if (event->kind == CENSUS_FRAME_EXEC_FAILURE) {
        printf(" errno=%u", event->error_number);
      } else if (event->kind == CENSUS_FRAME_BOOTSTRAP_FAILURE) {
        printf(" stage=%u errno=%u", event->stage, event->error_number);
      }
      printf("\n");
    } else if (strcmp(event->op, "poll") == 0) {
      printf("  [%llu] poll fd=%d nfds=%llu timeout=%lld\n", index, event->fd, event->size, event->value);
    } else if (strcmp(event->op, "prctl") == 0) {
      printf("  [%llu] prctl option=%u argument=%lld\n", index, event->kind, event->value);
    } else if (strcmp(event->op, "open") == 0) {
      printf("  [%llu] open %s fd=%d name=%s\n", index, event->kind == 1 ? "synthetic" : "forwarded", event->fd, event->name);
    } else if (strcmp(event->op, "exit") == 0) {
      printf("  [%llu] exit status=%lld\n", index, event->value);
    } else {
      printf("  [%llu] %s fd=%d value=%lld\n", index, event->op, event->fd, event->value);
    }
  }
  fflush(stdout);
}

static int census_execute_child(const struct census_child_setup *setup, struct census_run_result *result) {
  struct census_trace *trace = mmap(NULL, sizeof(*trace), PROT_READ | PROT_WRITE, MAP_SHARED | MAP_ANONYMOUS, -1, 0);
  if (trace == MAP_FAILED) {
    fprintf(stderr, "reason=mmap shared trace failed: %s\n", strerror(errno));
    return -1;
  }
  memset(trace, 0, sizeof(*trace));
  census_trace = trace;
  pid_t child = fork();
  if (child < 0) {
    fprintf(stderr, "reason=fork failed: %s\n", strerror(errno));
    return -1;
  }
  if (child == 0) {
    census_child_run(setup);
    syscall(SYS_exit_group, 3);
    __builtin_unreachable();
  }
  int status = 0;
  if (waitpid(child, &status, 0) != child) {
    fprintf(stderr, "reason=waitpid failed: %s\n", strerror(errno));
    return -1;
  }
  result->trace = trace;
  result->child_status = status;
  census_print_trace(trace, status);
  return 0;
}

static int census_reason_fail(const char *format, ...) {
  va_list arguments;
  va_start(arguments, format);
  printf("reason=");
  vprintf(format, arguments);
  printf("\n");
  va_end(arguments);
  fflush(stdout);
  return 1;
}

#define CENSUS_REQUIRE(condition, ...) do { \
  if (!(condition)) return census_reason_fail(__VA_ARGS__); \
} while (0)

static int census_require_child_ok(const struct census_run_result *run) {
  if (WIFEXITED(run->child_status) && WEXITSTATUS(run->child_status) == 0) return 0;
  return census_reason_fail("child did not exit 0 (status=%d exit=%d signal=%d setup_step=%d)",
      run->child_status,
      WIFEXITED(run->child_status) ? WEXITSTATUS(run->child_status) : -1,
      WIFEXITED(run->child_status) ? 0 : WTERMSIG(run->child_status),
      run->trace->setup_step);
}

static long census_first_sendmsg(const struct census_trace *trace, int fd) {
  for (unsigned long long index = 0; index < trace->count; ++index) {
    const struct census_event *event = &trace->events[index];
    if (strcmp(event->op, "sendmsg") == 0 && event->fd == fd) return (long)index;
  }
  return -1;
}

static long census_next_sendmsg(const struct census_trace *trace, long after, int fd, unsigned int kind) {
  for (unsigned long long index = (unsigned long long)(after + 1); index < trace->count; ++index) {
    const struct census_event *event = &trace->events[index];
    if (strcmp(event->op, "sendmsg") == 0 && event->fd == fd && event->kind == kind) return (long)index;
  }
  return -1;
}

static long census_count_frames(const struct census_trace *trace, int fd, long kind) {
  long count = 0;
  for (unsigned long long index = 0; index < trace->count; ++index) {
    const struct census_event *event = &trace->events[index];
    if (strcmp(event->op, "sendmsg") == 0 && event->fd == fd && (kind < 0 || (long)event->kind == kind)) ++count;
  }
  return count;
}

static long census_find_op(const struct census_trace *trace, const char *operation) {
  for (unsigned long long index = 0; index < trace->count; ++index) {
    if (strcmp(trace->events[index].op, operation) == 0) return (long)index;
  }
  return -1;
}

static long census_first_prctl(const struct census_trace *trace, unsigned int option) {
  for (unsigned long long index = 0; index < trace->count; ++index) {
    const struct census_event *event = &trace->events[index];
    if (strcmp(event->op, "prctl") == 0 && event->kind == option) return (long)index;
  }
  return -1;
}

static long census_next_op(const struct census_trace *trace, long after, const char *operation) {
  for (unsigned long long index = (unsigned long long)(after + 1); index < trace->count; ++index) {
    if (strcmp(trace->events[index].op, operation) == 0) return (long)index;
  }
  return -1;
}

static long census_next_prctl(const struct census_trace *trace, long after, unsigned int option) {
  for (unsigned long long index = (unsigned long long)(after + 1); index < trace->count; ++index) {
    const struct census_event *event = &trace->events[index];
    if (strcmp(event->op, "prctl") == 0 && event->kind == option) return (long)index;
  }
  return -1;
}

/* Pins the drop's own credential steps in order up to the capabilities write,
 * so deleting a step from drop_to_b cannot stay green. */
static int census_require_drop_step_order(const struct census_run_result *run) {
  long step = census_find_op(run->trace, "getgroups");
  CENSUS_REQUIRE(step >= 0, "the drop's zero-groups check was not observed");
  step = census_next_prctl(run->trace, step, PR_CAPBSET_DROP);
  CENSUS_REQUIRE(step >= 0, "the drop's bounding-set write was not observed");
  step = census_next_prctl(run->trace, step, PR_SET_KEEPCAPS);
  CENSUS_REQUIRE(step >= 0 && run->trace->events[step].value == 0,
      "PR_SET_KEEPCAPS,0 was not observed after the bounding-set write");
  step = census_next_op(run->trace, step, "setresgid");
  CENSUS_REQUIRE(step >= 0, "setresgid was not observed after PR_SET_KEEPCAPS");
  step = census_next_op(run->trace, step, "setresuid");
  CENSUS_REQUIRE(step >= 0, "setresuid was not observed after setresgid");
  step = census_next_prctl(run->trace, step, PR_SET_DUMPABLE);
  CENSUS_REQUIRE(step >= 0 && run->trace->events[step].value == 0,
      "PR_SET_DUMPABLE,0 was not observed after setresuid");
  step = census_next_op(run->trace, step, "capset");
  CENSUS_REQUIRE(step >= 0, "the capabilities write was not observed after PR_SET_DUMPABLE");
  return 0;
}

static int census_is_guard_poll(const struct census_trace *trace, unsigned long long index) {
  const struct census_event *event = &trace->events[index];
  return strcmp(event->op, "poll") == 0 &&
      (event->fd == OWNER_PIDFD || event->fd == trace->owner_pipe_fd);
}

static long census_last_guard_poll_before(const struct census_trace *trace, long limit) {
  long found = -1;
  for (unsigned long long index = 0; index < (unsigned long long)limit && index < trace->count; ++index) {
    if (census_is_guard_poll(trace, index)) found = (long)index;
  }
  return found;
}

static long census_count_guard_polls_between(const struct census_trace *trace, long start, long end) {
  long count = 0;
  for (unsigned long long index = (unsigned long long)(start + 1); index < (unsigned long long)end && index < trace->count; ++index) {
    if (census_is_guard_poll(trace, index)) ++count;
  }
  return count;
}

static int census_scenario_exec_fence(void) {
  struct stat identity;
  if (stat(census_image_path, &identity) != 0) return census_reason_fail("stat %s failed: %s", census_image_path, strerror(errno));
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.release_version = RELEASE_WIRE_VERSION;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  long attempt = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  CENSUS_REQUIRE(attempt >= 0, "no frame was sent on fd %d", EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[attempt];
  CENSUS_REQUIRE(frame->version == WIRE_VERSION && frame->kind == FRAME_EXEC_ATTEMPT &&
      frame->size == EXEC_ATTEMPT_SIZE,
      "first fd-%d frame is version=%u kind=%u size=%llu; want version=%d kind=%d size=%d",
      EXEC_STATUS_FD, frame->version, frame->kind, frame->size,
      WIRE_VERSION, FRAME_EXEC_ATTEMPT, EXEC_ATTEMPT_SIZE);
  CENSUS_REQUIRE(frame->dev == (unsigned long long)identity.st_dev && frame->ino == (unsigned long long)identity.st_ino,
      "attempt frame dev/ino %llu/%llu does not match the exec-fence image %llu/%llu",
      frame->dev, frame->ino, (unsigned long long)identity.st_dev, (unsigned long long)identity.st_ino);
  long execveat = census_find_op(run.trace, "execveat");
  CENSUS_REQUIRE(execveat > attempt, "execveat index %ld does not follow the attempt frame index %ld", execveat, attempt);
  long pdeathsig = census_first_prctl(run.trace, PR_SET_PDEATHSIG);
  CENSUS_REQUIRE(pdeathsig >= 0 && pdeathsig < attempt,
      "PR_SET_PDEATHSIG index %ld is missing or after the attempt frame index %ld", pdeathsig, attempt);
  CENSUS_REQUIRE(run.trace->events[pdeathsig].value == SIGKILL,
      "PR_SET_PDEATHSIG argument %lld is not SIGKILL(%d)", run.trace->events[pdeathsig].value, SIGKILL);
  CENSUS_REQUIRE(census_last_guard_poll_before(run.trace, attempt) >= 0,
      "no guard poll on the owner fds precedes the attempt frame index %ld", attempt);
  long post_send_guards = census_count_guard_polls_between(run.trace, attempt, execveat);
  CENSUS_REQUIRE(post_send_guards >= 2,
      "post-send guard polls strictly between the attempt frame index %ld and execveat index %ld: %ld; want the check_exec_guard owner-pidfd and owner-pipe polls",
      attempt, execveat, post_send_guards);
  long failure = census_next_sendmsg(run.trace, execveat, EXEC_STATUS_FD, FRAME_EXEC_FAILURE);
  CENSUS_REQUIRE(failure >= 0, "no kind-%d failure frame follows the intercepted execveat", FRAME_EXEC_FAILURE);
  CENSUS_REQUIRE(run.trace->events[failure].error_number == ENOEXEC,
      "the failure frame after the intercepted execveat carries errno=%u; want ENOEXEC(%d)",
      run.trace->events[failure].error_number, ENOEXEC);
  return 0;
}

static int census_scenario_exec_symlinked_image(void) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.release_version = RELEASE_WIRE_VERSION;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_EXEC_ATTEMPT) == 0,
      "symlinked image path still sent a kind-%d attempt frame", FRAME_EXEC_ATTEMPT);
  CENSUS_REQUIRE(census_find_op(run.trace, "execveat") < 0, "symlinked image path still reached execveat");
  long frames = census_count_frames(run.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(frames == 1, "fd-%d frames on a symlinked image path: %ld; want exactly one failure frame",
      EXEC_STATUS_FD, frames);
  long failure = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[failure];
  CENSUS_REQUIRE(frame->version == WIRE_VERSION && frame->kind == FRAME_EXEC_FAILURE &&
      frame->size == EXEC_FAILURE_SIZE,
      "symlinked-image frame is version=%u kind=%u size=%llu; want version=%d kind=%d size=%d",
      frame->version, frame->kind, frame->size, WIRE_VERSION, FRAME_EXEC_FAILURE, EXEC_FAILURE_SIZE);
  CENSUS_REQUIRE(frame->error_number == ELOOP,
      "symlinked-image frame errno=%u; want ELOOP(%d) from open_native_image's O_NOFOLLOW",
      frame->error_number, ELOOP);
  return 0;
}

static int census_scenario_exec_fence_expired(void) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.release_version = RELEASE_WIRE_VERSION;
  setup.expired_lease = 1;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_EXEC_ATTEMPT) == 0,
      "expired lease still sent a kind-%d attempt frame", FRAME_EXEC_ATTEMPT);
  CENSUS_REQUIRE(census_find_op(run.trace, "execveat") < 0, "expired lease still reached execveat");
  long frames = census_count_frames(run.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(frames == 1, "fd-%d frames on an expired lease: %ld; want exactly one failure frame",
      EXEC_STATUS_FD, frames);
  long failure = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[failure];
  CENSUS_REQUIRE(frame->version == WIRE_VERSION && frame->kind == FRAME_EXEC_FAILURE &&
      frame->size == EXEC_FAILURE_SIZE,
      "expired-lease frame is version=%u kind=%u size=%llu; want version=%d kind=%d size=%d",
      frame->version, frame->kind, frame->size, WIRE_VERSION, FRAME_EXEC_FAILURE, EXEC_FAILURE_SIZE);
  CENSUS_REQUIRE(frame->error_number == ETIMEDOUT,
      "expired-lease frame errno=%u; want ETIMEDOUT(%d)", frame->error_number, ETIMEDOUT);
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_CLONE_BOOTSTRAP_FAILURE) == 0,
      "v2 release still sent a v3 kind-%d bootstrap failure frame", FRAME_CLONE_BOOTSTRAP_FAILURE);
  CENSUS_REQUIRE(census_first_prctl(run.trace, PR_CAPBSET_READ) < 0,
      "the credential-fact classifier ran on a non-clone release");
  return 0;
}

static int census_scenario_clone_fence_expired(void) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.clone_mode = 1;
  setup.release_version = RELEASE_CLONE_WIRE_VERSION;
  setup.expired_lease = 1;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_EXEC_ATTEMPT) == 0,
      "expired clone release still sent a kind-%d attempt frame", FRAME_EXEC_ATTEMPT);
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_BOOTSTRAP_EXEC_ATTEMPT) == 0,
      "expired clone release still sent a kind-%d attempt frame", FRAME_BOOTSTRAP_EXEC_ATTEMPT);
  CENSUS_REQUIRE(census_find_op(run.trace, "execveat") < 0, "expired clone release still reached execveat");
  long frames = census_count_frames(run.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(frames == 1, "expired clone release sent %ld frames on fd %d; want exactly one",
      frames, EXEC_STATUS_FD);
  long failure = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[failure];
  CENSUS_REQUIRE(frame->version == CLONE_STATUS_WIRE_VERSION &&
      frame->kind == FRAME_CLONE_BOOTSTRAP_FAILURE && frame->size == CLONE_FAILURE_SIZE,
      "expired clone release sent version=%u kind=%u size=%llu; want version=%d kind=%d size=%d",
      frame->version, frame->kind, frame->size,
      CLONE_STATUS_WIRE_VERSION, FRAME_CLONE_BOOTSTRAP_FAILURE, CLONE_FAILURE_SIZE);
  CENSUS_REQUIRE(frame->stage == CLONE_FAILURE_GATE_FENCE_STAGE,
      "expired clone release stage=%u; want the fence guard stage %d",
      frame->stage, CLONE_FAILURE_GATE_FENCE_STAGE);
  CENSUS_REQUIRE(frame->error_number == ETIMEDOUT,
      "expired clone release errno=%u; want ETIMEDOUT(%d)", frame->error_number, ETIMEDOUT);
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_EXEC_FAILURE) == 0,
      "expired clone release still sent a v1 kind-%d exec failure frame", FRAME_EXEC_FAILURE);
  CENSUS_REQUIRE(census_first_prctl(run.trace, PR_CAPBSET_READ) < 0,
      "the credential-fact classifier ran although the credential drop succeeded");
  return 0;
}

static int census_require_drop_classification(int drop_fact, uint32_t want_stage, uint32_t want_errno,
    int require_drop_order) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.clone_mode = 1;
  setup.release_version = RELEASE_CLONE_WIRE_VERSION;
  setup.expired_lease = 1;
  setup.drop_fact = drop_fact;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  if (require_drop_order && census_require_drop_step_order(&run) != 0) return 1;
  CENSUS_REQUIRE(census_find_op(run.trace, "execveat") < 0, "denied credential drop still reached execveat");
  long frames = census_count_frames(run.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(frames == 1, "credential fact %d sent %ld frames on fd %d; want exactly one",
      drop_fact, frames, EXEC_STATUS_FD);
  long failure = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[failure];
  CENSUS_REQUIRE(frame->version == CLONE_STATUS_WIRE_VERSION &&
      frame->kind == FRAME_CLONE_BOOTSTRAP_FAILURE && frame->size == CLONE_FAILURE_SIZE &&
      frame->stage == want_stage && frame->error_number == want_errno,
      "credential fact %d sent version=%u kind=%u size=%llu stage=%u errno=%u;"
      " want version=%d kind=%d size=%d stage=%d errno=%u",
      drop_fact, frame->version, frame->kind, frame->size, frame->stage, frame->error_number,
      CLONE_STATUS_WIRE_VERSION, FRAME_CLONE_BOOTSTRAP_FAILURE, CLONE_FAILURE_SIZE,
      want_stage, want_errno);
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_EXEC_FAILURE) == 0,
      "credential fact %d still sent a v1 kind-%d exec failure frame", drop_fact, FRAME_EXEC_FAILURE);
  return 0;
}

static int census_scenario_clone_drop_denied(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_CREDENTIALS,
      CLONE_FAILURE_GATE_DROP_CREDENTIALS_STAGE, EPERM, 0);
}

static int census_scenario_clone_drop_groups(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_GROUPS,
      CLONE_FAILURE_GATE_DROP_GROUPS_STAGE, EPERM, 0);
}

/* The bounding-set row denies the drop's own step, leaving the IDs untouched:
 * two facts are unsatisfied and the frame must name the first in drop order. */
static int census_scenario_clone_drop_bounding_set(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_BOUNDING_SET,
      CLONE_FAILURE_GATE_DROP_BOUNDING_SET_STAGE, EPERM, 0);
}

static int census_scenario_clone_drop_capabilities(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_CAPABILITIES,
      CLONE_FAILURE_GATE_DROP_CAPABILITIES_STAGE, EPERM, 1);
}

static int census_scenario_clone_drop_ambient(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_AMBIENT,
      CLONE_FAILURE_GATE_DROP_AMBIENT_STAGE, EPERM, 0);
}

static int census_scenario_clone_drop_no_new_privs(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_NO_NEW_PRIVS,
      CLONE_FAILURE_GATE_DROP_NO_NEW_PRIVS_STAGE, EPERM, 0);
}

static int census_scenario_clone_drop_status(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_STATUS,
      CLONE_FAILURE_GATE_DROP_STATUS_STAGE, EPERM, 0);
}

/* The unclassifiable row denies the ID step and makes the classifier's
 * bounding-set read fail: the frame keeps the plain guard stage and errno. */
static int census_scenario_clone_drop_unclassified(void) {
  return census_require_drop_classification(CENSUS_DROP_FACT_UNCLASSIFIED,
      CLONE_FAILURE_GATE_DROP_STAGE, EPERM, 0);
}

/* Each remaining pre-clone guard has its own row so no call site can swap two
 * guard stages unnoticed. */
static int census_require_preclone_guard(int deny_release_close, int deny_parent_death, uint32_t want_stage,
    unsigned int want_errno) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.clone_mode = 1;
  setup.release_version = RELEASE_CLONE_WIRE_VERSION;
  setup.deny_release_close = deny_release_close;
  setup.deny_parent_death = deny_parent_death;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  CENSUS_REQUIRE(census_find_op(run.trace, "execveat") < 0, "denied pre-clone guard still reached execveat");
  long frames = census_count_frames(run.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(frames == 1, "denied pre-clone guard sent %ld frames on fd %d; want exactly one",
      frames, EXEC_STATUS_FD);
  long failure = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[failure];
  CENSUS_REQUIRE(frame->version == CLONE_STATUS_WIRE_VERSION &&
      frame->kind == FRAME_CLONE_BOOTSTRAP_FAILURE && frame->size == CLONE_FAILURE_SIZE &&
      frame->stage == want_stage && frame->error_number == want_errno,
      "denied pre-clone guard sent version=%u kind=%u size=%llu stage=%u errno=%u;"
      " want version=%d kind=%d size=%d stage=%d errno=%u",
      frame->version, frame->kind, frame->size, frame->stage, frame->error_number,
      CLONE_STATUS_WIRE_VERSION, FRAME_CLONE_BOOTSTRAP_FAILURE, CLONE_FAILURE_SIZE, want_stage, want_errno);
  CENSUS_REQUIRE(census_first_prctl(run.trace, PR_CAPBSET_READ) < 0,
      "the credential-fact classifier ran although the guard failed before the drop");
  return 0;
}

static int census_scenario_clone_close_failed(void) {
  return census_require_preclone_guard(1, 0, CLONE_FAILURE_GATE_CLOSE_STAGE, EBADF);
}

static int census_scenario_clone_parent_death_failed(void) {
  return census_require_preclone_guard(0, 1, CLONE_FAILURE_GATE_PARENT_DEATH_STAGE, EINVAL);
}

/* A v1 release whose credential drop fails keeps the v1 frame, the failing
 * call's errno, and no classifier run. */
static int census_scenario_exec_drop_denied(void) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.release_version = RELEASE_WIRE_VERSION;
  setup.drop_fact = CENSUS_DROP_FACT_CREDENTIALS;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  CENSUS_REQUIRE(census_find_op(run.trace, "execveat") < 0, "denied credential drop still reached execveat");
  long frames = census_count_frames(run.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(frames == 1, "v1 denied drop sent %ld frames on fd %d; want exactly one",
      frames, EXEC_STATUS_FD);
  long failure = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[failure];
  CENSUS_REQUIRE(frame->version == WIRE_VERSION && frame->kind == FRAME_EXEC_FAILURE &&
      frame->size == EXEC_FAILURE_SIZE && frame->error_number == EPERM,
      "v1 denied drop sent version=%u kind=%u size=%llu errno=%u; want version=%d kind=%d size=%d errno=%d",
      frame->version, frame->kind, frame->size, frame->error_number,
      WIRE_VERSION, FRAME_EXEC_FAILURE, EXEC_FAILURE_SIZE, EPERM);
  CENSUS_REQUIRE(census_count_frames(run.trace, EXEC_STATUS_FD, FRAME_CLONE_BOOTSTRAP_FAILURE) == 0,
      "v1 denied drop still sent a v3 kind-%d bootstrap failure frame", FRAME_CLONE_BOOTSTRAP_FAILURE);
  CENSUS_REQUIRE(census_first_prctl(run.trace, PR_CAPBSET_READ) < 0,
      "the credential-fact classifier ran on a non-clone release");
  return 0;
}

static int census_scenario_clone_attempt_fence(void) {
  struct census_child_setup setup;
  memset(&setup, 0, sizeof(setup));
  setup.clone_entry = 1;
  setup.clone_mode = 1;
  setup.release_version = RELEASE_CLONE_WIRE_VERSION;
  setup.pinned_bootstrap_stat = 1;
  struct census_run_result run;
  if (census_execute_child(&setup, &run) != 0) return 1;
  if (census_require_child_ok(&run) != 0) return 1;
  long attempt = census_first_sendmsg(run.trace, EXEC_STATUS_FD);
  CENSUS_REQUIRE(attempt >= 0, "no frame was sent on fd %d", EXEC_STATUS_FD);
  const struct census_event *frame = &run.trace->events[attempt];
  CENSUS_REQUIRE(frame->version == CLONE_STATUS_WIRE_VERSION && frame->kind == FRAME_BOOTSTRAP_EXEC_ATTEMPT &&
      frame->size == CLONE_ATTEMPT_SIZE,
      "first fd-%d frame is version=%u kind=%u size=%llu; want version=%d kind=%d size=%d",
      EXEC_STATUS_FD, frame->version, frame->kind, frame->size,
      CLONE_STATUS_WIRE_VERSION, FRAME_BOOTSTRAP_EXEC_ATTEMPT, CLONE_ATTEMPT_SIZE);
  CENSUS_REQUIRE(frame->dev == CENSUS_PINNED_DEV && frame->ino == CENSUS_PINNED_INO,
      "bootstrap attempt dev/ino %llu/%llu does not match the pinned %llu/%llu",
      frame->dev, frame->ino, CENSUS_PINNED_DEV, CENSUS_PINNED_INO);
  CENSUS_REQUIRE(census_last_guard_poll_before(run.trace, attempt) >= 0,
      "no guard poll on the owner fds precedes the bootstrap attempt frame index %ld", attempt);
  long execveat = census_find_op(run.trace, "execveat");
  CENSUS_REQUIRE(execveat > attempt, "execveat index %ld does not follow the bootstrap attempt frame index %ld", execveat, attempt);
  long failure = census_next_sendmsg(run.trace, execveat, EXEC_STATUS_FD, FRAME_CLONE_BOOTSTRAP_FAILURE);
  CENSUS_REQUIRE(failure >= 0, "no kind-%d bootstrap failure frame follows execveat", FRAME_CLONE_BOOTSTRAP_FAILURE);
  const struct census_event *failure_frame = &run.trace->events[failure];
  CENSUS_REQUIRE(failure_frame->version == CLONE_STATUS_WIRE_VERSION &&
      failure_frame->size == CLONE_FAILURE_SIZE && failure_frame->stage == CLONE_FAILURE_EXEC_STAGE,
      "bootstrap failure frame is version=%u size=%llu stage=%u; want version=%d size=%d stage=%d",
      failure_frame->version, failure_frame->size, failure_frame->stage,
      CLONE_STATUS_WIRE_VERSION, CLONE_FAILURE_SIZE, CLONE_FAILURE_EXEC_STAGE);
  setup.expired_lease = 1;
  struct census_run_result expired;
  if (census_execute_child(&setup, &expired) != 0) return 1;
  if (census_require_child_ok(&expired) != 0) return 1;
  CENSUS_REQUIRE(census_count_frames(expired.trace, EXEC_STATUS_FD, FRAME_BOOTSTRAP_EXEC_ATTEMPT) == 0,
      "expired lease still sent a kind-%d bootstrap attempt frame", FRAME_BOOTSTRAP_EXEC_ATTEMPT);
  CENSUS_REQUIRE(census_find_op(expired.trace, "execveat") < 0, "expired lease still reached execveat");
  long expired_frames = census_count_frames(expired.trace, EXEC_STATUS_FD, -1);
  CENSUS_REQUIRE(expired_frames == 1, "expired bootstrap fd-%d frames: %ld; want exactly one failure frame",
      EXEC_STATUS_FD, expired_frames);
  long expired_failure = census_first_sendmsg(expired.trace, EXEC_STATUS_FD);
  const struct census_event *expired_frame = &expired.trace->events[expired_failure];
  CENSUS_REQUIRE(expired_frame->version == CLONE_STATUS_WIRE_VERSION &&
      expired_frame->kind == FRAME_CLONE_BOOTSTRAP_FAILURE && expired_frame->size == CLONE_FAILURE_SIZE &&
      expired_frame->stage == CLONE_FAILURE_EXEC_STAGE,
      "expired bootstrap failure frame is version=%u kind=%u size=%llu stage=%u; want version=%d kind=%d size=%d stage=%d",
      expired_frame->version, expired_frame->kind, expired_frame->size, expired_frame->stage,
      CLONE_STATUS_WIRE_VERSION, FRAME_CLONE_BOOTSTRAP_FAILURE, CLONE_FAILURE_SIZE, CLONE_FAILURE_EXEC_STAGE);
  CENSUS_REQUIRE(expired_frame->error_number == ETIMEDOUT,
      "expired bootstrap failure errno=%u; want ETIMEDOUT(%d)", expired_frame->error_number, ETIMEDOUT);
  return 0;
}

int main(int argc, char **argv) {
  if (argc != 3) {
    fprintf(stderr, "usage: gate-release-fence-probe <scenario> <image>\n");
    return 2;
  }
  census_image_path = argv[2];
  if (strcmp(argv[1], "exec-fence") == 0) return census_scenario_exec_fence();
  if (strcmp(argv[1], "exec-symlinked-image") == 0) return census_scenario_exec_symlinked_image();
  if (strcmp(argv[1], "exec-fence-expired") == 0) return census_scenario_exec_fence_expired();
  if (strcmp(argv[1], "clone-fence-expired") == 0) return census_scenario_clone_fence_expired();
  if (strcmp(argv[1], "exec-drop-denied") == 0) return census_scenario_exec_drop_denied();
  if (strcmp(argv[1], "clone-close-failed") == 0) return census_scenario_clone_close_failed();
  if (strcmp(argv[1], "clone-parent-death-failed") == 0) return census_scenario_clone_parent_death_failed();
  if (strcmp(argv[1], "clone-drop-denied") == 0) return census_scenario_clone_drop_denied();
  if (strcmp(argv[1], "clone-drop-groups") == 0) return census_scenario_clone_drop_groups();
  if (strcmp(argv[1], "clone-drop-bounding-set") == 0) return census_scenario_clone_drop_bounding_set();
  if (strcmp(argv[1], "clone-drop-capabilities") == 0) return census_scenario_clone_drop_capabilities();
  if (strcmp(argv[1], "clone-drop-ambient") == 0) return census_scenario_clone_drop_ambient();
  if (strcmp(argv[1], "clone-drop-no-new-privs") == 0) return census_scenario_clone_drop_no_new_privs();
  if (strcmp(argv[1], "clone-drop-status") == 0) return census_scenario_clone_drop_status();
  if (strcmp(argv[1], "clone-drop-unclassified") == 0) return census_scenario_clone_drop_unclassified();
  if (strcmp(argv[1], "clone-attempt-fence") == 0) return census_scenario_clone_attempt_fence();
  fprintf(stderr, "unknown scenario %s\n", argv[1]);
  return 2;
}
`
	return compileGateProbe(t, source, "release-fence")
}
