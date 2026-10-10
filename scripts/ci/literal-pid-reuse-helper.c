#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <linux/capability.h>
#include <linux/fs.h>
#include <linux/sched.h>
#include <poll.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/random.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/sysmacros.h>
#include <time.h>
#include <unistd.h>

#define MAX_JSON 16384
#define MAX_TOKENS 512
#define MAX_PATH 2048
#define MAX_TEXT 256

enum token_type { TOK_OBJECT, TOK_STRING, TOK_PRIMITIVE };

struct token {
  enum token_type type;
  size_t start;
  size_t end;
  size_t next;
  int parent;
};

struct parser {
  const char *text;
  size_t length;
  size_t cursor;
  struct token tokens[MAX_TOKENS];
  size_t count;
};

struct request {
  char run_id[MAX_TEXT];
  char claim_id[MAX_TEXT];
  char old_identity_sha256[65];
  char boot_id[37];
  char leaf_parent_path[MAX_PATH];
  char leaf_name[MAX_TEXT];
  uint64_t generation;
  uint64_t runtime_epoch;
  uint64_t pid_namespace_device;
  uint64_t pid_namespace_inode;
  uint64_t leaf_mount_id;
  uint64_t leaf_parent_device;
  uint64_t leaf_parent_inode;
  uint64_t leaf_device;
  uint64_t leaf_inode;
  pid_t dead_target_pid;
};

struct cleanup_request {
  char run_id[MAX_TEXT];
  char replacement_identity_sha256[65];
  uint64_t generation;
  pid_t replacement_pid;
};

struct cleanup_acknowledgement {
  char run_id[MAX_TEXT];
  uint64_t generation;
};

struct live_child {
  pid_t pid;
  int pidfd;
  char identity_sha256[65];
};

struct sha256_state {
  uint32_t h[8];
  uint64_t bits;
  unsigned char block[64];
  size_t used;
};

static int read_unified_cgroup(pid_t pid, char *out, size_t capacity);
static const char *errno_name(int error);

static const uint32_t sha256_k[64] = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
    0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
    0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
    0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
    0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
    0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2};

static void fail(const char *stage, int error) {
  fprintf(stderr, "literal-pid-reuse-helper: stage=%s errno=%d (%s)\n",
          stage, error, errno_name(error));
  exit(1);
}

static int is_hex(const char *value, size_t length) {
  for (size_t i = 0; i < length; i++) {
    char c = value[i];
    if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))) return 0;
  }
  return 1;
}

static int safe_component(const char *value) {
  if (!value[0] || strcmp(value, ".") == 0 || strcmp(value, "..") == 0) return 0;
  for (const unsigned char *p = (const unsigned char *)value; *p; p++) {
    if (!((*p >= 'a' && *p <= 'z') || (*p >= 'A' && *p <= 'Z') ||
          (*p >= '0' && *p <= '9') || *p == '_' || *p == '-' || *p == '.'))
      return 0;
  }
  return 1;
}

static void skip_ws(struct parser *p) {
  while (p->cursor < p->length &&
         (p->text[p->cursor] == ' ' || p->text[p->cursor] == '\n' ||
          p->text[p->cursor] == '\r' || p->text[p->cursor] == '\t'))
    p->cursor++;
}

static size_t add_token(struct parser *p, enum token_type type, size_t start,
                        int parent) {
  if (p->count >= MAX_TOKENS) fail("json-token-limit", E2BIG);
  size_t index = p->count++;
  p->tokens[index] = (struct token){.type = type,
                                    .start = start,
                                    .end = start,
                                    .next = index + 1,
                                    .parent = parent};
  return index;
}

static size_t parse_string(struct parser *p, int parent) {
  if (p->cursor >= p->length || p->text[p->cursor] != '"')
    fail("json-string", EINVAL);
  p->cursor++;
  size_t start = p->cursor;
  size_t index = add_token(p, TOK_STRING, start, parent);
  while (p->cursor < p->length && p->text[p->cursor] != '"') {
    unsigned char c = (unsigned char)p->text[p->cursor];
    if (c == '\\' || c < 0x20) fail("json-string-escape", EINVAL);
    p->cursor++;
  }
  if (p->cursor >= p->length) fail("json-string-end", EINVAL);
  p->tokens[index].end = p->cursor;
  p->cursor++;
  p->tokens[index].next = p->count;
  return index;
}

static size_t parse_value(struct parser *p, int parent, unsigned depth) {
  if (depth > 16) fail("json-depth", E2BIG);
  skip_ws(p);
  if (p->cursor >= p->length) fail("json-value", EINVAL);
  char c = p->text[p->cursor];
  if (c == '"') return parse_string(p, parent);
  if (c == '{') {
    size_t index = add_token(p, TOK_OBJECT, p->cursor, parent);
    p->cursor++;
    skip_ws(p);
    if (p->cursor < p->length && p->text[p->cursor] == '}') {
      p->cursor++;
      p->tokens[index].end = p->cursor;
      p->tokens[index].next = p->count;
      return index;
    }
    for (;;) {
      skip_ws(p);
      size_t key = parse_string(p, (int)index);
      (void)key;
      skip_ws(p);
      if (p->cursor >= p->length || p->text[p->cursor++] != ':')
        fail("json-colon", EINVAL);
      parse_value(p, (int)index, depth + 1);
      skip_ws(p);
      if (p->cursor >= p->length) fail("json-object-end", EINVAL);
      char separator = p->text[p->cursor++];
      if (separator == '}') break;
      if (separator != ',') fail("json-object-separator", EINVAL);
    }
    p->tokens[index].end = p->cursor;
    p->tokens[index].next = p->count;
    return index;
  }
  if (c == '[' || c == ']' || c == '}') fail("json-array-unsupported", EINVAL);
  size_t start = p->cursor;
  while (p->cursor < p->length && p->text[p->cursor] != ',' &&
         p->text[p->cursor] != '}' && p->text[p->cursor] != ']' &&
         p->text[p->cursor] != ' ' && p->text[p->cursor] != '\n' &&
         p->text[p->cursor] != '\r' && p->text[p->cursor] != '\t')
    p->cursor++;
  if (p->cursor == start) fail("json-primitive", EINVAL);
  size_t index = add_token(p, TOK_PRIMITIVE, start, parent);
  p->tokens[index].end = p->cursor;
  p->tokens[index].next = p->count;
  return index;
}

static size_t parse_json(struct parser *p) {
  skip_ws(p);
  size_t root = parse_value(p, -1, 0);
  skip_ws(p);
  if (p->cursor != p->length || p->tokens[root].type != TOK_OBJECT)
    fail("json-trailing-data", EINVAL);
  return root;
}

static int token_equals(const struct parser *p, size_t index, const char *value) {
  const struct token *t = &p->tokens[index];
  size_t length = strlen(value);
  return t->type == TOK_STRING && t->end - t->start == length &&
         memcmp(p->text + t->start, value, length) == 0;
}

static size_t object_field(const struct parser *p, size_t object,
                           const char *field) {
  if (object >= p->count || p->tokens[object].type != TOK_OBJECT)
    fail("json-object-required", EINVAL);
  size_t found = SIZE_MAX;
  for (size_t i = object + 1; i < p->tokens[object].next;) {
    size_t key = i;
    if (key + 1 >= p->tokens[object].next || p->tokens[key].parent != (int)object)
      fail("json-object-pair", EINVAL);
    size_t value = key + 1;
    if (token_equals(p, key, field)) {
      if (found != SIZE_MAX) fail("json-duplicate-key", EINVAL);
      found = value;
    }
    i = p->tokens[value].next;
  }
  if (found == SIZE_MAX) fail("json-required-field", EINVAL);
  return found;
}

