package pi

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	ProviderBridgeURLEnvironment    = "SYMMETRY_PI_PROVIDER_BRIDGE_URL"
	ProviderBridgeNonceEnvironment  = "SYMMETRY_PI_PROVIDER_BRIDGE_NONCE"
	providerBridgeExtensionPrefix   = "symmetry-pi-provider-bridge-"
	providerBridgeExtensionSuffix   = ".ts"
	maxProviderBridgeExtensionBytes = 1 << 20
)

// ProviderBridgeExtension identifies one immutable generated Pi extension.
// The file contains only granted resource IDs and operations; endpoint and
// nonce values remain in the child process environment.
type ProviderBridgeExtension struct {
	Path   string
	SHA256 string
}

// MaterializeProviderBridgeExtension writes a deterministic, dependency-free
// TypeScript extension beneath an app-owned absolute directory.
func MaterializeProviderBridgeExtension(directory string, grants []protocol.ProviderGrant) (ProviderBridgeExtension, error) {
	if strings.TrimSpace(directory) == "" || !filepath.IsAbs(directory) || strings.IndexByte(directory, 0) >= 0 {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension directory is invalid")
	}
	source, err := providerBridgeExtensionSource(grants)
	if err != nil {
		return ProviderBridgeExtension{}, err
	}
	digest := sha256.Sum256(source)
	digestText := hex.EncodeToString(digest[:])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ProviderBridgeExtension{}, errors.New("create provider bridge extension directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension directory is unsafe")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return ProviderBridgeExtension{}, errors.New("secure provider bridge extension directory")
	}

	path := filepath.Join(directory, providerBridgeExtensionPrefix+digestText+providerBridgeExtensionSuffix)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return ProviderBridgeExtension{}, errors.New("provider bridge extension conflicts with unsafe file")
		}
		existing, readErr := readProviderBridgeExtension(path, len(source))
		if readErr != nil || !bytes.Equal(existing, source) {
			return ProviderBridgeExtension{}, errors.New("provider bridge extension conflicts with existing file")
		}
		return ProviderBridgeExtension{Path: path, SHA256: digestText}, nil
	}
	if err != nil {
		return ProviderBridgeExtension{}, errors.New("create provider bridge extension")
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(source); err != nil {
		return ProviderBridgeExtension{}, errors.New("write provider bridge extension")
	}
	if err := file.Sync(); err != nil {
		return ProviderBridgeExtension{}, errors.New("sync provider bridge extension")
	}
	if err := file.Close(); err != nil {
		return ProviderBridgeExtension{}, errors.New("close provider bridge extension")
	}
	remove = false
	return ProviderBridgeExtension{Path: path, SHA256: digestText}, nil
}

