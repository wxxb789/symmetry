/*
 * Small static Linux/amd64 A gate. Build this C-only source independently
 * from the Go module. The root-owned launcher executes it before it drops to
 * host UID A; this image then creates its own user namespace as A and parks
 * until one authenticated release. No native code runs before B drop.
 */
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <elf.h>
#include <fcntl.h>
#include <grp.h>
#include <linux/capability.h>
#include <linux/memfd.h>
#include <poll.h>
#include <stdint.h>
#include <sched.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <time.h>
#include <unistd.h>

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
#ifndef SYS_execveat
#error "target Linux syscall headers must define SYS_execveat"
#endif
#ifndef SYS_pidfd_send_signal
#error "target Linux syscall headers must define SYS_pidfd_send_signal"
#endif
#ifndef PR_CAP_AMBIENT
#define PR_CAP_AMBIENT 47
#endif
#ifndef PR_CAP_AMBIENT_CLEAR_ALL
#define PR_CAP_AMBIENT_CLEAR_ALL 4
#endif

#define CLONE_BOOTSTRAP_PATH "/usr/libexec/symmetry/symmetry-linux-clone-bootstrap"
#define GIT_IMAGE_PATH "/usr/bin/git"

enum {
  RELEASE_FD = 4,
  SYNC_FD = 5,
  OWNER_PIDFD = 6,
  EXEC_STATUS_FD = 7,
  IMAGE_FD = 3,
  WIRE_VERSION = 1,
  FRAME_GATE_READY = 3,
  FRAME_GATE_MAP = 4,
  FRAME_GATE_PARKED = 5,
  FRAME_RELEASE = 6,
  FRAME_EXEC_ATTEMPT = 7,
  FRAME_EXEC_FAILURE = 8,
  FRAME_BOOTSTRAP_EXEC_ATTEMPT = 20,
  FRAME_CLONE_BOOTSTRAP_FAILURE = 24,
  CLONE_FAILURE_REQUEST_STAGE = 1,
  CLONE_FAILURE_EXEC_STAGE = 7,
  RELEASE_WIRE_VERSION = 2,
  RELEASE_CLONE_WIRE_VERSION = 3,
  CLONE_STATUS_WIRE_VERSION = 3,
  GATE_READY_SIZE = 16,
  GATE_MAP_SIZE = 60,
  GATE_PARKED_SIZE = 16,
  RELEASE_HEADER_SIZE = 60,
  RELEASE_STDIO_FD_COUNT = 3,
  RELEASE_FD_COUNT = 4,
  EXEC_ATTEMPT_SIZE = 28,
  EXEC_FAILURE_SIZE = 16,
  CLONE_ATTEMPT_SIZE = 28,
  CLONE_FAILURE_SIZE = 20,
  MAX_RELEASE_SIZE = 128 * 1024,
  MAX_COMMAND_STRING = 16 * 1024,
  MAX_COMMAND_VECTOR = 128,
  USERNS_WAIT_MS = 10000,
  RELEASE_WAIT_MS = 300000,
};

static const unsigned char WIRE_MAGIC[8] = {'S', 'Y', 'M', 'L', 'A', '0', '0', '1'};
static int clone_bootstrap_image_fd = -1;
static int clone_git_image_fd = -1;
static struct stat clone_bootstrap_image_stat;

struct owner_config {
  uint32_t a_uid;
  uint32_t a_gid;
  uint32_t b_uid;
  uint32_t b_gid;
};

struct byte_span {
  const unsigned char *data;
  uint32_t length;
};

struct release_command {
  struct byte_span path;
  struct byte_span cwd;
  struct byte_span argv[MAX_COMMAND_VECTOR];
  struct byte_span env[MAX_COMMAND_VECTOR];
  uint32_t argc;
  uint32_t envc;
  int stdio_fds[RELEASE_STDIO_FD_COUNT];
  int owner_pipe_fd;
  uint64_t lease_deadline_ns;
  uint16_t release_version;
  int clone_mode;
  const unsigned char *clone_grant;
  uint32_t clone_grant_size;
};

static int read_span(const unsigned char *body, size_t size, size_t *position,
    struct byte_span *span, uint32_t *total);
static int read_count(const unsigned char *body, size_t size, size_t *position, uint32_t *count);

static uint16_t load_u16(const unsigned char *value) {
  return (uint16_t)value[0] | (uint16_t)((uint16_t)value[1] << 8);
}

static uint32_t load_u32(const unsigned char *value) {
  return (uint32_t)value[0] | ((uint32_t)value[1] << 8) |
      ((uint32_t)value[2] << 16) | ((uint32_t)value[3] << 24);
}

static uint64_t load_u64(const unsigned char *value) {
  return (uint64_t)load_u32(value) | ((uint64_t)load_u32(value + 4) << 32);
}

static void store_u16(unsigned char *value, uint16_t number) {
  value[0] = (unsigned char)number;
  value[1] = (unsigned char)(number >> 8);
}

static void store_u32(unsigned char *value, uint32_t number) {
  value[0] = (unsigned char)number;
  value[1] = (unsigned char)(number >> 8);
  value[2] = (unsigned char)(number >> 16);
  value[3] = (unsigned char)(number >> 24);
}

static void store_u64(unsigned char *value, uint64_t number) {
  store_u32(value, (uint32_t)number);
  store_u32(value + 4, (uint32_t)(number >> 32));
}

static int fixed_header(const unsigned char *packet, size_t size, uint16_t type) {
  return packet != NULL && size >= 12 && memcmp(packet, WIRE_MAGIC, sizeof(WIRE_MAGIC)) == 0 &&
      load_u16(packet + 8) == WIRE_VERSION && load_u16(packet + 10) == type;
}

static int fixed_release_header(const unsigned char *packet, size_t size) {
  uint16_t version;
  if (packet == NULL || size < 12 || memcmp(packet, WIRE_MAGIC, sizeof(WIRE_MAGIC)) != 0) return 0;
  version = load_u16(packet + 8);
	return (version == RELEASE_WIRE_VERSION || version == RELEASE_CLONE_WIRE_VERSION) &&
      load_u16(packet + 10) == FRAME_RELEASE;
}

static int verify_root_directory(const char *path) {
  int fd = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  struct stat st;
  int valid = fstat(fd, &st) == 0 && S_ISDIR(st.st_mode) && st.st_uid == 0 && st.st_gid == 0 &&
      (st.st_mode & 0022) == 0;
  int saved_errno = errno;
  close(fd);
  errno = valid ? saved_errno : EPERM;
  return valid ? 0 : -1;
}

static int verify_amd64_elf_fd(int fd, int require_static) {
  Elf64_Ehdr header;
  ssize_t count;
  do {
    count = pread(fd, &header, sizeof(header), 0);
  } while (count < 0 && errno == EINTR);
  if (count != (ssize_t)sizeof(header) || memcmp(header.e_ident, ELFMAG, SELFMAG) != 0 ||
      header.e_ident[EI_CLASS] != ELFCLASS64 || header.e_ident[EI_DATA] != ELFDATA2LSB ||
      header.e_machine != EM_X86_64 || header.e_phentsize != sizeof(Elf64_Phdr) || header.e_phnum > 128) {
    errno = ENOEXEC;
    return -1;
  }
  for (uint16_t index = 0; index < header.e_phnum; ++index) {
    Elf64_Phdr program;
    off_t offset = (off_t)header.e_phoff + (off_t)index * (off_t)header.e_phentsize;
    do {
      count = pread(fd, &program, sizeof(program), offset);
    } while (count < 0 && errno == EINTR);
    if (count != (ssize_t)sizeof(program)) {
      errno = ENOEXEC;
      return -1;
    }
    if (require_static && program.p_type == PT_INTERP) {
      errno = ENOEXEC;
      return -1;
    }
  }
  return 0;
}