static size_t optional_object_field(const struct parser *p, size_t object,
                                    const char *field) {
  if (object >= p->count || p->tokens[object].type != TOK_OBJECT)
    fail("json-object-required", EINVAL);
  size_t found = SIZE_MAX;
  for (size_t i = object + 1; i < p->tokens[object].next;) {
    size_t key = i;
    if (key + 1 >= p->tokens[object].next || p->tokens[key].parent != (int)object)
      fail("json-object-pair", EINVAL);
    size_t value = key + 1;
    if (token_equals(p, key, field)) {
      if (found != SIZE_MAX) fail("json-duplicate-key", EINVAL);
      found = value;
    }
    i = p->tokens[value].next;
  }
  return found;
}

static void copy_string(const struct parser *p, size_t token, char *out,
                        size_t capacity, const char *label) {
  const struct token *t = &p->tokens[token];
  size_t length = t->end - t->start;
  if (t->type != TOK_STRING || length == 0 || length >= capacity)
    fail(label, EINVAL);
  memcpy(out, p->text + t->start, length);
  out[length] = '\0';
}

static uint64_t token_uint(const struct parser *p, size_t token,
                           const char *label) {
  const struct token *t = &p->tokens[token];
  if (t->type != TOK_PRIMITIVE || t->end == t->start ||
      (t->end - t->start > 1 && p->text[t->start] == '0'))
    fail(label, EINVAL);
  uint64_t value = 0;
  for (size_t i = t->start; i < t->end; i++) {
    char c = p->text[i];
    if (c < '0' || c > '9' || value > (UINT64_MAX - (uint64_t)(c - '0')) / 10)
      fail(label, EINVAL);
    value = value * 10 + (uint64_t)(c - '0');
  }
  return value;
}

static void read_json_at(int dirfd, const char *name, uid_t owner, gid_t group,
                         char *buffer, size_t capacity, size_t *length_out) {
  int fd = openat(dirfd, name, O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_NONBLOCK);
  if (fd < 0) fail("request-open", errno);
  struct stat st;
  if (fstat(fd, &st) != 0) fail("request-stat", errno);
  if (!S_ISREG(st.st_mode) || st.st_uid != owner || st.st_gid != group ||
      (st.st_mode & 07777) != 0600 || st.st_nlink != 1 || st.st_size <= 0 ||
      (uint64_t)st.st_size >= capacity || (uint64_t)st.st_size > MAX_JSON)
    fail("request-file-policy", EINVAL);
  size_t length = 0;
  while (length < (size_t)st.st_size) {
    ssize_t n = read(fd, buffer + length, (size_t)st.st_size - length);
    if (n < 0 && errno == EINTR) continue;
    if (n <= 0) fail("request-read", n == 0 ? EIO : errno);
    length += (size_t)n;
  }
  struct stat after;
  if (fstat(fd, &after) != 0 || after.st_size != st.st_size ||
      after.st_ino != st.st_ino || after.st_dev != st.st_dev)
    fail("request-changed-during-read", EAGAIN);
  close(fd);
  buffer[length] = '\0';
  *length_out = length;
}

static struct request parse_create_request(const char *json, size_t length) {
  struct parser p = {.text = json, .length = length};
  size_t root = parse_json(&p);
  struct request r = {0};
  if (token_uint(&p, object_field(&p, root, "version"), "request-version") != 1 ||
      !token_equals(&p, object_field(&p, root, "op"), "create") ||
      !token_equals(&p, object_field(&p, root, "case"), "literal_pid_reuse"))
    fail("request-contract", EINVAL);
  size_t run = object_field(&p, root, "run_key");
  size_t fence = object_field(&p, root, "fence");
  size_t leaf = object_field(&p, root, "leaf");
  copy_string(&p, object_field(&p, run, "run_id"), r.run_id, sizeof(r.run_id), "run-id");
  r.generation = token_uint(&p, object_field(&p, run, "generation"), "run-generation");
  char fence_run_id[MAX_TEXT];
  copy_string(&p, object_field(&p, fence, "runtime_id"), fence_run_id,
              sizeof(fence_run_id), "fence-runtime-id");
  if (strlen(fence_run_id) != 36 || r.generation == 0) fail("run-fence", EINVAL);
  r.runtime_epoch = token_uint(&p, object_field(&p, fence, "runtime_epoch"), "runtime-epoch");
  uint64_t fence_generation = token_uint(&p, object_field(&p, fence, "generation"), "fence-generation");
  copy_string(&p, object_field(&p, fence, "claim_id"), r.claim_id, sizeof(r.claim_id), "claim-id");
  if (fence_generation != r.generation || r.runtime_epoch == 0 ||
      strlen(r.claim_id) != 36)
    fail("run-fence-binding", EINVAL);
  uint64_t target = token_uint(&p, object_field(&p, root, "dead_target_pid"), "dead-target-pid");
  if (target < 2 || target > INT32_MAX) fail("dead-target-pid", EINVAL);
  r.dead_target_pid = (pid_t)target;
  copy_string(&p, object_field(&p, root, "old_identity_sha256"), r.old_identity_sha256,
              sizeof(r.old_identity_sha256), "old-identity-sha256");
  if (strlen(r.old_identity_sha256) != 64 || !is_hex(r.old_identity_sha256, 64))
    fail("old-identity-sha256", EINVAL);
  copy_string(&p, object_field(&p, root, "boot_id"), r.boot_id, sizeof(r.boot_id), "boot-id");
  uint64_t nsdev = token_uint(&p, object_field(&p, root, "pid_namespace_device"), "pid-namespace-device");
  uint64_t nsino = token_uint(&p, object_field(&p, root, "pid_namespace_inode"), "pid-namespace-inode");
  r.pid_namespace_device = nsdev;
  r.pid_namespace_inode = nsino;
  r.leaf_mount_id = token_uint(&p, object_field(&p, leaf, "mount_id"), "leaf-mount-id");
  r.leaf_parent_device = token_uint(&p, object_field(&p, leaf, "parent_device"), "leaf-parent-device");
  r.leaf_parent_inode = token_uint(&p, object_field(&p, leaf, "parent_inode"), "leaf-parent-inode");
  copy_string(&p, object_field(&p, leaf, "parent_path"), r.leaf_parent_path,
              sizeof(r.leaf_parent_path), "leaf-parent-path");
  copy_string(&p, object_field(&p, leaf, "name"), r.leaf_name, sizeof(r.leaf_name), "leaf-name");
  r.leaf_device = token_uint(&p, object_field(&p, leaf, "device"), "leaf-device");
  r.leaf_inode = token_uint(&p, object_field(&p, leaf, "inode"), "leaf-inode");
  if (!safe_component(r.leaf_name) || r.leaf_mount_id == 0 || r.leaf_parent_inode == 0 ||
      r.leaf_inode == 0 || r.leaf_device == 0 || r.leaf_parent_device == 0 ||
      r.pid_namespace_inode == 0)
    fail("leaf-identity", EINVAL);
  return r;
}