// MaterializeProviderBridgeExtensionForShare publishes the extension into an
// operator-provisioned A:B-group share. Unlike the legacy materializer, it
// never creates or changes directory permissions. The StateDir is resolved
// separately so the share cannot expose private daemon state. expectedBGroupID
// must come from the installed root-owned fixed-B configuration, not user input.
func MaterializeProviderBridgeExtensionForShare(directory, stateDir string, expectedBGroupID uint32, grants []protocol.ProviderGrant) (ProviderBridgeExtension, error) {
	if !filepath.IsAbs(directory) || strings.IndexByte(directory, 0) >= 0 || filepath.Clean(directory) != directory ||
		!filepath.IsAbs(stateDir) || strings.IndexByte(stateDir, 0) >= 0 || expectedBGroupID == 0 {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share path is invalid")
	}
	resolvedDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil || resolvedDirectory != directory {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share path is not canonical")
	}
	resolvedStateDir, err := resolveProviderBridgeStateDir(filepath.Clean(stateDir))
	if err != nil {
		return ProviderBridgeExtension{}, errors.New("resolve private provider bridge state directory")
	}
	if relative, err := filepath.Rel(resolvedStateDir, resolvedDirectory); err != nil || relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share must be outside the private state directory")
	}

	directoryInfo, err := os.Lstat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share directory is unsafe")
	}
	directoryUID, directoryGID, identified := providerBridgeUnixOwner(directoryInfo)
	if !identified || directoryUID != uint64(os.Geteuid()) || directoryGID != uint64(expectedBGroupID) || directoryInfo.Mode().Perm() != 0o750 ||
		directoryInfo.Mode()&os.ModeSetgid == 0 || directoryInfo.Mode()&(os.ModeSetuid|os.ModeSticky) != 0 {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share directory must be A-owned by B primary GID, setgid, and mode 02750")
	}
	if err := validateProviderBridgeShareAncestors(filepath.Dir(directory)); err != nil {
		return ProviderBridgeExtension{}, err
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return ProviderBridgeExtension{}, errors.New("open provider bridge extension share directory")
	}
	defer root.Close()
	openedDirectory, err := root.Stat(".")
	if err != nil || !os.SameFile(directoryInfo, openedDirectory) {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share directory changed during open")
	}
	if openedUID, openedGID, ok := providerBridgeUnixOwner(openedDirectory); !ok || openedUID != directoryUID || openedGID != directoryGID {
		return ProviderBridgeExtension{}, errors.New("provider bridge extension share directory identity changed during open")
	}

	source, err := providerBridgeExtensionSource(grants)
	if err != nil {
		return ProviderBridgeExtension{}, err
	}
	digest := sha256.Sum256(source)
	digestText := hex.EncodeToString(digest[:])
	name := providerBridgeExtensionPrefix + digestText + providerBridgeExtensionSuffix
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440)
	if errors.Is(err, os.ErrExist) {
		if err := verifySharedProviderBridgeExtension(root, name, source, directoryUID, directoryGID); err != nil {
			return ProviderBridgeExtension{}, err
		}
		if err := verifyProviderBridgeSharePathPinned(directory, directoryInfo); err != nil {
			return ProviderBridgeExtension{}, err
		}
		return ProviderBridgeExtension{Path: filepath.Join(directory, name), SHA256: digestText}, nil
	}
	if err != nil {
		return ProviderBridgeExtension{}, errors.New("create provider bridge extension in share")
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = root.Remove(name)
		}
	}()
	if _, err := file.Write(source); err != nil {
		return ProviderBridgeExtension{}, errors.New("write provider bridge extension in share")
	}
	if err := file.Sync(); err != nil {
		return ProviderBridgeExtension{}, errors.New("sync provider bridge extension in share")
	}
	if err := file.Chmod(0o440); err != nil {
		return ProviderBridgeExtension{}, errors.New("secure provider bridge extension in share")
	}
	if err := file.Close(); err != nil {
		return ProviderBridgeExtension{}, errors.New("close provider bridge extension in share")
	}
	if err := verifySharedProviderBridgeExtension(root, name, source, directoryUID, directoryGID); err != nil {
		return ProviderBridgeExtension{}, err
	}
	if err := verifyProviderBridgeSharePathPinned(directory, directoryInfo); err != nil {
		return ProviderBridgeExtension{}, err
	}
	remove = false
	return ProviderBridgeExtension{Path: filepath.Join(directory, name), SHA256: digestText}, nil
}

func validateProviderBridgeShareAncestors(directory string) error {
	for current := directory; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("provider bridge extension share ancestor is unsafe")
		}
		uid, _, ok := providerBridgeUnixOwner(info)
		if !ok || uid != 0 && uid != uint64(os.Geteuid()) {
			return errors.New("provider bridge extension share ancestor is not owned by root or A")
		}
		if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return errors.New("provider bridge extension share ancestor is writable by an untrusted group or other user")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}

func verifyProviderBridgeSharePathPinned(directory string, expected os.FileInfo) error {
	current, err := os.Lstat(directory)
	if err != nil || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, current) {
		return errors.New("provider bridge extension share path changed during materialization")
	}
	if err := validateProviderBridgeShareAncestors(filepath.Dir(directory)); err != nil {
		return err
	}
	return nil
}

func resolveProviderBridgeStateDir(directory string) (string, error) {
	resolved, err := filepath.EvalSymlinks(directory)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(directory)
	if parent == directory {
		return "", err
	}
	resolvedParent, err := resolveProviderBridgeStateDir(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(directory)), nil
}