static int open_trusted_image_at(int directory_fd, const char *name, int require_static) {
  int fd = openat(directory_fd, name, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  struct stat st;
  if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) || st.st_uid != 0 || st.st_gid != 0 ||
      (st.st_mode & 06022) != 0 || (st.st_mode & 0111) == 0 || st.st_nlink != 1 ||
      verify_amd64_elf_fd(fd, require_static) != 0) {
    int saved_errno = errno == 0 ? EPERM : errno;
    close(fd);
    errno = saved_errno;
    return -1;
  }
  return fd;
}

static int open_clone_bootstrap_images(void) {
  const char *trusted_directories[] = {"/usr", "/usr/libexec", "/usr/libexec/symmetry", "/usr/bin"};
  for (size_t index = 0; index < sizeof(trusted_directories) / sizeof(trusted_directories[0]); ++index) {
    if (verify_root_directory(trusted_directories[index]) != 0) return -1;
  }
  int symmetry = open("/usr/libexec/symmetry", O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (symmetry < 0) return -1;
  clone_bootstrap_image_fd = open_trusted_image_at(symmetry, "symmetry-linux-clone-bootstrap", 1);
  int saved_errno = errno;
  close(symmetry);
  errno = saved_errno;
  if (clone_bootstrap_image_fd < 0 || fstat(clone_bootstrap_image_fd, &clone_bootstrap_image_stat) != 0) return -1;
  int bin = open("/usr/bin", O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (bin < 0) return -1;
  clone_git_image_fd = open_trusted_image_at(bin, "git", 0);
  saved_errno = errno;
  close(bin);
  errno = saved_errno;
  if (clone_git_image_fd < 0) return -1;
  return 0;
}

static int check_zero_groups(void) {
  int count = getgroups(0, NULL);
  return count == 0 ? 0 : -1;
}

static int check_seqpacket(int fd) {
  int type = 0;
  socklen_t length = sizeof(type);
  if (getsockopt(fd, SOL_SOCKET, SO_TYPE, &type, &length) != 0) return -1;
  if (length != sizeof(type) || type != SOCK_SEQPACKET) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int get_peer(int fd, struct ucred *peer) {
  socklen_t length = sizeof(*peer);
  if (getsockopt(fd, SOL_SOCKET, SO_PEERCRED, peer, &length) != 0) return -1;
  if (length != sizeof(*peer)) {
    errno = EPROTO;
    return -1;
  }
  return 0;
}

static int pidfd_signal_zero(int fd) {
  return (int)syscall(SYS_pidfd_send_signal, fd, 0, NULL, 0);
}

static int pidfd_pid(int fd, pid_t *pid_out) {
  char path[64];
  int length = snprintf(path, sizeof(path), "/proc/self/fdinfo/%d", fd);
  if (length < 0 || (size_t)length >= sizeof(path)) {
    errno = EINVAL;
    return -1;
  }
  int info = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (info < 0) return -1;
  char data[2048];
  ssize_t count;
  do {
    count = read(info, data, sizeof(data) - 1);
  } while (count < 0 && errno == EINTR);
  int saved_errno = errno;
  close(info);
  errno = saved_errno;
  if (count <= 0) return -1;
  data[count] = '\0';
  char *line = data;
  while (line != NULL && *line != '\0') {
    char *next = strchr(line, '\n');
    if (next != NULL) *next++ = '\0';
    if (strncmp(line, "Pid:", 4) == 0) {
      char *end = NULL;
      errno = 0;
      long value = strtol(line + 4, &end, 10);
      while (end != NULL && (*end == ' ' || *end == '\t')) ++end;
      if (errno != 0 || end == line + 4 || end == NULL || *end != '\0' || value <= 1 || value > INT32_MAX) {
        errno = EPROTO;
        return -1;
      }
      *pid_out = (pid_t)value;
      return 0;
    }
    line = next;
  }
  errno = EINVAL;
  return -1;
}

static int parse_proc_numeric_name(const char *name, long minimum, long *value_out) {
  if (name[0] < '0' || name[0] > '9') {
    errno = EPROTO;
    return -1;
  }
  char *end = NULL;
  errno = 0;
  long value = strtol(name, &end, 10);
  if (errno != 0 || end == name || *end != '\0' || value < minimum || value > INT32_MAX) {
    errno = EPROTO;
    return -1;
  }
  *value_out = value;
  return 0;
}

static int verify_single_threaded(void) {
  DIR *tasks = opendir("/proc/self/task");
  if (tasks == NULL) return -1;
  int result = 0;
  int task_count = 0;
  struct dirent *entry;
  for (;;) {
    errno = 0;
    entry = readdir(tasks);
    if (entry == NULL) {
      if (errno != 0) result = -1;
      break;
    }
    if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0) continue;
    long tid;
    if (parse_proc_numeric_name(entry->d_name, 1, &tid) != 0 || tid != (long)getpid() || ++task_count != 1) {
      errno = EPERM;
      result = -1;
      break;
    }
  }
  if (result == 0 && task_count != 1) {
    errno = EPERM;
    result = -1;
  }
  int saved_errno = errno;
  if (closedir(tasks) != 0 && result == 0) {
    result = -1;
    saved_errno = errno;
  }
  errno = saved_errno;
  return result;
}

static int reject_unexpected_inherited_fds(void) {
  /* execve starts this static gate as one thread, and it creates no threads
   * before this census. That keeps the FD table stable while procfs is read. */
  if (verify_single_threaded() != 0) return -1;
  DIR *directory = opendir("/proc/self/fd");
  if (directory == NULL) return -1;
  int directory_fd = dirfd(directory);
  if (directory_fd != IMAGE_FD) {
    int saved_errno = EPROTO;
    closedir(directory);
    errno = saved_errno;
    return -1;
  }
  int result = 0;
  struct dirent *entry;
  for (;;) {
    errno = 0;
    entry = readdir(directory);
    if (entry == NULL) {
      if (errno != 0) result = -1;
      break;
    }
    if (strcmp(entry->d_name, ".") == 0 || strcmp(entry->d_name, "..") == 0) continue;
    long descriptor;
    if (parse_proc_numeric_name(entry->d_name, 0, &descriptor) != 0) {
      result = -1;
      break;
    }
    if (descriptor != directory_fd && descriptor > EXEC_STATUS_FD) {
      errno = EPERM;
      result = -1;
      break;
    }
  }
  int saved_errno = errno;
  if (closedir(directory) != 0 && result == 0) {
    result = -1;
    saved_errno = errno;
  }
  errno = saved_errno;
  return result;
}

static int set_cloexec(int fd) {
  int flags = fcntl(fd, F_GETFD);
  if (flags < 0) return -1;
  return fcntl(fd, F_SETFD, flags | FD_CLOEXEC);
}

static int send_packet(int fd, const unsigned char *packet, size_t size) {
  struct iovec iov = {.iov_base = (void *)packet, .iov_len = size};
  struct msghdr message;
  memset(&message, 0, sizeof(message));
  message.msg_iov = &iov;
  message.msg_iovlen = 1;
  ssize_t count;
  do {
    count = sendmsg(fd, &message, MSG_NOSIGNAL);
  } while (count < 0 && errno == EINTR);
  if (count < 0) return -1;
  if ((size_t)count != size) {
    errno = EIO;
    return -1;
  }
  return 0;
}

static int receive_packet(int fd, unsigned char *packet, size_t capacity, size_t *size_out, int timeout_ms) {
  struct pollfd pfd = {.fd = fd, .events = POLLIN | POLLHUP | POLLERR};
  int ready;
  do {
    ready = poll(&pfd, 1, timeout_ms);
  } while (ready < 0 && errno == EINTR);
  if (ready <= 0) {
    if (ready == 0) errno = ETIMEDOUT;
    return -1;
  }
  unsigned char ancillary[CMSG_SPACE(sizeof(int) * 4)];
  struct iovec iov = {.iov_base = packet, .iov_len = capacity};
  struct msghdr message;
  memset(&message, 0, sizeof(message));
  message.msg_iov = &iov;
  message.msg_iovlen = 1;
  message.msg_control = ancillary;
  message.msg_controllen = sizeof(ancillary);
  ssize_t count;
  do {
    count = recvmsg(fd, &message, MSG_CMSG_CLOEXEC);
  } while (count < 0 && errno == EINTR);
  if (count < 0) return -1;
  int unexpected_fds[16];
  size_t unexpected_count = 0;
  int has_control = 0;
  for (struct cmsghdr *control = CMSG_FIRSTHDR(&message); control != NULL; control = CMSG_NXTHDR(&message, control)) {
    has_control = 1;
    if (control->cmsg_level == SOL_SOCKET && control->cmsg_type == SCM_RIGHTS && control->cmsg_len >= CMSG_LEN(sizeof(int))) {
      size_t number = (control->cmsg_len - CMSG_LEN(0)) / sizeof(int);
      int *received = (int *)CMSG_DATA(control);
      for (size_t index = 0; index < number; ++index) {
        if (unexpected_count < sizeof(unexpected_fds) / sizeof(unexpected_fds[0])) unexpected_fds[unexpected_count++] = received[index];
        else close(received[index]);
      }
    }
  }
  for (size_t index = 0; index < unexpected_count; ++index) close(unexpected_fds[index]);
  if (has_control || unexpected_count != 0 || (message.msg_flags & (MSG_TRUNC | MSG_CTRUNC)) != 0 || count == 0 || (size_t)count > capacity) {
    errno = EPROTO;
    return -1;
  }
  *size_out = (size_t)count;
  return 0;
}

static void close_fd_array(int *fds, size_t count) {
  for (size_t index = 0; index < count; ++index) close(fds[index]);
}

static int receive_release_packet(int fd, unsigned char *packet, size_t capacity,
    size_t *size_out, int release_fds[RELEASE_FD_COUNT]) {
  struct pollfd pfd = {.fd = fd, .events = POLLIN | POLLHUP | POLLERR};
  int ready;
  do {
    ready = poll(&pfd, 1, USERNS_WAIT_MS);
  } while (ready < 0 && errno == EINTR);
  if (ready <= 0) {
    if (ready == 0) errno = ETIMEDOUT;
    return -1;
  }
  unsigned char ancillary[CMSG_SPACE(sizeof(int) * 16)];
  struct iovec iov = {.iov_base = packet, .iov_len = capacity};
  struct msghdr message;
  memset(&message, 0, sizeof(message));
  message.msg_iov = &iov;
  message.msg_iovlen = 1;
  message.msg_control = ancillary;
  message.msg_controllen = sizeof(ancillary);
  ssize_t count;
  do {
    count = recvmsg(fd, &message, MSG_CMSG_CLOEXEC);
  } while (count < 0 && errno == EINTR);
  if (count < 0) return -1;

  int received[16];
  size_t received_count = 0;
  size_t control_count = 0;
  size_t rights_messages = 0;
  int invalid_control = 0;
  for (struct cmsghdr *control = CMSG_FIRSTHDR(&message); control != NULL; control = CMSG_NXTHDR(&message, control)) {
    ++control_count;
    if (control->cmsg_level != SOL_SOCKET || control->cmsg_type != SCM_RIGHTS || control->cmsg_len < CMSG_LEN(0)) {
      invalid_control = 1;
      continue;
    }
    ++rights_messages;
    size_t number = (control->cmsg_len - CMSG_LEN(0)) / sizeof(int);
    int *descriptors = (int *)CMSG_DATA(control);
    for (size_t index = 0; index < number; ++index) {
      if (received_count < sizeof(received) / sizeof(received[0])) received[received_count++] = descriptors[index];
      else close(descriptors[index]);
    }
  }
  if (invalid_control || control_count != 1 || rights_messages != 1 || received_count != RELEASE_FD_COUNT ||
      (message.msg_flags & (MSG_TRUNC | MSG_CTRUNC)) != 0 || count == 0 || (size_t)count > capacity) {
    close_fd_array(received, received_count);
    errno = EPROTO;
    return -1;
  }
  for (size_t index = 0; index < RELEASE_FD_COUNT; ++index) release_fds[index] = received[index];
  *size_out = (size_t)count;
  return 0;
}

static int validate_stdio_pipe(int fd, int expected_access, struct stat *identity) {
  int descriptor_flags = fcntl(fd, F_GETFD);
  int status_flags = fcntl(fd, F_GETFL);
  if (fd < 3 || descriptor_flags < 0 || (descriptor_flags & FD_CLOEXEC) == 0 || status_flags < 0 ||
      (status_flags & O_ACCMODE) != expected_access || fstat(fd, identity) != 0 || !S_ISFIFO(identity->st_mode)) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int validate_owner_pipe(int fd, struct stat *identity) {
  int descriptor_flags = fcntl(fd, F_GETFD);
  int status_flags = fcntl(fd, F_GETFL);
  if (fd < 3 || descriptor_flags < 0 || (descriptor_flags & FD_CLOEXEC) == 0 || status_flags < 0 ||
      (status_flags & O_ACCMODE) != O_RDONLY || (status_flags & O_PATH) != 0 ||
      fstat(fd, identity) != 0 || !S_ISFIFO(identity->st_mode)) {
    /* A verified the host-UID/GID ownership before SCM_RIGHTS transfer. Inside
     * this B-only namespace A's host ID is intentionally unmapped. */
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int install_native_stdio(int pipe_fds[RELEASE_STDIO_FD_COUNT]) {
  for (int target = STDIN_FILENO; target <= STDERR_FILENO; ++target) {
    if (pipe_fds[target] < 3 || dup2(pipe_fds[target], target) < 0) return -1;
  }
  int close_error = 0;
  for (size_t index = 0; index < RELEASE_STDIO_FD_COUNT; ++index) {
    if (close(pipe_fds[index]) != 0) close_error = 1;
    pipe_fds[index] = -1;
  }
  if (close_error) return -1;
  return 0;
}

static int wait_peer_eof(int fd, pid_t owner_pid, int owner_pidfd, int timeout_ms) {
  uint64_t deadline;
  struct timespec now;
  if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
  deadline = (uint64_t)now.tv_sec * 1000u + (uint64_t)now.tv_nsec / 1000000u + (uint64_t)timeout_ms;
  for (;;) {
    if (getppid() != owner_pid || pidfd_signal_zero(owner_pidfd) != 0) {
      errno = ESRCH;
      return -1;
    }
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
    uint64_t current = (uint64_t)now.tv_sec * 1000u + (uint64_t)now.tv_nsec / 1000000u;
    if (current >= deadline) {
      errno = ETIMEDOUT;
      return -1;
    }
    int remaining = (int)(deadline - current);
    if (remaining > 100) remaining = 100;
    struct pollfd fds[2] = {
      {.fd = fd, .events = POLLIN | POLLHUP | POLLERR},
      {.fd = owner_pidfd, .events = POLLIN | POLLHUP | POLLERR},
    };
    int ready;
    do {
      ready = poll(fds, 2, remaining);
    } while (ready < 0 && errno == EINTR);
    if (ready < 0) return -1;
    if (fds[1].revents != 0) {
      errno = ESRCH;
      return -1;
    }
    if (fds[0].revents & POLLIN) {
      unsigned char byte;
      ssize_t count = recv(fd, &byte, sizeof(byte), MSG_DONTWAIT | MSG_PEEK);
      if (count > 0) {
        errno = EPROTO;
        return -1;
      }
      if (count < 0 && errno != EAGAIN && errno != EWOULDBLOCK && errno != EINTR) return -1;
      if (count == 0 && (fds[0].revents & POLLHUP)) return 0;
    }
    if (fds[0].revents & POLLHUP) return 0;
    if (fds[0].revents & POLLERR) {
      errno = EIO;
      return -1;
    }
  }
}

static int check_parent(pid_t owner_pid, int owner_pidfd) {
  if (getppid() != owner_pid) {
    errno = ESRCH;
    return -1;
  }
  return pidfd_signal_zero(owner_pidfd);
}

/* After B drop, signal-zero against A's pidfd is not permitted. Polling the
 * retained CLOEXEC pidfd is passive and needs no signal permission. */
static int check_owner_after_drop(pid_t owner_pid, int owner_pidfd) {
  if (getppid() != owner_pid) {
    errno = ESRCH;
    return -1;
  }
  struct pollfd owner = {.fd = owner_pidfd, .events = POLLIN | POLLHUP | POLLERR};
  int ready;
  do {
    ready = poll(&owner, 1, 0);
  } while (ready < 0 && errno == EINTR);
  if (ready < 0) return -1;
  if (ready != 0 || owner.revents != 0) {
    errno = ESRCH;
    return -1;
  }
  return 0;
}

static int check_exec_guard(pid_t owner_pid, int owner_pidfd, int owner_pipe_fd,
    int exec_status_fd, uint64_t lease_deadline_ns) {
  struct timespec now;
  if (lease_deadline_ns == 0 || clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
  if (now.tv_sec < 0 || now.tv_nsec < 0 || now.tv_nsec >= 1000000000L ||
      (uint64_t)now.tv_sec > (UINT64_MAX - (uint64_t)now.tv_nsec) / UINT64_C(1000000000)) {
    errno = EINVAL;
    return -1;
  }
  uint64_t current_ns = (uint64_t)now.tv_sec * UINT64_C(1000000000) + (uint64_t)now.tv_nsec;
  if (current_ns >= lease_deadline_ns) {
    errno = ETIMEDOUT;
    return -1;
  }
  if (check_owner_after_drop(owner_pid, owner_pidfd) != 0) return -1;

  struct pollfd owner_pipe = {.fd = owner_pipe_fd, .events = POLLIN | POLLHUP | POLLERR};
  int ready;
  do {
    ready = poll(&owner_pipe, 1, 0);
  } while (ready < 0 && errno == EINTR);
  if (ready < 0) return -1;
  if (ready != 0 || owner_pipe.revents != 0) {
    errno = ECANCELED;
    return -1;
  }

  unsigned char unexpected;
  ssize_t count;
  do {
    count = recv(exec_status_fd, &unexpected, sizeof(unexpected), MSG_PEEK | MSG_DONTWAIT);
  } while (count < 0 && errno == EINTR);
  if (count >= 0) {
    errno = ECANCELED;
    return -1;
  }
  if (errno == EAGAIN || errno == EWOULDBLOCK) return 0;
  return -1;
}

static int wait_for_adoption(pid_t owner_pid, int owner_pidfd, pid_t launcher_pid) {
  uint64_t deadline;
  struct timespec now;
  if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
  deadline = (uint64_t)now.tv_sec * 1000u + (uint64_t)now.tv_nsec / 1000000u + USERNS_WAIT_MS;
  for (;;) {
    if (pidfd_signal_zero(owner_pidfd) != 0) return -1;
    pid_t parent = getppid();
    if (parent == owner_pid) {
      if (prctl(PR_SET_PDEATHSIG, SIGKILL) != 0) return -1;
      if (getppid() != owner_pid || check_parent(owner_pid, owner_pidfd) != 0) {
        errno = ESRCH;
        return -1;
      }
      return 0;
    }
    if (parent != launcher_pid) {
      errno = ESRCH;
      return -1;
    }
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
    uint64_t current = (uint64_t)now.tv_sec * 1000u + (uint64_t)now.tv_nsec / 1000000u;
    if (current >= deadline) {
      errno = ETIMEDOUT;
      return -1;
    }
    struct pollfd owner = {.fd = owner_pidfd, .events = POLLIN | POLLHUP | POLLERR};
    int ready;
    do {
      ready = poll(&owner, 1, 10);
    } while (ready < 0 && errno == EINTR);
    if (ready < 0 || owner.revents != 0) {
      if (ready >= 0) errno = ESRCH;
      return -1;
    }
  }
}

static int wait_for_release(pid_t owner_pid, int owner_pidfd, int release_fd) {
  uint64_t deadline;
  struct timespec now;
  if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
  deadline = (uint64_t)now.tv_sec * 1000u + (uint64_t)now.tv_nsec / 1000000u + RELEASE_WAIT_MS;
  for (;;) {
    if (check_parent(owner_pid, owner_pidfd) != 0) return -1;
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) return -1;
    uint64_t current = (uint64_t)now.tv_sec * 1000u + (uint64_t)now.tv_nsec / 1000000u;
    if (current >= deadline) {
      errno = ETIMEDOUT;
      return -1;
    }
    int remaining = (int)(deadline - current);
    if (remaining > 100) remaining = 100;
    struct pollfd fds[2] = {
      {.fd = release_fd, .events = POLLIN | POLLHUP | POLLERR},
      {.fd = owner_pidfd, .events = POLLIN | POLLHUP | POLLERR},
    };
    int ready;
    do {
      ready = poll(fds, 2, remaining);
    } while (ready < 0 && errno == EINTR);
    if (ready < 0) return -1;
    if (fds[1].revents != 0) {
      errno = ESRCH;
      return -1;
    }
    if (fds[0].revents & (POLLIN | POLLHUP | POLLERR)) return 0;
  }
}

static int read_release(pid_t owner_pid, int owner_pidfd, struct owner_config *config,
    unsigned char *token, unsigned char **packet_out, size_t *packet_size_out,
    struct release_command *command) {
  unsigned char *packet = (unsigned char *)malloc(MAX_RELEASE_SIZE + 1);
  if (packet == NULL) return -1;
  size_t packet_size = 0;
  int release_fds[RELEASE_FD_COUNT] = {-1, -1, -1, -1};
  if (receive_release_packet(RELEASE_FD, packet, MAX_RELEASE_SIZE + 1, &packet_size, release_fds) != 0) {
    free(packet);
    return -1;
  }
  if (packet_size < RELEASE_HEADER_SIZE || packet_size > MAX_RELEASE_SIZE ||
      !fixed_release_header(packet, packet_size) ||
      load_u32(packet + 44) != packet_size - RELEASE_HEADER_SIZE ||
      load_u32(packet + 48) != RELEASE_FD_COUNT) goto invalid;
  uint16_t release_version = load_u16(packet + 8);
  uint64_t lease_deadline_ns = load_u64(packet + 52);
  if (lease_deadline_ns == 0) goto invalid;
  struct stat pipe_stat[RELEASE_STDIO_FD_COUNT];
  struct stat owner_pipe_stat;
  if (validate_stdio_pipe(release_fds[0], O_RDONLY, &pipe_stat[0]) != 0 ||
      validate_stdio_pipe(release_fds[1], O_WRONLY, &pipe_stat[1]) != 0 ||
      validate_stdio_pipe(release_fds[2], O_WRONLY, &pipe_stat[2]) != 0 ||
      validate_owner_pipe(release_fds[3], &owner_pipe_stat) != 0) goto invalid;
  for (size_t index = 0; index < RELEASE_STDIO_FD_COUNT; ++index) {
    for (size_t prior = 0; prior < index; ++prior) {
      if (pipe_stat[index].st_dev == pipe_stat[prior].st_dev && pipe_stat[index].st_ino == pipe_stat[prior].st_ino) goto invalid;
    }
    if (pipe_stat[index].st_dev == owner_pipe_stat.st_dev && pipe_stat[index].st_ino == owner_pipe_stat.st_ino) goto invalid;
  }
  unsigned difference = 0;
  for (size_t index = 0; index < 32; ++index) difference |= (unsigned)(packet[12 + index] ^ token[index]);
  if (difference != 0) {
    errno = EACCES;
    goto invalid;
  }

  const unsigned char *body = packet + RELEASE_HEADER_SIZE;
  size_t body_size = packet_size - RELEASE_HEADER_SIZE;
  size_t position = 0;
  uint32_t total = 0;
  if (read_span(body, body_size, &position, &command->path, &total) != 0 ||
      command->path.length == 0 || command->path.length > MAX_COMMAND_STRING ||
      command->path.data[0] != '/' ||
      (command->path.length == 1 && command->path.data[0] == '/')) goto invalid;
  if (read_count(body, body_size, &position, &command->argc) != 0 ||
      command->argc == 0 || command->argc > MAX_COMMAND_VECTOR) goto invalid;
  for (uint32_t index = 0; index < command->argc; ++index) {
    if (read_span(body, body_size, &position, &command->argv[index], &total) != 0 || command->argv[index].length == 0) goto invalid;
  }
  if (read_count(body, body_size, &position, &command->envc) != 0 || command->envc > MAX_COMMAND_VECTOR) goto invalid;
  for (uint32_t index = 0; index < command->envc; ++index) {
    struct byte_span *entry = &command->env[index];
    if (read_span(body, body_size, &position, entry, &total) != 0) goto invalid;
    const unsigned char *equals = (const unsigned char *)memchr(entry->data, '=', entry->length);
    if (equals == NULL || equals == entry->data) goto invalid;
    size_t key_length = (size_t)(equals - entry->data);
    for (uint32_t prior = 0; prior < index; ++prior) {
      const unsigned char *prior_equals = (const unsigned char *)memchr(command->env[prior].data, '=', command->env[prior].length);
      if (prior_equals == NULL) goto invalid;
      size_t prior_key_length = (size_t)(prior_equals - command->env[prior].data);
      if (key_length == prior_key_length && memcmp(entry->data, command->env[prior].data, key_length) == 0) goto invalid;
    }
  }
  if (read_span(body, body_size, &position, &command->cwd, &total) != 0 ||
      command->cwd.length == 0 || command->cwd.data[0] != '/') goto invalid;
  command->clone_mode = release_version == RELEASE_CLONE_WIRE_VERSION;
  if (command->clone_mode) {
    uint32_t clone_size = 0;
    if (read_count(body, body_size, &position, &clone_size) != 0 || clone_size == 0 ||
        (size_t)clone_size != body_size - position || memchr(body + position, 0, clone_size) != NULL ||
        body[position] != '{') goto invalid;
    command->clone_grant = body + position;
    command->clone_grant_size = clone_size;
    position += clone_size;
  }
  if (release_version != RELEASE_WIRE_VERSION && release_version != RELEASE_CLONE_WIRE_VERSION) goto invalid;
  if (!command->clone_mode && release_version != RELEASE_WIRE_VERSION) goto invalid;
  if (position != body_size) goto invalid;
  if (check_parent(owner_pid, owner_pidfd) != 0) goto invalid;
  if (wait_peer_eof(RELEASE_FD, owner_pid, owner_pidfd, USERNS_WAIT_MS) != 0) goto invalid;
  /* RELEASE is the only source of command-specific descriptors after the
   * fixed trusted image opens. Accept exactly three distinct FIFO stdio ends
   * in their required directions plus one distinct read-only owner FIFO;
   * cgroup control files are not in this list and are rejected before
   * exec_as_b drops credentials. */
  for (size_t index = 0; index < RELEASE_STDIO_FD_COUNT; ++index) command->stdio_fds[index] = release_fds[index];
  command->owner_pipe_fd = release_fds[3];
  command->lease_deadline_ns = lease_deadline_ns;
  command->release_version = release_version;
  *packet_out = packet;
  *packet_size_out = packet_size;
  return 0;

invalid:
  close_fd_array(release_fds, RELEASE_FD_COUNT);
  memset(packet, 0, packet_size);
  free(packet);
  errno = EPROTO;
  return -1;
}

static int read_span(const unsigned char *body, size_t size, size_t *position,
    struct byte_span *span, uint32_t *total) {
  if (*position > size || size - *position < 4) return -1;
  uint32_t length = load_u32(body + *position);
  *position += 4;
  if (length > MAX_COMMAND_STRING || (size_t)length > size - *position) return -1;
  const unsigned char *value = body + *position;
  if (memchr(value, 0, length) != NULL) return -1;
  if (*total > MAX_RELEASE_SIZE - RELEASE_HEADER_SIZE - length) return -1;
  *total += length;
  span->data = value;
  span->length = length;
  *position += length;
  return 0;
}

static int read_count(const unsigned char *body, size_t size, size_t *position, uint32_t *count) {
  if (*position > size || size - *position < 4) return -1;
  *count = load_u32(body + *position);
  *position += 4;
  return 0;
}

static char *span_string(const struct byte_span *span) {
  char *value = (char *)malloc((size_t)span->length + 1);
  if (value == NULL) return NULL;
  memcpy(value, span->data, span->length);
  value[span->length] = '\0';
  return value;
}

static int set_capabilities_zero(void) {
  struct __user_cap_header_struct header = {
    .version = _LINUX_CAPABILITY_VERSION_3,
    .pid = 0,
  };
  struct __user_cap_data_struct data[2];
  memset(data, 0, sizeof(data));
  return (int)syscall(SYS_capset, &header, data);
}

static int drop_capability_bounding_set(void) {
  int fd = open("/proc/sys/kernel/cap_last_cap", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  char data[32];
  ssize_t count;
  do {
    count = read(fd, data, sizeof(data) - 1);
  } while (count < 0 && errno == EINTR);
  int saved_errno = errno;
  close(fd);
  errno = saved_errno;
  if (count <= 0) return -1;
  data[count] = '\0';
  char *end = NULL;
  errno = 0;
  long last = strtol(data, &end, 10);
  if (errno != 0 || end == data || last < 0 || last > 63) {
    errno = EINVAL;
    return -1;
  }
  for (long capability = 0; capability <= last; ++capability) {
    if (prctl(PR_CAPBSET_DROP, capability, 0, 0, 0) != 0) return -1;
  }
  return 0;
}

static int read_self_status(struct owner_config *config) {
  int fd = open("/proc/self/status", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  char data[16384];
  ssize_t count;
  do {
    count = read(fd, data, sizeof(data) - 1);
  } while (count < 0 && errno == EINTR);
  int saved_errno = errno;
  close(fd);
  errno = saved_errno;
  if (count <= 0 || count >= (ssize_t)sizeof(data) - 1) return -1;
  data[count] = '\0';
  int seen = 0;
  char *save = NULL;
  for (char *line = strtok_r(data, "\n", &save); line != NULL; line = strtok_r(NULL, "\n", &save)) {
    if (strncmp(line, "Uid:", 4) == 0 || strncmp(line, "Gid:", 4) == 0) {
      int is_uid = line[0] == 'U';
      char *cursor = line + 4;
      uint32_t expected = is_uid ? config->b_uid : config->b_gid;
      for (int index = 0; index < 4; ++index) {
        while (*cursor == ' ' || *cursor == '\t') ++cursor;
        if (*cursor < '0' || *cursor > '9') return -1;
        errno = 0;
        char *end = NULL;
        unsigned long value = strtoul(cursor, &end, 10);
        if (errno != 0 || end == cursor || value != expected) return -1;
        cursor = end;
      }
      if (is_uid) seen |= 1;
      else seen |= 2;
    } else if (strncmp(line, "Groups:", 7) == 0) {
      char *cursor = line + 7;
      while (*cursor == ' ' || *cursor == '\t') ++cursor;
      if (*cursor != '\0') return -1;
      seen |= 4;
    } else if (strncmp(line, "CapEff:", 7) == 0 || strncmp(line, "CapPrm:", 7) == 0 ||
        strncmp(line, "CapInh:", 7) == 0 || strncmp(line, "CapBnd:", 7) == 0 ||
        strncmp(line, "CapAmb:", 7) == 0) {
      char *cursor = line + 7;
      while (*cursor == ' ' || *cursor == '\t') ++cursor;
      if (*cursor != '0') return -1;
      ++cursor;
      while (*cursor == '0') ++cursor;
      if (*cursor != '\0') return -1;
      if (strncmp(line, "CapEff:", 7) == 0) seen |= 8;
      if (strncmp(line, "CapPrm:", 7) == 0) seen |= 16;
      if (strncmp(line, "CapInh:", 7) == 0) seen |= 32;
      if (strncmp(line, "CapBnd:", 7) == 0) seen |= 64;
      if (strncmp(line, "CapAmb:", 7) == 0) seen |= 128;
    } else if (strncmp(line, "NoNewPrivs:", 11) == 0) {
      char *cursor = line + 11;
      while (*cursor == ' ' || *cursor == '\t') ++cursor;
      if (*cursor != '1' || cursor[1] != '\0') return -1;
      seen |= 256;
    }
  }
  if (seen != 511) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int drop_to_b(struct owner_config *config) {
  if (check_zero_groups() != 0 || drop_capability_bounding_set() != 0 ||
      prctl(PR_SET_KEEPCAPS, 0, 0, 0, 0) != 0 ||
      setresgid(1, 1, 1) != 0 || setresuid(1, 1, 1) != 0 ||
      prctl(PR_SET_DUMPABLE, 0, 0, 0, 0) != 0 || set_capabilities_zero() != 0) return -1;
#ifdef PR_CAP_AMBIENT
  if (prctl(PR_CAP_AMBIENT, PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0) != 0 && errno != EINVAL) return -1;
#endif
  if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0) return -1;
  uid_t ruid, euid, suid;
  gid_t rgid, egid, sgid;
  if (getresuid(&ruid, &euid, &suid) != 0 || ruid != 1 || euid != 1 || suid != 1 ||
      getresgid(&rgid, &egid, &sgid) != 0 || rgid != 1 || egid != 1 || sgid != 1 ||
      check_zero_groups() != 0 || read_self_status(config) != 0) return -1;
  return 0;
}

static void send_exec_failure(int error_number) {
  unsigned char packet[EXEC_FAILURE_SIZE];
  memset(packet, 0, sizeof(packet));
  memcpy(packet, WIRE_MAGIC, 8);
  store_u16(packet + 8, WIRE_VERSION);
  store_u16(packet + 10, FRAME_EXEC_FAILURE);
  store_u32(packet + 12, (uint32_t)(error_number > 0 ? error_number : EPERM));
  (void)send_packet(EXEC_STATUS_FD, packet, sizeof(packet));
}

static void send_clone_bootstrap_failure(uint32_t stage, int error_number) {
  unsigned char packet[CLONE_FAILURE_SIZE];
  memset(packet, 0, sizeof(packet));
  memcpy(packet, WIRE_MAGIC, 8);
  store_u16(packet + 8, CLONE_STATUS_WIRE_VERSION);
  store_u16(packet + 10, FRAME_CLONE_BOOTSTRAP_FAILURE);
  store_u32(packet + 12, stage);
  store_u32(packet + 16, (uint32_t)(error_number > 0 ? error_number : EPERM));
  (void)send_packet(EXEC_STATUS_FD, packet, sizeof(packet));
}

static int unset_cloexec(int fd) {
  int flags = fcntl(fd, F_GETFD);
  if (flags < 0) return -1;
  return fcntl(fd, F_SETFD, flags & ~FD_CLOEXEC);
}

static int create_sealed_clone_request(const unsigned char *packet, size_t size) {
#ifdef SYS_memfd_create
  int fd = (int)syscall(SYS_memfd_create, "symmetry-clone-request", MFD_CLOEXEC | MFD_ALLOW_SEALING);
#else
  int fd = -1;
  errno = ENOSYS;
#endif
  if (fd < 0) return -1;
  size_t written = 0;
  while (written < size) {
    ssize_t count = write(fd, packet + written, size - written);
    if (count < 0 && errno == EINTR) continue;
    if (count <= 0) {
      int saved_errno = count == 0 ? EIO : errno;
      close(fd);
      errno = saved_errno;
      return -1;
    }
    written += (size_t)count;
  }
  if (fcntl(fd, F_ADD_SEALS, F_SEAL_SEAL | F_SEAL_SHRINK | F_SEAL_GROW | F_SEAL_WRITE) != 0 ||
      lseek(fd, 0, SEEK_SET) < 0) {
    int saved_errno = errno;
    close(fd);
    errno = saved_errno;
    return -1;
  }
  return fd;
}

static int open_native_image(const struct byte_span *path, struct stat *status) {
  char *filename = span_string(path);
  if (filename == NULL) {
    errno = ENOMEM;
    return -1;
  }
  int fd = open(filename, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  int saved_errno = errno;
  free(filename);
  errno = saved_errno;
  if (fd < 0) return -1;
  if (fstat(fd, status) != 0 || !S_ISREG(status->st_mode) || (status->st_mode & 0111) == 0) {
    int error_number = errno == 0 ? EACCES : errno;
    close(fd);
    errno = error_number;
    return -1;
  }
  unsigned char elf[20];
  ssize_t count;
  do {
    count = pread(fd, elf, sizeof(elf), 0);
  } while (count < 0 && errno == EINTR);
  uint16_t expected_machine;
#if defined(__x86_64__)
  expected_machine = EM_X86_64;
#else
#error "the Linux A gate supports only amd64"
#endif
  if (count != (ssize_t)sizeof(elf) || memcmp(elf, "\177ELF", 4) != 0 || elf[4] != 2 || elf[5] != 1 ||
      load_u16(elf + 18) != expected_machine || (load_u16(elf + 16) != 2 && load_u16(elf + 16) != 3)) {
    close(fd);
    errno = ENOEXEC;
    return -1;
  }
  return fd;
}

static int format_fd_argument(char *buffer, size_t capacity, int fd) {
  int length = snprintf(buffer, capacity, "%d", fd);
  if (length < 0 || (size_t)length >= capacity) {
    errno = EOVERFLOW;
    return -1;
  }
  return 0;
}

static int format_uid_argument(char *buffer, size_t capacity, uint32_t uid) {
  int length = snprintf(buffer, capacity, "%u", uid);
  if (length < 0 || (size_t)length >= capacity) {
    errno = EOVERFLOW;
    return -1;
  }
  return 0;
}

static void exec_clone_bootstrap(const struct owner_config *config,
    unsigned char *packet, size_t packet_size, const struct release_command *command,
    pid_t owner_pid) {
  if (!command->clone_mode || command->release_version != RELEASE_CLONE_WIRE_VERSION ||
      command->clone_grant == NULL || command->clone_grant_size == 0 ||
      clone_bootstrap_image_fd < 0 || clone_git_image_fd < 0) {
    send_clone_bootstrap_failure(CLONE_FAILURE_REQUEST_STAGE, EPROTO);
    _exit(1);
  }
  int request_fd = create_sealed_clone_request(packet, packet_size);
  if (request_fd < 0) {
    int error_number = errno;
    send_clone_bootstrap_failure(CLONE_FAILURE_REQUEST_STAGE, error_number);
    _exit(1);
  }

  int inherited[] = {
    clone_bootstrap_image_fd, clone_git_image_fd, request_fd, OWNER_PIDFD,
    EXEC_STATUS_FD, command->owner_pipe_fd, command->stdio_fds[0],
    command->stdio_fds[1], command->stdio_fds[2],
  };
  for (size_t index = 0; index < sizeof(inherited) / sizeof(inherited[0]); ++index) {
    if (unset_cloexec(inherited[index]) != 0) {
      int error_number = errno;
      close(request_fd);
      send_clone_bootstrap_failure(CLONE_FAILURE_EXEC_STAGE, error_number);
      _exit(1);
    }
  }
  if (check_exec_guard(owner_pid, OWNER_PIDFD, command->owner_pipe_fd,
      EXEC_STATUS_FD, command->lease_deadline_ns) != 0) {
    int error_number = errno == 0 ? ECANCELED : errno;
    close(request_fd);
    send_clone_bootstrap_failure(CLONE_FAILURE_EXEC_STAGE, error_number);
    _exit(1);
  }

  unsigned char attempt[CLONE_ATTEMPT_SIZE];
  memset(attempt, 0, sizeof(attempt));
  memcpy(attempt, WIRE_MAGIC, 8);
  store_u16(attempt + 8, CLONE_STATUS_WIRE_VERSION);
  store_u16(attempt + 10, FRAME_BOOTSTRAP_EXEC_ATTEMPT);
  store_u64(attempt + 12, (uint64_t)clone_bootstrap_image_stat.st_dev);
  store_u64(attempt + 20, (uint64_t)clone_bootstrap_image_stat.st_ino);
  if (send_packet(EXEC_STATUS_FD, attempt, sizeof(attempt)) != 0) {
    int error_number = errno;
    close(request_fd);
    send_clone_bootstrap_failure(CLONE_FAILURE_EXEC_STAGE, error_number);
    _exit(1);
  }

  char request_arg[24], git_arg[24], owner_pidfd_arg[24], status_arg[24], owner_pipe_arg[24];
  char stdin_arg[24], stdout_arg[24], stderr_arg[24], uid_arg[24], gid_arg[24];
  if (format_fd_argument(request_arg, sizeof(request_arg), request_fd) != 0 ||
      format_fd_argument(git_arg, sizeof(git_arg), clone_git_image_fd) != 0 ||
      format_fd_argument(owner_pidfd_arg, sizeof(owner_pidfd_arg), OWNER_PIDFD) != 0 ||
      format_fd_argument(status_arg, sizeof(status_arg), EXEC_STATUS_FD) != 0 ||
      format_fd_argument(owner_pipe_arg, sizeof(owner_pipe_arg), command->owner_pipe_fd) != 0 ||
      format_fd_argument(stdin_arg, sizeof(stdin_arg), command->stdio_fds[0]) != 0 ||
      format_fd_argument(stdout_arg, sizeof(stdout_arg), command->stdio_fds[1]) != 0 ||
      format_fd_argument(stderr_arg, sizeof(stderr_arg), command->stdio_fds[2]) != 0 ||
      format_uid_argument(uid_arg, sizeof(uid_arg), config->b_uid) != 0 ||
      format_uid_argument(gid_arg, sizeof(gid_arg), config->b_gid) != 0) {
    int error_number = errno == 0 ? EOVERFLOW : errno;
    close(request_fd);
    send_clone_bootstrap_failure(CLONE_FAILURE_EXEC_STAGE, error_number);
    _exit(1);
  }
  char *const bootstrap_argv[] = {
    (char *)"symmetry-linux-clone-bootstrap", request_arg, git_arg,
    owner_pidfd_arg, status_arg, owner_pipe_arg,
    stdin_arg, stdout_arg, stderr_arg, uid_arg, gid_arg, NULL,
  };
  char *const bootstrap_env[] = {(char *)"PATH=/usr/bin:/bin", (char *)"LANG=C", NULL};
  syscall(SYS_execveat, clone_bootstrap_image_fd, "", bootstrap_argv, bootstrap_env, AT_EMPTY_PATH);
  int error_number = errno;
  close(request_fd);
  send_clone_bootstrap_failure(CLONE_FAILURE_EXEC_STAGE, error_number);
  _exit(1);
}

static void exec_as_b(struct owner_config *config, unsigned char *packet,
    size_t packet_size, const struct release_command *command, pid_t owner_pid) {
  memset(packet + 12, 0, 32);
  if (close(RELEASE_FD) != 0 || drop_to_b(config) != 0 ||
      prctl(PR_SET_PDEATHSIG, SIGKILL) != 0 ||
      check_exec_guard(owner_pid, OWNER_PIDFD, command->owner_pipe_fd, EXEC_STATUS_FD, command->lease_deadline_ns) != 0) {
    int error_number = errno;
    send_exec_failure(error_number);
    _exit(1);
  }
  if (command->clone_mode) exec_clone_bootstrap(config, packet, packet_size, command, owner_pid);
  char *cwd = span_string(&command->cwd);
  char *path = span_string(&command->path);
  char **argv = (char **)calloc((size_t)command->argc + 1, sizeof(char *));
  char **envp = (char **)calloc((size_t)command->envc + 1, sizeof(char *));
  if (cwd == NULL || path == NULL || argv == NULL || envp == NULL) {
    send_exec_failure(ENOMEM);
    _exit(1);
  }
  for (uint32_t index = 0; index < command->argc; ++index) {
    argv[index] = span_string(&command->argv[index]);
    if (argv[index] == NULL) {
      send_exec_failure(ENOMEM);
      _exit(1);
    }
  }
  for (uint32_t index = 0; index < command->envc; ++index) {
    envp[index] = span_string(&command->env[index]);
    if (envp[index] == NULL) {
      send_exec_failure(ENOMEM);
      _exit(1);
    }
  }
  if (chdir(cwd) != 0 || prctl(PR_SET_DUMPABLE, 0, 0, 0, 0) != 0 || read_self_status(config) != 0) {
    int error_number = errno == 0 ? EPERM : errno;
    send_exec_failure(error_number);
    _exit(1);
  }
  struct stat image_stat;
  int image = open_native_image(&command->path, &image_stat);
  if (image < 0) {
    send_exec_failure(errno);
    _exit(1);
  }
  if (install_native_stdio((int *)command->stdio_fds) != 0) {
    int error_number = errno == 0 ? EIO : errno;
    close(image);
    send_exec_failure(error_number);
    _exit(1);
  }
  if (set_cloexec(EXEC_STATUS_FD) != 0 || set_cloexec(OWNER_PIDFD) != 0 ||
      set_cloexec(command->owner_pipe_fd) != 0) {
    int error_number = errno;
    close(image);
    send_exec_failure(error_number);
    _exit(1);
  }
  if (check_exec_guard(owner_pid, OWNER_PIDFD, command->owner_pipe_fd, EXEC_STATUS_FD,
      command->lease_deadline_ns) != 0) {
    int error_number = errno == 0 ? ECANCELED : errno;
    close(image);
    send_exec_failure(error_number);
    _exit(1);
  }
  unsigned char attempt[EXEC_ATTEMPT_SIZE];
  memset(attempt, 0, sizeof(attempt));
  memcpy(attempt, WIRE_MAGIC, 8);
  store_u16(attempt + 8, WIRE_VERSION);
  store_u16(attempt + 10, FRAME_EXEC_ATTEMPT);
  store_u64(attempt + 12, (uint64_t)image_stat.st_dev);
  store_u64(attempt + 20, (uint64_t)image_stat.st_ino);
  if (send_packet(EXEC_STATUS_FD, attempt, sizeof(attempt)) != 0) {
    close(image);
    _exit(1);
  }
  /* Poll/execveat cannot be atomic. If abort wins after this final check, A's
   * retained exact pidfd and run-leaf kill remain the bounded recovery fence. */
  if (check_exec_guard(owner_pid, OWNER_PIDFD, command->owner_pipe_fd, EXEC_STATUS_FD,
      command->lease_deadline_ns) != 0) {
    int error_number = errno == 0 ? ECANCELED : errno;
    close(image);
    send_exec_failure(error_number);
    _exit(1);
  }
  (void)packet_size;
  syscall(SYS_execveat, image, "", argv, envp, AT_EMPTY_PATH);
  int error_number = errno;
  close(image);
  send_exec_failure(error_number);
  _exit(1);
}

static int gate_run(void) {
  if (close(IMAGE_FD) != 0) return -1;
  uid_t root_ruid, root_euid, root_suid;
  gid_t root_rgid, root_egid, root_sgid;
  if (getresuid(&root_ruid, &root_euid, &root_suid) != 0 || root_ruid == 0 || root_euid != 0 || root_suid != 0 ||
      getresgid(&root_rgid, &root_egid, &root_sgid) != 0 || root_rgid == 0 || root_egid != root_rgid || root_sgid != root_rgid ||
      check_zero_groups() != 0) return -1;
  if (check_seqpacket(RELEASE_FD) != 0 || check_seqpacket(SYNC_FD) != 0 || check_seqpacket(EXEC_STATUS_FD) != 0) return -1;
  struct ucred sync_peer, release_peer, status_peer;
  if (get_peer(SYNC_FD, &sync_peer) != 0 || get_peer(RELEASE_FD, &release_peer) != 0 ||
      get_peer(EXEC_STATUS_FD, &status_peer) != 0 || sync_peer.uid != 0 || sync_peer.pid != getppid() ||
      release_peer.pid <= 1 || release_peer.uid == 0 || status_peer.pid != release_peer.pid ||
      status_peer.uid != release_peer.uid || status_peer.gid != release_peer.gid) return -1;
  pid_t launcher_pid = sync_peer.pid;
  pid_t owner_pid = release_peer.pid;
  pid_t owner_pidfd_pid = 0;
  if (pidfd_pid(OWNER_PIDFD, &owner_pidfd_pid) != 0 || owner_pidfd_pid != owner_pid || pidfd_signal_zero(OWNER_PIDFD) != 0) return -1;
  if (reject_unexpected_inherited_fds() != 0) return -1;
  if (set_cloexec(RELEASE_FD) != 0 || set_cloexec(SYNC_FD) != 0 || set_cloexec(OWNER_PIDFD) != 0 ||
      set_cloexec(EXEC_STATUS_FD) != 0 || open_clone_bootstrap_images() != 0) return -1;

  uid_t a_uid = getuid();
  gid_t a_gid = getgid();
  if (a_uid == 0 || a_gid == 0 || prctl(PR_SET_DUMPABLE, 0, 0, 0, 0) != 0 ||
      prctl(PR_SET_KEEPCAPS, 0, 0, 0, 0) != 0 ||
      setresgid(a_gid, a_gid, a_gid) != 0 || setresuid(a_uid, a_uid, a_uid) != 0 ||
      set_capabilities_zero() != 0 || check_zero_groups() != 0 ||
      release_peer.uid != a_uid || release_peer.gid != a_gid) return -1;
  uid_t check_ru, check_eu, check_su;
  gid_t check_rg, check_eg, check_sg;
  if (getresuid(&check_ru, &check_eu, &check_su) != 0 || check_ru != a_uid || check_eu != a_uid || check_su != a_uid ||
      getresgid(&check_rg, &check_eg, &check_sg) != 0 || check_rg != a_gid || check_eg != a_gid || check_sg != a_gid ||
      unshare(CLONE_NEWUSER) != 0 || prctl(PR_SET_DUMPABLE, 0, 0, 0, 0) != 0 || check_zero_groups() != 0) return -1;

  unsigned char ready[GATE_READY_SIZE];
  memset(ready, 0, sizeof(ready));
  memcpy(ready, WIRE_MAGIC, 8);
  store_u16(ready + 8, WIRE_VERSION);
  store_u16(ready + 10, FRAME_GATE_READY);
  if (send_packet(SYNC_FD, ready, sizeof(ready)) != 0) return -1;

  unsigned char map_frame[GATE_MAP_SIZE + 1];
  size_t map_size = 0;
  if (receive_packet(SYNC_FD, map_frame, sizeof(map_frame), &map_size, USERNS_WAIT_MS) != 0 ||
      map_size != GATE_MAP_SIZE || !fixed_header(map_frame, map_size, FRAME_GATE_MAP)) return -1;
  struct owner_config config = {
    .a_uid = load_u32(map_frame + 12),
    .a_gid = load_u32(map_frame + 16),
    .b_uid = load_u32(map_frame + 20),
    .b_gid = load_u32(map_frame + 24),
  };
  unsigned char release_token[32];
  memcpy(release_token, map_frame + 28, sizeof(release_token));
  memset(map_frame, 0, sizeof(map_frame));
  if (config.a_uid != a_uid || config.a_gid != a_gid || config.b_uid == 0 || config.b_gid == 0 ||
      config.b_uid == config.a_uid || config.b_gid == config.a_gid ||
      release_peer.uid != config.a_uid || release_peer.gid != config.a_gid) return -1;

  unsigned char parked[GATE_PARKED_SIZE];
  memset(parked, 0, sizeof(parked));
  memcpy(parked, WIRE_MAGIC, 8);
  store_u16(parked + 8, WIRE_VERSION);
  store_u16(parked + 10, FRAME_GATE_PARKED);
  if (send_packet(SYNC_FD, parked, sizeof(parked)) != 0) return -1;
  if (close(SYNC_FD) != 0) return -1;
  if (wait_for_adoption(owner_pid, OWNER_PIDFD, launcher_pid) != 0 || wait_for_release(owner_pid, OWNER_PIDFD, RELEASE_FD) != 0) {
    memset(release_token, 0, sizeof(release_token));
    return -1;
  }
  if (check_parent(owner_pid, OWNER_PIDFD) != 0) {
    memset(release_token, 0, sizeof(release_token));
    return -1;
  }

  struct release_command command;
  memset(&command, 0, sizeof(command));
  unsigned char *packet = NULL;
  size_t packet_size = 0;
  if (read_release(owner_pid, OWNER_PIDFD, &config, release_token, &packet, &packet_size, &command) != 0) {
    memset(release_token, 0, sizeof(release_token));
    return -1;
  }
  memset(release_token, 0, sizeof(release_token));
  exec_as_b(&config, packet, packet_size, &command, owner_pid);
  return -1;
}

int main(int argc, char **argv, char **envp) {
  (void)envp;
  if (argc != 1 || argv == NULL || argv[0] == NULL || strcmp(argv[0], "symmetry-linux-gate") != 0) _exit(1);
  if (getenv("PATH") == NULL || strcmp(getenv("PATH"), "/usr/bin:/bin") != 0 ||
      getenv("LANG") == NULL || strcmp(getenv("LANG"), "C") != 0) _exit(1);
  extern char **environ;
  if (environ == NULL || environ[0] == NULL || environ[1] == NULL || environ[2] != NULL ||
      strcmp(environ[0], "PATH=/usr/bin:/bin") != 0 || strcmp(environ[1], "LANG=C") != 0 || chdir("/") != 0) _exit(1);
  if (gate_run() != 0) _exit(1);
  _exit(1);
}