static struct cleanup_request parse_cleanup_request(const char *json, size_t length) {
  struct parser p = {.text = json, .length = length};
  size_t root = parse_json(&p);
  struct cleanup_request r = {0};
  if (token_uint(&p, object_field(&p, root, "version"), "cleanup-version") != 1 ||
      !token_equals(&p, object_field(&p, root, "op"), "kill_reap") ||
      !token_equals(&p, object_field(&p, root, "case"), "literal_pid_reuse"))
    fail("cleanup-contract", EINVAL);
  size_t run = object_field(&p, root, "run_key");
  copy_string(&p, object_field(&p, run, "run_id"), r.run_id, sizeof(r.run_id), "cleanup-run-id");
  r.generation = token_uint(&p, object_field(&p, run, "generation"), "cleanup-generation");
  size_t pid_field = optional_object_field(&p, root, "replacement_pid");
  size_t hash_field = optional_object_field(&p, root, "replacement_identity_sha256");
  if (pid_field != SIZE_MAX) {
    uint64_t pid = token_uint(&p, pid_field, "cleanup-pid");
    if (pid < 2 || pid > INT32_MAX) fail("cleanup-pid", EINVAL);
    r.replacement_pid = (pid_t)pid;
  }
  if (hash_field != SIZE_MAX) {
    copy_string(&p, hash_field, r.replacement_identity_sha256,
                sizeof(r.replacement_identity_sha256), "cleanup-identity-sha256");
    if (strlen(r.replacement_identity_sha256) != 64 ||
        !is_hex(r.replacement_identity_sha256, 64))
      fail("cleanup-identity-sha256", EINVAL);
  }
  if (r.generation == 0 ||
      ((r.replacement_pid == 0) != (r.replacement_identity_sha256[0] == '\0')))
    fail("cleanup-binding", EINVAL);
  return r;
}

static struct cleanup_acknowledgement parse_cleanup_acknowledgement(
    const char *json, size_t length) {
  struct parser p = {.text = json, .length = length};
  size_t root = parse_json(&p);
  struct cleanup_acknowledgement ack = {0};
  if (token_uint(&p, object_field(&p, root, "version"), "cleanup-ack-version") != 1 ||
      !token_equals(&p, object_field(&p, root, "op"), "ack_cleanup") ||
      !token_equals(&p, object_field(&p, root, "case"), "literal_pid_reuse") ||
      !token_equals(&p, object_field(&p, root, "status"), "acknowledged"))
    fail("cleanup-ack-contract", EINVAL);
  size_t run = object_field(&p, root, "run_key");
  copy_string(&p, object_field(&p, run, "run_id"), ack.run_id,
              sizeof(ack.run_id), "cleanup-ack-run-id");
  ack.generation = token_uint(&p, object_field(&p, run, "generation"),
                              "cleanup-ack-generation");
  if (ack.generation == 0) fail("cleanup-ack-run-key", EINVAL);
  return ack;
}

static uint32_t rotate_right(uint32_t x, unsigned n) { return (x >> n) | (x << (32 - n)); }

static void sha256_transform(struct sha256_state *s, const unsigned char *block) {
  uint32_t w[64];
  for (int i = 0; i < 16; i++)
    w[i] = ((uint32_t)block[i * 4] << 24) | ((uint32_t)block[i * 4 + 1] << 16) |
           ((uint32_t)block[i * 4 + 2] << 8) | block[i * 4 + 3];
  for (int i = 16; i < 64; i++) {
    uint32_t x = w[i - 15], y = w[i - 2];
    uint32_t a = rotate_right(x, 7) ^ rotate_right(x, 18) ^ (x >> 3);
    uint32_t b = rotate_right(y, 17) ^ rotate_right(y, 19) ^ (y >> 10);
    w[i] = w[i - 16] + a + w[i - 7] + b;
  }
  uint32_t a = s->h[0], b = s->h[1], c = s->h[2], d = s->h[3];
  uint32_t e = s->h[4], f = s->h[5], g = s->h[6], h = s->h[7];
  for (int i = 0; i < 64; i++) {
    uint32_t s1 = rotate_right(e, 6) ^ rotate_right(e, 11) ^ rotate_right(e, 25);
    uint32_t ch = (e & f) ^ (~e & g);
    uint32_t t1 = h + s1 + ch + sha256_k[i] + w[i];
    uint32_t s0 = rotate_right(a, 2) ^ rotate_right(a, 13) ^ rotate_right(a, 22);
    uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
    uint32_t t2 = s0 + maj;
    h = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
  }
  s->h[0] += a; s->h[1] += b; s->h[2] += c; s->h[3] += d;
  s->h[4] += e; s->h[5] += f; s->h[6] += g; s->h[7] += h;
}

static void sha256_init(struct sha256_state *s) {
  *s = (struct sha256_state){.h = {0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
                                    0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}};
}

static void sha256_update(struct sha256_state *s, const void *data, size_t length) {
  const unsigned char *p = data;
  s->bits += (uint64_t)length * 8;
  while (length) {
    size_t take = 64 - s->used;
    if (take > length) take = length;
    memcpy(s->block + s->used, p, take);
    s->used += take; p += take; length -= take;
    if (s->used == 64) { sha256_transform(s, s->block); s->used = 0; }
  }
}

static void sha256_final(struct sha256_state *s, unsigned char digest[32]) {
  uint64_t bits = s->bits;
  unsigned char one = 0x80;
  sha256_update(s, &one, 1);
  unsigned char zero = 0;
  while (s->used != 56) sha256_update(s, &zero, 1);
  unsigned char length[8];
  for (int i = 0; i < 8; i++) length[7 - i] = (unsigned char)(bits >> (i * 8));
  sha256_update(s, length, sizeof(length));
  for (int i = 0; i < 8; i++) {
    digest[i * 4] = (unsigned char)(s->h[i] >> 24);
    digest[i * 4 + 1] = (unsigned char)(s->h[i] >> 16);
    digest[i * 4 + 2] = (unsigned char)(s->h[i] >> 8);
    digest[i * 4 + 3] = (unsigned char)s->h[i];
  }
}

static void sha256_hex(const char *text, char out[65]) {
  static const char hex[] = "0123456789abcdef";
  struct sha256_state s;
  unsigned char digest[32];
  sha256_init(&s);
  sha256_update(&s, text, strlen(text));
  sha256_final(&s, digest);
  for (int i = 0; i < 32; i++) { out[i * 2] = hex[digest[i] >> 4]; out[i * 2 + 1] = hex[digest[i] & 15]; }
  out[64] = '\0';
}

static void read_boot_id(char out[37]) {
  int fd = open("/proc/sys/kernel/random/boot_id", O_RDONLY | O_CLOEXEC);
  if (fd < 0) fail("boot-id-open", errno);
  ssize_t n = read(fd, out, 36);
  close(fd);
  if (n != 36) fail("boot-id-read", n < 0 ? errno : EINVAL);
  out[36] = '\0';
  if (out[8] != '-' || out[13] != '-' || out[18] != '-' || out[23] != '-')
    fail("boot-id-format", EINVAL);
  for (int i = 0; i < 36; i++)
    if (i != 8 && i != 13 && i != 18 && i != 23 &&
        !((out[i] >= '0' && out[i] <= '9') || (out[i] >= 'a' && out[i] <= 'f')))
      fail("boot-id-format", EINVAL);
}

static uint64_t read_starttime(pid_t pid) {
  char path[64], buffer[4096];
  snprintf(path, sizeof(path), "/proc/%d/stat", pid);
  int fd = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) fail("process-stat-open", errno);
  ssize_t n = read(fd, buffer, sizeof(buffer) - 1);
  int saved = errno;
  close(fd);
  if (n <= 0) fail("process-stat-read", n < 0 ? saved : EIO);
  buffer[n] = '\0';
  char *end_comm = strrchr(buffer, ')');
  if (!end_comm) fail("process-stat-comm", EINVAL);
  char *cursor = end_comm + 1;
  for (int field = 3; field <= 22; field++) {
    while (*cursor == ' ') cursor++;
    if (!*cursor) fail("process-stat-field", EINVAL);
    char *end = cursor;
    while (*end && *end != ' ' && *end != '\n') end++;
    if (field == 22) {
      char saved_char = *end;
      *end = '\0';
      char *number_end = NULL;
      errno = 0;
      unsigned long long value = strtoull(cursor, &number_end, 10);
      int invalid = errno != 0 || number_end == cursor || number_end != end || value == 0;
      *end = saved_char;
      if (invalid) fail("process-starttime", EINVAL);
      return (uint64_t)value;
    }
    cursor = end;
  }
  fail("process-starttime", EINVAL);
  return 0;
}