func verifySharedProviderBridgeExtension(root *os.Root, name string, expected []byte, ownerUID, directoryGID uint64) error {
	pathInfo, err := root.Lstat(name)
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Mode().Perm() != 0o440 ||
		pathInfo.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("provider bridge extension conflicts with unsafe shared file")
	}
	uid, gid, identified := providerBridgeUnixOwner(pathInfo)
	if !identified || uid != ownerUID || gid != directoryGID {
		return errors.New("provider bridge extension conflicts with shared file ownership")
	}
	// Linux O_NOFOLLOW closes the gap between the no-symlink check and opening
	// an existing digest file. The share root pins all subsequent operations.
	const linuxONoFollow = 0x20000
	file, err := root.OpenFile(name, os.O_RDONLY|linuxONoFollow, 0)
	if err != nil {
		return errors.New("open provider bridge extension in share")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) {
		return errors.New("provider bridge extension changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(expected))+1))
	if err != nil || !bytes.Equal(data, expected) {
		return errors.New("provider bridge extension conflicts with existing digest file")
	}
	return nil
}

func providerBridgeUnixOwner(info os.FileInfo) (uid, gid uint64, ok bool) {
	if info == nil || info.Sys() == nil {
		return 0, 0, false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, 0, false
	}
	uidField, gidField := value.FieldByName("Uid"), value.FieldByName("Gid")
	if !uidField.IsValid() || !gidField.IsValid() || !uidField.CanUint() || !gidField.CanUint() {
		return 0, 0, false
	}
	return uidField.Uint(), gidField.Uint(), true
}

func readProviderBridgeExtension(path string, size int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, int64(size)+1))
}

func providerBridgeExtensionSource(grants []protocol.ProviderGrant) ([]byte, error) {
	resources := map[string][]string{
		string(protocol.ProviderOperationResourceSync): {},
		string(protocol.ProviderOperationChangeUpsert): {},
		string(protocol.ProviderOperationChangeUpdate): {},
	}
	seen := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if validateProviderBridgeGrant(grant) != nil {
			return nil, errProviderBridgeInvalidOptions
		}
		if _, duplicate := seen[grant.ResourceID]; duplicate {
			return nil, errProviderBridgeInvalidOptions
		}
		seen[grant.ResourceID] = struct{}{}
		for _, operation := range grant.Operations {
			resources[operation] = append(resources[operation], grant.ResourceID)
		}
	}
	if len(seen) == 0 {
		return nil, errProviderBridgeInvalidOptions
	}
	for operation := range resources {
		sort.Strings(resources[operation])
	}
	encoded, err := json.Marshal(resources)
	if err != nil {
		return nil, errors.New("encode provider bridge extension grants")
	}
	return []byte(fmt.Sprintf(providerBridgeExtensionTemplate, encoded)), nil
}

