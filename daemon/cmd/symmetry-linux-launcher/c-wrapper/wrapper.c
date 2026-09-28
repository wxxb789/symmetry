/*
 * Minimal setuid entry point. Build this C-only source as a static Linux/amd64
 * executable; only this binary receives root:A_GID mode 04750. It authenticates A and the inherited
 * descriptors, clears supplementary groups, replaces stdio/cwd/environment,
 * then execveat()s the non-setuid static Go launcher core by pinned FD.
 */
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <grp.h>
#include <linux/magic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/statfs.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <signal.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/sysmacros.h>

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
#ifndef O_PATH
#define O_PATH 010000000
#endif
#ifndef SYS_execveat
#error "target Linux syscall headers must define SYS_execveat"
#endif
#if !defined(__x86_64__)
#error "the Linux owner launcher supports only amd64"
#endif

enum {
  CONTROL_FD = 3,
  LEAF_FD = 4,
  RELEASE_FD = 5,
  EXEC_STATUS_FD = 6,
  MAX_CONFIG = 512,
};

struct owner_config {
  uint32_t a_uid;
  uint32_t a_gid;
  uint32_t b_uid;
  uint32_t b_gid;
};

static void die(void) { _exit(126); }

static int parse_u32_line(const char *line, const char *key, uint32_t *out) {
  size_t key_len = strlen(key);
  if (strncmp(line, key, key_len) != 0 || line[key_len] != '=') return -1;
  const char *value = line + key_len + 1;
  if (*value == '\0' || *value < '0' || *value > '9') return -1;
  errno = 0;
  char *end = NULL;
  unsigned long parsed = strtoul(value, &end, 10);
  if (errno != 0 || end == value || *end != '\0' || parsed > UINT32_MAX) return -1;
  char canonical[32];
  int count = snprintf(canonical, sizeof(canonical), "%lu", parsed);
  if (count < 0 || (size_t)count >= sizeof(canonical) || strcmp(canonical, value) != 0) return -1;
  *out = (uint32_t)parsed;
  return 0;
}

static int verify_root_directory(const char *path) {
  int fd = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  struct stat st;
  int ok = fstat(fd, &st) == 0 && S_ISDIR(st.st_mode) && st.st_uid == 0 && st.st_gid == 0 &&
      (st.st_mode & 0022) == 0;
  close(fd);
  if (!ok) errno = EPERM;
  return ok ? 0 : -1;
}

static int verify_setuid_image(uint32_t a_gid) {
  int fd = open("/proc/self/exe", O_PATH | O_CLOEXEC);
  if (fd < 0) return -1;
  struct stat st;
  int ok = fstat(fd, &st) == 0 && S_ISREG(st.st_mode) && st.st_uid == 0 && st.st_gid == a_gid &&
      (st.st_mode & 07777) == 04750 && st.st_nlink == 1;
  close(fd);
  if (!ok) errno = EPERM;
  return ok ? 0 : -1;
}