static uint64_t pid_namespace_inode(pid_t pid, uint64_t *device) {
  char path[64];
  snprintf(path, sizeof(path), "/proc/%s/ns/pid", pid > 0 ? "" : "self");
  if (pid > 0) snprintf(path, sizeof(path), "/proc/%d/ns/pid", pid);
  struct stat st;
  if (stat(path, &st) != 0) fail("pid-namespace-stat", errno);
  if (device) *device = (uint64_t)st.st_dev;
  return (uint64_t)st.st_ino;
}

static void get_capabilities(uint64_t *effective, int *seccomp) {
  struct __user_cap_header_struct header = {_LINUX_CAPABILITY_VERSION_3, 0};
  struct __user_cap_data_struct data[2] = {{0}};
  if (syscall(SYS_capget, &header, data) != 0) fail("capget", errno);
  *effective = ((uint64_t)data[1].effective << 32) | data[0].effective;
  int mode = prctl(PR_GET_SECCOMP, 0, 0, 0, 0);
  if (mode < 0) fail("seccomp-query", errno);
  *seccomp = mode;
}

static int get_pidfd(pid_t pid) {
#ifdef SYS_pidfd_open
  return (int)syscall(SYS_pidfd_open, pid, 0);
#else
  errno = ENOSYS;
  return -1;
#endif
}

static int pidfd_send_signal(int pidfd, int signal_number) {
#ifdef SYS_pidfd_send_signal
  return (int)syscall(SYS_pidfd_send_signal, pidfd, signal_number, NULL, 0);
#else
  errno = ENOSYS;
  return -1;
#endif
}

static pid_t clone_at_pid(pid_t wanted_pid, int *pidfd_out) {
#ifdef SYS_clone3
  uint64_t set_tid = (uint64_t)wanted_pid;
  int pidfd = -1;
  struct clone_args args = {0};
  args.flags = CLONE_PIDFD;
  args.pidfd = (uint64_t)(uintptr_t)&pidfd;
  args.exit_signal = SIGCHLD;
  args.set_tid = (uint64_t)(uintptr_t)&set_tid;
  args.set_tid_size = 1;
  long result = syscall(SYS_clone3, &args, sizeof(args));
  if (result == 0) {
    pid_t parent = getppid();
    if (parent <= 1 || prctl(PR_SET_PDEATHSIG, SIGKILL, 0, 0, 0) != 0 ||
        getppid() != parent)
      _exit(111);
    for (;;) pause();
  }
  if (result < 0) return -1;
  if (result != wanted_pid || pidfd < 0) {
    if (pidfd >= 0) close(pidfd);
    errno = EPROTO;
    return -1;
  }
  *pidfd_out = pidfd;
  return (pid_t)result;
#else
  (void)wanted_pid;
  (void)pidfd_out;
  errno = ENOSYS;
  return -1;
#endif
}

static int pidfd_alive(int pidfd) {
  struct pollfd pfd = {.fd = pidfd, .events = POLLIN};
  int result;
  do { result = poll(&pfd, 1, 0); } while (result < 0 && errno == EINTR);
  if (result < 0) return -1;
  return result == 0;
}

static void wait_for_exit_and_reap(pid_t pid, int pidfd, const char *stage) {
  if (pidfd_send_signal(pidfd, SIGKILL) != 0 && errno != ESRCH) fail(stage, errno);
  int status = 0;
  pid_t waited;
  do { waited = waitpid(pid, &status, 0); } while (waited < 0 && errno == EINTR);
  if (waited != pid) fail(stage, waited < 0 ? errno : ECHILD);
  if (!WIFSIGNALED(status) && !WIFEXITED(status)) fail(stage, ECHILD);
  if (pidfd_alive(pidfd) != 0) fail(stage, EBUSY);
}

static void probe_literal_reuse(uint64_t host_pidns_inode, const char *owner_manager) {
  if (geteuid() != 0) fail("probe-requires-root", EPERM);
  uint64_t nsdev = 0;
  uint64_t nsino = pid_namespace_inode(0, &nsdev);
  if (nsino == host_pidns_inode) fail("pid-namespace-not-isolated", EXDEV);
  uint64_t caps = 0;
  int seccomp = -1;
  get_capabilities(&caps, &seccomp);
  int has_checkpoint = (caps & (1ULL << CAP_CHECKPOINT_RESTORE)) != 0;
  int has_sys_admin = (caps & (1ULL << CAP_SYS_ADMIN)) != 0;
  pid_t sacrificial = fork();
  if (sacrificial < 0) fail("probe-fork", errno);
  if (sacrificial == 0) _exit(0);
  int status = 0;
  pid_t waited;
  do { waited = waitpid(sacrificial, &status, 0); } while (waited < 0 && errno == EINTR);
  if (waited != sacrificial || !WIFEXITED(status)) fail("probe-reap-original", waited < 0 ? errno : ECHILD);
  char path[64];
  snprintf(path, sizeof(path), "/proc/%d", sacrificial);
  struct stat gone;
  if (lstat(path, &gone) == 0 || errno != ENOENT) fail("probe-pid-not-absent", EEXIST);
  int pidfd = -1;
  pid_t replacement = clone_at_pid(sacrificial, &pidfd);
  if (replacement < 0) fail("probe-clone3-set-tid", errno);
  if (replacement != sacrificial || pidfd_alive(pidfd) != 1) fail("probe-pidfd-alive", EPROTO);
  (void)read_starttime(replacement);
  char cgroup[MAX_PATH];
  if (read_unified_cgroup(getpid(), cgroup, sizeof(cgroup)) != 0)
    fail("probe-cgroup", errno ? errno : EINVAL);
  if (strncmp(cgroup, owner_manager, strlen(owner_manager)) == 0 &&
      (cgroup[strlen(owner_manager)] == '\0' || cgroup[strlen(owner_manager)] == '/'))
    fail("probe-inside-owner-manager", EXDEV);
  wait_for_exit_and_reap(replacement, pidfd, "probe-cleanup");
  close(pidfd);
  printf("probe=passed host_pid_namespace_inode=%llu guest_pid_namespace_device=%llu guest_pid_namespace_inode=%llu cap_sys_admin=%d cap_checkpoint_restore=%d seccomp=%d reused_pid=%d\n",
         (unsigned long long)host_pidns_inode, (unsigned long long)nsdev,
         (unsigned long long)nsino, has_sys_admin, has_checkpoint, seccomp,
         (int)sacrificial);
}

static int write_all(int fd, const char *data, size_t length) {
  size_t offset = 0;
  while (offset < length) {
    ssize_t n = write(fd, data + offset, length - offset);
    if (n < 0 && errno == EINTR) continue;
    if (n <= 0) return -1;
    offset += (size_t)n;
  }
  return 0;
}

static void random_bytes(void *buffer, size_t length) {
  unsigned char *cursor = buffer;
  while (length > 0) {
    ssize_t n = getrandom(cursor, length, 0);
    if (n < 0 && errno == EINTR) continue;
    if (n <= 0) fail("temporary-name-random", n == 0 ? EIO : errno);
    cursor += (size_t)n;
    length -= (size_t)n;
  }
}