const providerBridgeExtensionTemplate = `const resourcesByOperation = %s as const;

function requiredEnvironment(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error("Symmetry provider bridge configuration is missing");
  return value;
}

function bridgeConfiguration(): { endpoint: string; nonce: string } {
  const endpoint = requiredEnvironment("SYMMETRY_PI_PROVIDER_BRIDGE_URL");
  const nonce = requiredEnvironment("SYMMETRY_PI_PROVIDER_BRIDGE_NONCE");
  let parsed: URL;
  try { parsed = new URL(endpoint); } catch { throw new Error("Symmetry provider bridge endpoint is invalid"); }
  if (parsed.protocol !== "http:" || parsed.hostname !== "127.0.0.1" || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/v1/actions") {
    throw new Error("Symmetry provider bridge endpoint is invalid");
  }
  if (!/^[0-9a-f]{64}$/.test(nonce)) throw new Error("Symmetry provider bridge nonce is invalid");
  return { endpoint: parsed.toString(), nonce };
}

function safeFailureCode(value: unknown): string {
  return typeof value === "string" && /^[a-z0-9_]{1,256}$/.test(value) ? value : "invalid_bridge_response";
}

async function executeAction(toolCallId: string, operation: string, resourceId: string, input: Record<string, unknown>, signal?: AbortSignal) {
  const { endpoint, nonce } = bridgeConfiguration();
  let response: Response;
  try {
    response = await fetch(endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Symmetry-Bridge-Nonce": nonce },
      body: JSON.stringify({ resource_id: resourceId, operation, action_key: toolCallId, input }),
      signal,
    });
  } catch {
    throw new Error("Symmetry provider bridge request failed");
  }
  const declaredLength = Number(response.headers.get("content-length") ?? "0");
  if (!Number.isFinite(declaredLength) || declaredLength < 0 || declaredLength > 65536) throw new Error("Symmetry provider bridge response is invalid");
  const text = await response.text();
  if (new TextEncoder().encode(text).byteLength > 65536) throw new Error("Symmetry provider bridge response is invalid");
  if (!response.ok) throw new Error("Symmetry provider bridge rejected the request");
  let payload: any;
  try { payload = JSON.parse(text); } catch { throw new Error("Symmetry provider bridge response is invalid"); }
  if (!payload || typeof payload !== "object" || typeof payload.action_id !== "string") throw new Error("Symmetry provider bridge response is invalid");
  if (payload.outcome === "succeeded") {
    return { content: [{ type: "text", text: "Provider action succeeded." }], details: payload };
  }
  const failureCode = safeFailureCode(payload.failure_code);
  if (payload.outcome === "failed") throw new Error("Symmetry provider action failed (" + failureCode + ")");
  if (payload.outcome === "unknown") throw new Error("Symmetry provider action outcome is unknown (" + failureCode + "); do not retry with a new tool call");
  throw new Error("Symmetry provider bridge response is invalid");
}

function registerAction(pi: any, operation: string, name: string, label: string, description: string) {
  const resourceIds = (resourcesByOperation as Record<string, readonly string[]>)[operation] ?? [];
  if (resourceIds.length === 0) return;
  const change = operation !== "resource.sync";
  const properties: Record<string, unknown> = { resource_id: { type: "string", enum: resourceIds, description: "Authorized connected resource ID." } };
  const required = ["resource_id"];
  if (change) {
    properties.title = { type: "string", minLength: 1, maxLength: 255, description: "Pull request title." };
    properties.body = { type: "string", maxLength: 1048576, description: "Pull request description in Markdown. Omit to keep an existing description unchanged." };
    required.push("title");
  }
  pi.registerTool({
    name,
    label,
    description,
    promptSnippet: label + " through the authorized Symmetry provider broker",
    promptGuidelines: ["Use " + name + " only for the authorized connected resource. Never substitute local Git commands for this provider operation."],
    parameters: { type: "object", properties, required, additionalProperties: false },
    async execute(toolCallId: string, params: any, signal?: AbortSignal) {
      const input = change ? { title: params.title, ...(params.body === undefined ? {} : { body: params.body }) } : {};
      return executeAction(toolCallId, operation, params.resource_id, input, signal);
    },
  });
}

export default function symmetryProviderBridge(pi: any) {
  bridgeConfiguration();
  registerAction(pi, "resource.sync", "symmetry_resource_sync", "Synchronize connected resource",
    "Refresh Symmetry's stored copy of a connected GitHub or Azure DevOps resource, such as repository metadata, issues or work items, CI status and linked pull request state. It changes nothing at the provider. The text result reports only success or a failure code, not the synchronized content.");
  registerAction(pi, "change.upsert", "symmetry_change_upsert", "Create or reconcile change request",
    "Create the pull request from this task's approved source branch to its approved target branch, or update the title and body of the open pull request between them. Symmetry fixes the repository and both branches; you supply the title and optional body. It does not push commits, so the source branch must already exist on the remote. The text result reports only success or a failure code.");
  registerAction(pi, "change.update", "symmetry_change_update", "Update authorized change request",
    "Update the title and body of the one pull request this task may change: the approved pull request for this task, or the one this run's symmetry_change_upsert returned. Symmetry selects the pull request; you supply the title and optional body. It cannot create a pull request. The text result reports only success or a failure code.");
}
`

func piRPCArgsWithProviderExtension(profileArgs []string, resumeState *SessionState, extensionPath string) ([]string, error) {
	if strings.TrimSpace(extensionPath) == "" || !filepath.IsAbs(extensionPath) || strings.IndexByte(extensionPath, 0) >= 0 {
		return nil, errors.New("pi provider bridge extension path is invalid")
	}
	info, err := os.Stat(extensionPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("pi provider bridge extension is unavailable")
	}
	if err := ValidateProviderBridgeProfileArgs(profileArgs); err != nil {
		return nil, err
	}
	base, err := piRPCArgs(profileArgs, resumeState)
	if err != nil {
		return nil, err
	}
	filtered := make([]string, 0, len(base)+3)
	for _, argument := range base {
		if argument != "--no-extensions" && argument != "-ne" {
			filtered = append(filtered, argument)
		}
	}
	return append(filtered, "--no-extensions", "--extension", extensionPath), nil
}