static int read_owner_config(struct owner_config *config) {
  if (verify_root_directory("/etc") != 0 || verify_root_directory("/etc/symmetry") != 0) return -1;
  int fd = open("/etc/symmetry/linux-owner.conf", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  struct stat st;
  if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) || st.st_uid != 0 || st.st_gid != 0 ||
      (st.st_mode & 0022) != 0 || st.st_nlink != 1 || st.st_size <= 0 || st.st_size > MAX_CONFIG) {
    close(fd);
    errno = EPERM;
    return -1;
  }
  char data[MAX_CONFIG + 1];
  size_t total = 0;
  while (total < (size_t)st.st_size) {
    ssize_t n = read(fd, data + total, (size_t)st.st_size - total);
    if (n < 0 && errno == EINTR) continue;
    if (n <= 0) {
      close(fd);
      errno = EIO;
      return -1;
    }
    total += (size_t)n;
  }
  char extra;
  ssize_t extra_count;
  do {
    extra_count = read(fd, &extra, 1);
  } while (extra_count < 0 && errno == EINTR);
  int close_result = close(fd);
  if (extra_count != 0 || close_result != 0 || total != (size_t)st.st_size) {
    errno = EIO;
    return -1;
  }
  data[total] = '\0';

  char *cursor = data;
  char *line = strtok(cursor, "\n");
  if (line == NULL || strcmp(line, "version=1") != 0) return -1;
  line = strtok(NULL, "\n");
  if (line == NULL || parse_u32_line(line, "a_uid", &config->a_uid) != 0) return -1;
  line = strtok(NULL, "\n");
  if (line == NULL || parse_u32_line(line, "a_gid", &config->a_gid) != 0) return -1;
  line = strtok(NULL, "\n");
  if (line == NULL || parse_u32_line(line, "b_uid", &config->b_uid) != 0) return -1;
  line = strtok(NULL, "\n");
  if (line == NULL || parse_u32_line(line, "b_gid", &config->b_gid) != 0) return -1;
  if (strtok(NULL, "\n") != NULL || config->a_uid == 0 || config->b_uid == 0 ||
      config->a_uid == config->b_uid || config->a_gid == 0 || config->b_gid == 0 ||
      config->a_gid == config->b_gid) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int verify_peer(int fd, const struct ucred *expected) {
  int type = 0;
  socklen_t type_len = sizeof(type);
  if (getsockopt(fd, SOL_SOCKET, SO_TYPE, &type, &type_len) != 0 ||
      type_len != sizeof(type) || type != SOCK_SEQPACKET) return -1;
  struct ucred peer;
  socklen_t peer_len = sizeof(peer);
  if (getsockopt(fd, SOL_SOCKET, SO_PEERCRED, &peer, &peer_len) != 0 || peer_len != sizeof(peer)) return -1;
  if (peer.pid != expected->pid || peer.uid != expected->uid || peer.gid != expected->gid) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int verify_leaf(int fd, uint32_t a_uid) {
  struct statfs filesystem;
  struct stat st;
  if (fstatfs(fd, &filesystem) != 0 || fstat(fd, &st) != 0) return -1;
  if ((unsigned long)filesystem.f_type != (unsigned long)CGROUP2_SUPER_MAGIC ||
      !S_ISDIR(st.st_mode) || st.st_uid != a_uid) {
    errno = EPERM;
    return -1;
  }
  return 0;
}

static int close_unexpected_fds(void) {
#ifdef SYS_close_range
  if (syscall(SYS_close_range, 7u, ~0u, 0u) == 0) return 0;
  if (errno != ENOSYS && errno != EINVAL && errno != EPERM) return -1;
#endif
  DIR *directory = opendir("/proc/self/fd");
  if (directory == NULL) return -1;
  int directory_fd = dirfd(directory);
  struct dirent *entry;
  while ((entry = readdir(directory)) != NULL) {
    char *end = NULL;
    errno = 0;
    long value = strtol(entry->d_name, &end, 10);
    if (errno != 0 || end == entry->d_name || *end != '\0' || value < 7 || value == directory_fd) continue;
    close((int)value);
  }
  return closedir(directory);
}

static int replace_standard_fds(void) {
  int fd = open("/dev/null", O_RDWR | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  struct stat st;
  if (fstat(fd, &st) != 0 || !S_ISCHR(st.st_mode) || major(st.st_rdev) != 1 || minor(st.st_rdev) != 3) {
    close(fd);
    errno = EPERM;
    return -1;
  }
  for (int target = STDIN_FILENO; target <= STDERR_FILENO; ++target) {
    if (dup2(fd, target) < 0) {
      close(fd);
      return -1;
    }
  }
  if (fd > STDERR_FILENO) close(fd);
  return 0;
}

static int verify_trusted_directory(void) {
  const char *paths[] = {"/usr", "/usr/libexec", "/usr/libexec/symmetry"};
  for (size_t i = 0; i < sizeof(paths) / sizeof(paths[0]); ++i) {
    if (verify_root_directory(paths[i]) != 0) return -1;
  }
  return 0;
}

static int open_core_image(void) {
  if (verify_trusted_directory() != 0) return -1;
  int directory = open("/usr/libexec/symmetry", O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (directory < 0) return -1;
  int fd = openat(directory, "symmetry-linux-launcher-core", O_PATH | O_CLOEXEC | O_NOFOLLOW);
  int saved_errno = errno;
  close(directory);
  errno = saved_errno;
  if (fd < 0) return -1;
  struct stat st;
  if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode) || st.st_uid != 0 || st.st_gid != 0 ||
      (st.st_mode & 0022) != 0 || (st.st_mode & 0111) == 0 || st.st_nlink != 1) {
    close(fd);
    errno = EPERM;
    return -1;
  }
  return fd;
}

int main(int argc, char **argv) {
  (void)argc;
  (void)argv;
  if (clearenv() != 0) die();
  uid_t ruid, euid, suid;
  gid_t rgid, egid, sgid;
  if (getresuid(&ruid, &euid, &suid) != 0 || ruid == 0 || euid != 0 || suid != 0 ||
      getresgid(&rgid, &egid, &sgid) != 0 || rgid == 0 || egid != rgid || sgid != rgid) die();
  struct owner_config config;
  if (read_owner_config(&config) != 0 || ruid != config.a_uid || rgid != config.a_gid) die();
  if (verify_setuid_image(config.a_gid) != 0) die();

  struct ucred expected = {.pid = getppid(), .uid = config.a_uid, .gid = config.a_gid};
  if (expected.pid <= 1 || verify_peer(CONTROL_FD, &expected) != 0 ||
      verify_peer(RELEASE_FD, &expected) != 0 || verify_peer(EXEC_STATUS_FD, &expected) != 0 ||
      verify_leaf(LEAF_FD, config.a_uid) != 0) die();

  int control_flags = fcntl(CONTROL_FD, F_GETFD);
  int leaf_flags = fcntl(LEAF_FD, F_GETFD);
  int release_flags = fcntl(RELEASE_FD, F_GETFD);
  int status_flags = fcntl(EXEC_STATUS_FD, F_GETFD);
  if (control_flags < 0 || leaf_flags < 0 || release_flags < 0 || status_flags < 0 ||
      fcntl(CONTROL_FD, F_SETFD, control_flags & ~FD_CLOEXEC) != 0 ||
      fcntl(LEAF_FD, F_SETFD, leaf_flags & ~FD_CLOEXEC) != 0 ||
      fcntl(RELEASE_FD, F_SETFD, release_flags & ~FD_CLOEXEC) != 0 ||
      fcntl(EXEC_STATUS_FD, F_SETFD, status_flags & ~FD_CLOEXEC) != 0) die();

  if (close_unexpected_fds() != 0 || replace_standard_fds() != 0) die();
  /* The Go core's authenticated READY frame must never precede this clear. */
  if (setgroups(0, NULL) != 0 || getgroups(0, NULL) != 0) die();
  if (chdir("/") != 0) die();
  umask(0077);
  if (clearenv() != 0) die();
  if (prctl(PR_SET_PDEATHSIG, SIGKILL) != 0 || getppid() != expected.pid) die();

  int core_fd = open_core_image();
  if (core_fd < 0) die();
  char *const child_argv[] = {(char *)"symmetry-linux-launcher-core", NULL};
  char *const child_env[] = {(char *)"PATH=/usr/bin:/bin", (char *)"LANG=C", NULL};
  syscall(SYS_execveat, core_fd, "", child_argv, child_env, AT_EMPTY_PATH);
  close(core_fd);
  die();
  return 126;
}