static void write_exclusive_at(int dirfd, const char *name, const char *contents,
                               size_t length, uid_t owner, gid_t group,
                               mode_t mode) {
  unsigned char random[16];
  char temporary[48];
  random_bytes(random, sizeof(random));
  static const char hex[] = "0123456789abcdef";
  static const char prefix[] = ".pid-reuse-";
  memcpy(temporary, prefix, sizeof(prefix) - 1);
  size_t temp_length = sizeof(prefix) - 1;
  for (size_t i = 0; i < sizeof(random); i++) {
    temporary[temp_length++] = hex[random[i] >> 4];
    temporary[temp_length++] = hex[random[i] & 15];
  }
  memcpy(temporary + temp_length, ".tmp", 5);
  int fd = openat(dirfd, temporary,
                  O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
  if (fd < 0) fail("result-create-exclusive-temporary", errno);
  if (fchown(fd, owner, group) != 0 || write_all(fd, contents, length) != 0 ||
      fsync(fd) != 0 || fchmod(fd, mode) != 0 || fsync(fd) != 0) {
    int saved = errno ? errno : EIO;
    (void)unlinkat(dirfd, temporary, 0);
    close(fd);
    fail("result-write", saved);
  }
  struct stat expected;
  if (fstat(fd, &expected) != 0) {
    int saved = errno;
    (void)unlinkat(dirfd, temporary, 0);
    close(fd);
    fail("result-temporary-policy", saved);
  }
  if (!S_ISREG(expected.st_mode) || expected.st_uid != owner ||
      expected.st_gid != group || (expected.st_mode & 07777) != mode ||
      expected.st_nlink != 1 || expected.st_size != (off_t)length) {
    (void)unlinkat(dirfd, temporary, 0);
    close(fd);
    fail("result-temporary-policy", EACCES);
  }
#ifdef SYS_renameat2
  if (syscall(SYS_renameat2, dirfd, temporary, dirfd, name, RENAME_NOREPLACE) != 0) {
    int saved = errno;
    (void)unlinkat(dirfd, temporary, 0);
    close(fd);
    fail("result-publish-no-replace", saved);
  }
#else
  (void)unlinkat(dirfd, temporary, 0);
  close(fd);
  fail("result-publish-no-replace", ENOSYS);
#endif
  struct stat published;
  if (fstatat(dirfd, name, &published, AT_SYMLINK_NOFOLLOW) != 0) {
    int saved = errno;
    close(fd);
    fail("result-published-policy", saved);
  }
  if (!S_ISREG(published.st_mode) || published.st_uid != owner ||
      published.st_gid != group || (published.st_mode & 07777) != mode ||
      published.st_nlink != 1 || published.st_size != (off_t)length ||
      published.st_dev != expected.st_dev || published.st_ino != expected.st_ino) {
    close(fd);
    fail("result-published-policy", EACCES);
  }
  if (close(fd) != 0) fail("result-close", errno);
  if (fsync(dirfd) != 0) fail("result-directory-sync", errno);
}

static int open_exchange_directory(const char *path, gid_t a_gid) {
  int fd = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) fail("exchange-directory-open", errno);
  struct stat st;
  if (fstat(fd, &st) != 0) fail("exchange-directory-stat", errno);
  if (!S_ISDIR(st.st_mode) || st.st_uid != 0 || st.st_gid != a_gid ||
      (st.st_mode & 07777) != 02730 || st.st_nlink < 2)
    fail("exchange-directory-policy", EACCES);
  return fd;
}

static int parse_cgroup_text(const char *text, char *out, size_t capacity) {
  const char *line = text;
  while (*line) {
    const char *end = strchr(line, '\n');
    size_t length = end ? (size_t)(end - line) : strlen(line);
    if (length >= 3 && line[0] == '0' && line[1] == ':' && line[2] == ':') {
      size_t path_length = length - 3;
      if (path_length == 0 || path_length >= capacity || line[3] != '/') return -1;
      memcpy(out, line + 3, path_length);
      out[path_length] = '\0';
      return 0;
    }
    if (!end) break;
    line = end + 1;
  }
  return -1;
}

static int read_unified_cgroup(pid_t pid, char *out, size_t capacity) {
  char path[64], buffer[4096];
  snprintf(path, sizeof(path), "/proc/%d/cgroup", pid);
  int fd = open(path, O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) return -1;
  ssize_t n = read(fd, buffer, sizeof(buffer) - 1);
  int saved = errno;
  close(fd);
  if (n <= 0) { errno = n < 0 ? saved : EIO; return -1; }
  buffer[n] = '\0';
  if (parse_cgroup_text(buffer, out, capacity) != 0) { errno = EINVAL; return -1; }
  return 0;
}

static int statx_path(const char *path, struct statx *stx) {
#ifdef SYS_statx
  memset(stx, 0, sizeof(*stx));
  return (int)syscall(SYS_statx, AT_FDCWD, path, AT_SYMLINK_NOFOLLOW,
                      STATX_TYPE | STATX_INO | STATX_MNT_ID, stx);
#else
  (void)path; (void)stx; errno = ENOSYS; return -1;
#endif
}

static uint64_t statx_dev(const struct statx *stx) {
  return (uint64_t)makedev(stx->stx_dev_major, stx->stx_dev_minor);
}

static int starts_component(const char *path, const char *prefix) {
  size_t length = strlen(prefix);
  return strncmp(path, prefix, length) == 0 &&
         (path[length] == '\0' || path[length] == '/');
}

static int identity_hash_for_pid(pid_t pid, char out[65], char boot_id[37],
                                 uint64_t *nsdev, uint64_t *nsino) {
  read_boot_id(boot_id);
  *nsino = pid_namespace_inode(pid, nsdev);
  uint64_t start = read_starttime(pid);
  char identity[256];
  int n = snprintf(identity, sizeof(identity), "linux:v2:%s:%llu:%d:%llu", boot_id,
                   (unsigned long long)*nsino, (int)pid,
                   (unsigned long long)start);
  if (n <= 0 || (size_t)n >= sizeof(identity)) { errno = EOVERFLOW; return -1; }
  sha256_hex(identity, out);
  return 0;
}

static void json_escape(const char *source, char *dest, size_t capacity) {
  size_t out = 0;
  for (const unsigned char *p = (const unsigned char *)source; *p; p++) {
    if (*p == '"' || *p == '\\') {
      if (out + 2 >= capacity) fail("json-output-size", E2BIG);
      dest[out++] = '\\'; dest[out++] = (char)*p;
    } else if (*p < 0x20) {
      if (out + 6 >= capacity) fail("json-output-size", E2BIG);
      int n = snprintf(dest + out, capacity - out, "\\u%04x", *p);
      if (n != 6) fail("json-output-size", EIO);
      out += 6;
    } else {
      if (out + 1 >= capacity) fail("json-output-size", E2BIG);
      dest[out++] = (char)*p;
    }
  }
  dest[out] = '\0';
}

static void poll_request(int dirfd) {
  struct timespec delay = {.tv_sec = 0, .tv_nsec = 100000000};
  for (;;) {
    struct stat st;
    if (fstatat(dirfd, "request.json", &st, AT_SYMLINK_NOFOLLOW) == 0) return;
    if (errno != ENOENT) fail("request-watch", errno);
    nanosleep(&delay, NULL);
  }
}

