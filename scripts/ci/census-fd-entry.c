/* Temporary process-entry oracle: never close or reflag inherited FDs. */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

int main(int argc, char **argv) {
  if (argc < 2) return 126;
  const int descriptors[] = {142, 0, 1, 2};
  struct snapshot {
    int flags;
    int flags_errno;
    int stat_errno;
    struct stat identity;
  } snapshots[4];
  memset(snapshots, 0, sizeof(snapshots));
  for (size_t index = 0; index < sizeof(descriptors) / sizeof(descriptors[0]); ++index) {
    errno = 0;
    snapshots[index].flags = fcntl(descriptors[index], F_GETFD);
    if (snapshots[index].flags < 0) snapshots[index].flags_errno = errno;
    errno = 0;
    if (fstat(descriptors[index], &snapshots[index].identity) != 0) snapshots[index].stat_errno = errno;
  }

  /* Snapshot every descriptor before writing; no logfile is opened here. */
  const char *program = strrchr(argv[1], '/');
  program = program == NULL ? argv[1] : program + 1;
  const char *role = strcmp(program, "go") == 0 ? "go-tool" : "test-binary";
  char report[2048];
  size_t length = 0;
  for (size_t index = 0; index < sizeof(descriptors) / sizeof(descriptors[0]); ++index) {
    const struct snapshot *snapshot = &snapshots[index];
    int count = snprintf(report + length, sizeof(report) - length,
        "census-fd-ownership stage=exec-entry role=%s fd=%d flags=%d flags_errno=%d stat_errno=%d type=%o dev=%llu ino=%llu rdev=%llu\n",
        role, descriptors[index], snapshot->flags, snapshot->flags_errno, snapshot->stat_errno,
        (unsigned int)(snapshot->identity.st_mode & S_IFMT),
        (unsigned long long)snapshot->identity.st_dev, (unsigned long long)snapshot->identity.st_ino,
        (unsigned long long)snapshot->identity.st_rdev);
    if (count < 0 || (size_t)count >= sizeof(report) - length) return 126;
    length += (size_t)count;
  }
  size_t written = 0;
  while (written < length) {
    ssize_t count = write(STDERR_FILENO, report + written, length - written);
    if (count < 0 && errno == EINTR) continue;
    if (count <= 0) return 126;
    written += (size_t)count;
  }
  execv(argv[1], argv + 1);
  int exec_errno = errno;
  int count = snprintf(report, sizeof(report), "census-fd-ownership exec_errno=%d\n", exec_errno);
  if (count > 0 && (size_t)count < sizeof(report)) {
    if (write(STDERR_FILENO, report, (size_t)count) != count) return 126;
  }
  return 126;
}