// ValidateProviderBridgeProfileArgs rejects profile-owned tool selection when
// the daemon must install the exact provider tools for this run.
func ValidateProviderBridgeProfileArgs(profileArgs []string) error {
	for _, argument := range profileArgs {
		if argument == "--no-tools" || argument == "-nt" || argument == "--tools" || argument == "-t" {
			return errors.New("pi provider bridge cannot use a profile tool override")
		}
	}
	return nil
}

func validateProviderBridgeLaunch(access *protocol.ProviderAccess, launch *harness.ProviderBridgeLaunch) (*harness.ProviderBridgeLaunch, error) {
	if access == nil {
		if launch != nil {
			return nil, errors.New("pi provider bridge launch has no provider access")
		}
		return nil, nil
	}
	if launch == nil || isNilProviderBridgeLifecycle(launch.Lifecycle) {
		return nil, unsupported(harness.CapabilityProviderAccess, "pi provider broker bridge is not verified")
	}
	if access.Path != "/api/v1/provider-actions" || strings.TrimSpace(access.Token) == "" || len(access.Grants) == 0 {
		return nil, errors.New("pi provider access is invalid")
	}
	expectedSource, err := providerBridgeExtensionSource(access.Grants)
	if err != nil {
		return nil, errors.New("pi provider access grants are invalid")
	}
	expectedDigest := sha256.Sum256(expectedSource)
	expectedDigestText := hex.EncodeToString(expectedDigest[:])
	parsed, err := url.Parse(launch.URL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != providerBridgePath {
		return nil, errors.New("pi provider bridge URL is invalid")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return nil, errors.New("pi provider bridge URL is invalid")
	}
	if len(launch.Nonce) != 64 || launch.Nonce != strings.ToLower(launch.Nonce) {
		return nil, errors.New("pi provider bridge nonce is invalid")
	}
	if _, err := hex.DecodeString(launch.Nonce); err != nil {
		return nil, errors.New("pi provider bridge nonce is invalid")
	}
	if len(launch.ExtensionSHA256) != sha256.Size*2 || launch.ExtensionSHA256 != strings.ToLower(launch.ExtensionSHA256) {
		return nil, errors.New("pi provider bridge extension digest is invalid")
	}
	if _, err := hex.DecodeString(launch.ExtensionSHA256); err != nil {
		return nil, errors.New("pi provider bridge extension digest is invalid")
	}
	if subtle.ConstantTimeCompare([]byte(expectedDigestText), []byte(launch.ExtensionSHA256)) != 1 {
		return nil, errors.New("pi provider bridge extension does not match provider grants")
	}
	if !filepath.IsAbs(launch.ExtensionPath) || strings.IndexByte(launch.ExtensionPath, 0) >= 0 {
		return nil, errors.New("pi provider bridge extension path is invalid")
	}
	info, err := os.Lstat(launch.ExtensionPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxProviderBridgeExtensionBytes {
		return nil, errors.New("pi provider bridge extension is unavailable")
	}
	data, err := readProviderBridgeExtension(launch.ExtensionPath, int(info.Size()))
	if err != nil || len(data) != int(info.Size()) {
		return nil, errors.New("read pi provider bridge extension")
	}
	digest := sha256.Sum256(data)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(launch.ExtensionSHA256)) != 1 {
		return nil, errors.New("pi provider bridge extension digest does not match")
	}
	copy := *launch
	return &copy, nil
}

func appendProviderBridgeEnvironment(environment []string, endpoint, nonce string) ([]string, error) {
	for _, entry := range environment {
		name, _, present := strings.Cut(entry, "=")
		if !present {
			continue
		}
		if strings.EqualFold(name, ProviderBridgeURLEnvironment) || strings.EqualFold(name, ProviderBridgeNonceEnvironment) {
			return nil, errors.New("pi provider bridge environment is already defined")
		}
	}
	result := append([]string(nil), environment...)
	return append(result, ProviderBridgeURLEnvironment+"="+endpoint, ProviderBridgeNonceEnvironment+"="+nonce), nil
}

func isNilProviderBridgeLifecycle(value harness.ProviderBridgeLifecycle) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