static const char *errno_name(int error) {
  switch (error) {
    case EACCES: return "EACCES";
    case EALREADY: return "EALREADY";
    case EBUSY: return "EBUSY";
    case EEXIST: return "EEXIST";
    case EINVAL: return "EINVAL";
    case EIO: return "EIO";
    case ENOENT: return "ENOENT";
    case ENOSYS: return "ENOSYS";
    case EPERM: return "EPERM";
    case EPROTO: return "EPROTO";
    case ESRCH: return "ESRCH";
    case ESTALE: return "ESTALE";
    case ETIMEDOUT: return "ETIMEDOUT";
    case EXDEV: return "EXDEV";
    case EOVERFLOW: return "EOVERFLOW";
    default: return "UNKNOWN";
  }
}

static void refuse_result(int dirfd, const struct request *r, gid_t owner_gid,
                          int error,
                          const char *stage, const char *refusal,
                          const char *boot, uint64_t nsdev, uint64_t nsino,
                          const char *service_cgroup) {
  char service[2 * MAX_PATH], json[4096];
  json_escape(service_cgroup ? service_cgroup : "", service, sizeof(service));
  int n = snprintf(json, sizeof(json),
      "{\"version\":1,\"op\":\"create\",\"case\":\"literal_pid_reuse\","
      "\"run_key\":{\"run_id\":\"%s\",\"generation\":%llu},"
      "\"status\":\"refused\",\"stage\":\"%s\",\"errno\":\"%s\","
      "\"refusal\":\"%s\",\"dead_target_pid\":%d,"
      "\"old_identity_sha256\":\"%s\",\"replacement_pid\":0,"
      "\"replacement_identity_sha256\":\"\","
      "\"boot_id\":\"%s\",\"pid_namespace_device\":%llu,"
      "\"pid_namespace_inode\":%llu,\"pidfd_verified\":false,"
      "\"pidfd_alive\":false,\"outside_original_leaf\":false,"
      "\"separate_service_cgroup\":false,\"service_cgroup_path\":\"%s\"}\n",
      r->run_id, (unsigned long long)r->generation, stage, errno_name(error),
      refusal, (int)r->dead_target_pid, r->old_identity_sha256,
      boot ? boot : "", (unsigned long long)nsdev, (unsigned long long)nsino, service);
  if (n < 0 || (size_t)n >= sizeof(json)) fail("refusal-json", EOVERFLOW);
  write_exclusive_at(dirfd, "response.json", json, (size_t)n, 0, owner_gid, 0440);
}

static int make_create_response(int dirfd, const struct request *r,
                                uint64_t host_pidns_inode,
                                const char *owner_manager,
                                gid_t owner_gid, struct live_child *child) {
  char boot[37] = "", replacement_hash[65] = "", cgroup[MAX_PATH] = "";
  uint64_t nsdev = 0, nsino = 0;
  if (geteuid() != 0) {
    refuse_result(dirfd, r, owner_gid, EPERM, "helper_identity", "watcher must run as root", boot, 0, 0, cgroup);
    return -1;
  }
  if (read_unified_cgroup(getpid(), cgroup, sizeof(cgroup)) != 0) {
    int error = errno ? errno : EINVAL;
    refuse_result(dirfd, r, owner_gid, error, "watcher_cgroup", "cannot read watcher unified cgroup", boot, 0, 0, "");
    return -1;
  }
  read_boot_id(boot);
  nsino = pid_namespace_inode(0, &nsdev);
  if (strcmp(boot, r->boot_id) != 0 || nsdev != r->pid_namespace_device ||
      nsino != r->pid_namespace_inode || nsino == host_pidns_inode) {
    refuse_result(dirfd, r, owner_gid, EXDEV, "pid_namespace_identity", "boot or PID namespace differs from the old target", boot, nsdev, nsino, cgroup);
    return -1;
  }
  if (starts_component(cgroup, owner_manager)) {
    refuse_result(dirfd, r, owner_gid, EXDEV, "service_cgroup", "watcher is inside the delegated owner subtree", boot, nsdev, nsino, cgroup);
    return -1;
  }
  uint64_t caps = 0;
  int seccomp = -1;
  get_capabilities(&caps, &seccomp);
  (void)seccomp;
  if ((caps & ((1ULL << CAP_CHECKPOINT_RESTORE) | (1ULL << CAP_SYS_ADMIN))) == 0) {
    refuse_result(dirfd, r, owner_gid, EPERM, "clone3_capability", "clone3 set_tid capability is unavailable", boot, nsdev, nsino, cgroup);
    return -1;
  }
  char parent[MAX_PATH];
  int n = snprintf(parent, sizeof(parent), "/sys/fs/cgroup%s", owner_manager);
  if (n < 0 || (size_t)n >= sizeof(parent)) fail("leaf-parent-path", EOVERFLOW);
  if (strcmp(r->leaf_parent_path, owner_manager) != 0) {
    refuse_result(dirfd, r, owner_gid, ESTALE, "leaf_parent_identity", "request names a different cgroup parent", boot, nsdev, nsino, cgroup);
    return -1;
  }
  struct statx parent_stat;
  if (statx_path(parent, &parent_stat) != 0) {
    int error = errno;
    refuse_result(dirfd, r, owner_gid, error, "leaf_parent_stat", "cannot stat the recorded cgroup parent", boot, nsdev, nsino, cgroup);
    return -1;
  }
  if (statx_dev(&parent_stat) != r->leaf_parent_device ||
      parent_stat.stx_ino != r->leaf_parent_inode ||
      parent_stat.stx_mnt_id != r->leaf_mount_id) {
    refuse_result(dirfd, r, owner_gid, ESTALE, "leaf_parent_identity", "cgroup parent mount/device/inode changed", boot, nsdev, nsino, cgroup);
    return -1;
  }
  int parentfd = open(parent, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
  if (parentfd < 0) {
    int error = errno;
    refuse_result(dirfd, r, owner_gid, error, "leaf_parent_open", "cannot pin the cgroup parent", boot, nsdev, nsino, cgroup);
    return -1;
  }
  struct stat old_leaf;
  if (fstatat(parentfd, r->leaf_name, &old_leaf, AT_SYMLINK_NOFOLLOW) == 0) {
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, EEXIST, "old_leaf_not_removed", "the old pinned cgroup leaf still exists", boot, nsdev, nsino, cgroup);
    return -1;
  }
  if (errno != ENOENT) {
    int error = errno;
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, error, "old_leaf_lookup", "cannot verify absence of the old pinned cgroup leaf", boot, nsdev, nsino, cgroup);
    return -1;
  }
  char old_proc[64];
  snprintf(old_proc, sizeof(old_proc), "/proc/%d", (int)r->dead_target_pid);
  struct stat old_process;
  if (lstat(old_proc, &old_process) == 0) {
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, EEXIST, "old_pid_not_absent", "the old numeric PID is still present", boot, nsdev, nsino, cgroup);
    return -1;
  }
  if (errno != ENOENT) {
    int error = errno;
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, error, "old_pid_lookup", "cannot verify absence of the old numeric PID", boot, nsdev, nsino, cgroup);
    return -1;
  }
  int dead_pidfd = get_pidfd(r->dead_target_pid);
  if (dead_pidfd >= 0) {
    close(dead_pidfd);
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, EEXIST, "old_pidfd_absent", "pidfd_open still resolves the old numeric PID", boot, nsdev, nsino, cgroup);
    return -1;
  }
  if (errno != ESRCH) {
    int error = errno;
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, error, "old_pidfd_absent", "pidfd_open did not prove the old PID absent", boot, nsdev, nsino, cgroup);
    return -1;
  }
  int helper_pidfd = get_pidfd(getpid());
  if (helper_pidfd < 0 || pidfd_alive(helper_pidfd) != 1) {
    int error = helper_pidfd < 0 ? errno : EPROTO;
    if (helper_pidfd >= 0) close(helper_pidfd);
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, error, "watcher_pidfd", "cannot verify the watcher process with a pidfd", boot, nsdev, nsino, cgroup);
    return -1;
  }
  int replacement_pidfd = -1;
  pid_t replacement = clone_at_pid(r->dead_target_pid, &replacement_pidfd);
  if (replacement < 0) {
    int error = errno;
    close(helper_pidfd);
    close(parentfd);
    refuse_result(dirfd, r, owner_gid, error, "clone3_set_tid", "kernel refused one exact clone3 set_tid attempt", boot, nsdev, nsino, cgroup);
    return -1;
  }
  int outside_leaf = 0;
  char replacement_cgroup[MAX_PATH] = "";
  char replacement_boot[37] = "";
  uint64_t replacement_nsdev = 0, replacement_nsino = 0;
  int alive = pidfd_alive(replacement_pidfd);
  if (alive == 1 && read_unified_cgroup(replacement, replacement_cgroup,
                                        sizeof(replacement_cgroup)) == 0 &&
      !starts_component(replacement_cgroup, parent + strlen("/sys/fs/cgroup")) &&
      identity_hash_for_pid(replacement, replacement_hash, replacement_boot,
                            &replacement_nsdev, &replacement_nsino) == 0 &&
      replacement_nsino == r->pid_namespace_inode &&
      replacement_nsdev == r->pid_namespace_device &&
      strcmp(replacement_boot, r->boot_id) == 0) {
    outside_leaf = 1;
  }
  close(parentfd);
  if (alive != 1 || !outside_leaf || pidfd_alive(helper_pidfd) != 1) {
    wait_for_exit_and_reap(replacement, replacement_pidfd, "refused-child-cleanup");
    close(replacement_pidfd);
    close(helper_pidfd);
    refuse_result(dirfd, r, owner_gid, EPROTO, "replacement_identity", "replacement failed exact pidfd/namespace/cgroup verification", boot, nsdev, nsino, cgroup);
    return -1;
  }
  char escaped_cgroup[2 * MAX_PATH], json[4096];
  json_escape(replacement_cgroup, escaped_cgroup, sizeof(escaped_cgroup));
  n = snprintf(json, sizeof(json),
      "{\"version\":1,\"op\":\"create\",\"case\":\"literal_pid_reuse\","
      "\"run_key\":{\"run_id\":\"%s\",\"generation\":%llu},"
      "\"status\":\"created\",\"stage\":\"clone3_set_tid\",\"errno\":\"0\","
      "\"refusal\":\"\",\"dead_target_pid\":%d,"
      "\"old_identity_sha256\":\"%s\","
      "\"replacement_pid\":%d,\"replacement_identity_sha256\":\"%s\","
      "\"boot_id\":\"%s\",\"pid_namespace_device\":%llu,"
      "\"pid_namespace_inode\":%llu,\"pidfd_verified\":true,"
      "\"pidfd_alive\":true,\"outside_original_leaf\":true,"
      "\"separate_service_cgroup\":true,\"service_cgroup_path\":\"%s\"}\n",
      r->run_id, (unsigned long long)r->generation, (int)r->dead_target_pid,
      r->old_identity_sha256,
      (int)replacement, replacement_hash, replacement_boot,
      (unsigned long long)replacement_nsdev, (unsigned long long)replacement_nsino,
      escaped_cgroup);
  if (n < 0 || (size_t)n >= sizeof(json)) {
    wait_for_exit_and_reap(replacement, replacement_pidfd, "response-overflow-cleanup");
    close(replacement_pidfd); close(helper_pidfd);
    fail("response-json", EOVERFLOW);
  }
  write_exclusive_at(dirfd, "response.json", json, (size_t)n, 0, owner_gid, 0440);
  child->pid = replacement;
  child->pidfd = replacement_pidfd;
  memcpy(child->identity_sha256, replacement_hash, sizeof(child->identity_sha256));
  close(helper_pidfd);
  return 0;
}

static int entry_exists(int dirfd, const char *name, const char *stage) {
  struct stat st;
  if (fstatat(dirfd, name, &st, AT_SYMLINK_NOFOLLOW) == 0) return 1;
  if (errno == ENOENT) return 0;
  fail(stage, errno);
  return 0;
}

static void fsync_exchange_directory(int dirfd, const char *stage) {
  if (fsync(dirfd) != 0) fail(stage, errno);
}

static void ensure_ready_file(int dirfd, gid_t owner_gid) {
  struct stat st;
  if (fstatat(dirfd, "ready", &st, AT_SYMLINK_NOFOLLOW) != 0) {
    if (errno != ENOENT) fail("ready-stat", errno);
    write_exclusive_at(dirfd, "ready", "ready\n", 6, 0, owner_gid, 0440);
    return;
  }
  if (!S_ISREG(st.st_mode) || st.st_uid != 0 || st.st_gid != owner_gid ||
      (st.st_mode & 07777) != 0440 || st.st_nlink != 1 || st.st_size != 6)
    fail("ready-policy", EACCES);
  int fd = openat(dirfd, "ready", O_RDONLY | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0) fail("ready-open", errno);
  char contents[7];
  ssize_t n = read(fd, contents, sizeof(contents));
  int saved = errno;
  close(fd);
  if (n != 6 || memcmp(contents, "ready\n", 6) != 0)
    fail("ready-content", n < 0 ? saved : EINVAL);
}

static void require_operation_files_absent(int dirfd) {
  static const char *const names[] = {
      "request.json", "response.json", "cleanup.json",
      "cleanup_response.json", "cleanup_ack.json"};
  for (size_t i = 0; i < sizeof(names) / sizeof(names[0]); i++) {
    if (entry_exists(dirfd, names[i], "stale-exchange-check"))
      fail("stale-exchange-entry", EEXIST);
  }
}

static int wait_for_exchange_file(int dirfd, const char *name,
                                  const struct timespec *deadline,
                                  const char *stage) {
  struct timespec delay = {.tv_sec = 0, .tv_nsec = 100000000};
  for (;;) {
    if (entry_exists(dirfd, name, stage)) return 1;
    if (deadline) {
      struct timespec now;
      if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) fail("monotonic-clock", errno);
      if (now.tv_sec > deadline->tv_sec ||
          (now.tv_sec == deadline->tv_sec && now.tv_nsec >= deadline->tv_nsec))
        return 0;
    }
    (void)nanosleep(&delay, NULL);
  }
}

static struct timespec deadline_after_seconds(time_t seconds) {
  struct timespec deadline;
  if (clock_gettime(CLOCK_MONOTONIC, &deadline) != 0) fail("monotonic-clock", errno);
  deadline.tv_sec += seconds;
  return deadline;
}

static void validate_exchange_file(int dirfd, const char *name, uid_t owner,
                                   gid_t group, mode_t mode) {
  struct stat st;
  if (fstatat(dirfd, name, &st, AT_SYMLINK_NOFOLLOW) != 0)
    fail("exchange-remove-stat", errno);
  if (!S_ISREG(st.st_mode) || st.st_uid != owner || st.st_gid != group ||
      (st.st_mode & 07777) != mode || st.st_nlink != 1)
    fail("exchange-remove-policy", EACCES);
}

static void remove_operation_files(int dirfd, uid_t owner_uid, gid_t owner_gid) {
  const struct {
    const char *name;
    uid_t owner;
    mode_t mode;
  } files[] = {
      {"request.json", owner_uid, 0600},
      {"response.json", 0, 0440},
      {"cleanup.json", owner_uid, 0600},
      {"cleanup_response.json", 0, 0440},
      {"cleanup_ack.json", owner_uid, 0600},
  };
  for (size_t i = 0; i < sizeof(files) / sizeof(files[0]); i++) {
    validate_exchange_file(dirfd, files[i].name, files[i].owner, owner_gid,
                           files[i].mode);
    if (unlinkat(dirfd, files[i].name, 0) != 0)
      fail("exchange-remove", errno);
  }
  fsync_exchange_directory(dirfd, "exchange-remove-sync");
}

static void write_cleanup_response(int dirfd, const struct request *request,
                                  const struct live_child *child,
                                  int created, gid_t owner_gid) {
  char json[1024];
  int replacement_pid = created ? (int)child->pid : 0;
  int n = snprintf(json, sizeof(json),
      "{\"version\":1,\"op\":\"kill_reap\",\"case\":\"literal_pid_reuse\","
      "\"run_key\":{\"run_id\":\"%s\",\"generation\":%llu},"
      "\"replacement_pid\":%d,\"replacement_identity_sha256\":\"%s\","
      "\"status\":\"%s\",\"pidfd_verified\":%s}\n",
      request->run_id, (unsigned long long)request->generation, replacement_pid,
      created ? child->identity_sha256 : "", created ? "reaped" : "not_created",
      created ? "true" : "false");
  if (n < 0 || (size_t)n >= sizeof(json)) fail("cleanup-response-json", EOVERFLOW);
  write_exclusive_at(dirfd, "cleanup_response.json", json, (size_t)n,
                     0, owner_gid, 0440);
}

static void wait_for_cleanup_acknowledgement(int dirfd,
                                             const struct request *request,
                                             uid_t owner_uid, gid_t owner_gid) {
  (void)wait_for_exchange_file(dirfd, "cleanup_ack.json", NULL,
                               "cleanup-ack-watch");
  fsync_exchange_directory(dirfd, "cleanup-ack-observed-sync");
  char buffer[MAX_JSON + 1];
  size_t length = 0;
  read_json_at(dirfd, "cleanup_ack.json", owner_uid, owner_gid,
               buffer, sizeof(buffer), &length);
  struct cleanup_acknowledgement ack = parse_cleanup_acknowledgement(buffer, length);
  if (strcmp(ack.run_id, request->run_id) != 0 ||
      ack.generation != request->generation)
    fail("cleanup-ack-run-binding", EINVAL);
}

static void process_cleanup(int dirfd, const struct request *request,
                            struct live_child *child, gid_t owner_gid,
                            uid_t owner_uid) {
  struct timespec deadline;
  const struct timespec *deadline_pointer = NULL;
  if (child->pid > 1) {
    deadline = deadline_after_seconds(15 * 60);
    deadline_pointer = &deadline;
  }
  if (!wait_for_exchange_file(dirfd, "cleanup.json", deadline_pointer,
                              "cleanup-watch")) {
    wait_for_exit_and_reap(child->pid, child->pidfd, "pidfd-self-cleanup-timeout");
    close(child->pidfd);
    child->pid = 0;
    child->pidfd = -1;
    fail("pidfd-self-cleanup-timeout", ETIMEDOUT);
  }
  fsync_exchange_directory(dirfd, "cleanup-observed-sync");
  char buffer[MAX_JSON + 1];
  size_t length = 0;
  read_json_at(dirfd, "cleanup.json", owner_uid, owner_gid,
               buffer, sizeof(buffer), &length);
  struct cleanup_request cleanup = parse_cleanup_request(buffer, length);
  if (strcmp(cleanup.run_id, request->run_id) != 0 ||
      cleanup.generation != request->generation)
    fail("cleanup-run-binding", EINVAL);

  int created = child->pid > 1;
  if (created) {
    if (cleanup.replacement_pid != child->pid ||
        strcmp(cleanup.replacement_identity_sha256, child->identity_sha256) != 0) {
      wait_for_exit_and_reap(child->pid, child->pidfd, "cleanup-mismatched-child-reap");
      close(child->pidfd);
      child->pid = 0;
      child->pidfd = -1;
      fail("cleanup-child-binding", EINVAL);
    }
    wait_for_exit_and_reap(child->pid, child->pidfd, "cleanup-kill-reap");
    close(child->pidfd);
    child->pidfd = -1;
  } else if (cleanup.replacement_pid != 0 ||
             cleanup.replacement_identity_sha256[0] != '\0') {
    fail("cleanup-untracked-child", EINVAL);
  }

  write_cleanup_response(dirfd, request, child, created, owner_gid);
  wait_for_cleanup_acknowledgement(dirfd, request, owner_uid, owner_gid);
  remove_operation_files(dirfd, owner_uid, owner_gid);
  ensure_ready_file(dirfd, owner_gid);
  child->pid = 0;
  child->identity_sha256[0] = '\0';
}

static void watch_requests(int dirfd, uint64_t host_pidns_inode,
                           gid_t owner_gid, uid_t owner_uid) {
  char last_run_id[MAX_TEXT] = "";
  uint64_t last_generation = 0;
  ensure_ready_file(dirfd, owner_gid);
  require_operation_files_absent(dirfd);
  for (;;) {
    poll_request(dirfd);
    fsync_exchange_directory(dirfd, "request-observed-sync");
    char buffer[MAX_JSON + 1];
    size_t length = 0;
    read_json_at(dirfd, "request.json", owner_uid, owner_gid,
                 buffer, sizeof(buffer), &length);
    struct request request = parse_create_request(buffer, length);
    if (strcmp(request.run_id, last_run_id) == 0 &&
        request.generation == last_generation)
      fail("duplicate-run-key", EALREADY);
    struct live_child child = {.pid = 0, .pidfd = -1};
    (void)make_create_response(dirfd, &request, host_pidns_inode,
                               "/system.slice/symmetry-owner.service/manager",
                               owner_gid, &child);
    process_cleanup(dirfd, &request, &child, owner_gid, owner_uid);
    memcpy(last_run_id, request.run_id, sizeof(last_run_id));
    last_generation = request.generation;
  }
}

static int parse_u64_arg(const char *value, uint64_t *out) {
  if (!value || !*value) return -1;
  errno = 0;
  char *end = NULL;
  unsigned long long n = strtoull(value, &end, 10);
  if (errno || end == value || *end) return -1;
  *out = (uint64_t)n;
  return 0;
}

int main(int argc, char **argv) {
  umask(077);
  if (argc == 4 && strcmp(argv[1], "--probe") == 0 &&
      strcmp(argv[2], "--host-pidns-inode") == 0) {
    uint64_t host_inode = 0;
    if (parse_u64_arg(argv[3], &host_inode) != 0) fail("probe-arguments", EINVAL);
    probe_literal_reuse(host_inode, "/system.slice/symmetry-owner.service/manager");
    return 0;
  }
  if (argc == 8 && strcmp(argv[1], "--watch") == 0 &&
      strcmp(argv[2], "--directory") == 0 &&
      strcmp(argv[4], "--host-pidns-inode") == 0 &&
      strcmp(argv[6], "--a-gid") == 0) {
    uint64_t host_inode = 0, gid = 0;
    if (parse_u64_arg(argv[5], &host_inode) != 0 ||
        parse_u64_arg(argv[7], &gid) != 0 || gid > UINT32_MAX)
      fail("watch-arguments", EINVAL);
    if (geteuid() != 0 || getegid() != 0) fail("watch-requires-root", EPERM);
    probe_literal_reuse(host_inode, "/system.slice/symmetry-owner.service/manager");
    int dirfd = open_exchange_directory(argv[3], (gid_t)gid);
    watch_requests(dirfd, host_inode, (gid_t)gid, (uid_t)52000);
    return 0;
  }
  fail("arguments", EINVAL);
  return 1;
}
