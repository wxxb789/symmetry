package pi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestProviderBridgeExtensionSourceIsDeterministicAndCredentialFree(t *testing.T) {
	first := []protocol.ProviderGrant{
		{ResourceID: "22222222-2222-4222-8222-222222222222", Provider: "github", Kind: "repository", Operations: []string{"change.update", "resource.sync"}},
		{ResourceID: "11111111-1111-4111-8111-111111111111", Provider: "github", Kind: "repository", Operations: []string{"change.upsert", "resource.sync"}},
	}
	second := []protocol.ProviderGrant{first[1], first[0]}
	left, err := providerBridgeExtensionSource(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := providerBridgeExtensionSource(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("extension source depends on grant order")
	}
	source := string(left)
	for _, required := range []string{
		"symmetry_resource_sync",
		"symmetry_change_upsert",
		"symmetry_change_update",
		"action_key: toolCallId",
		ProviderBridgeURLEnvironment,
		ProviderBridgeNonceEnvironment,
		"additionalProperties: false",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("extension source omits %q", required)
		}
	}
	for _, forbidden := range []string{"Authorization", "provider_token", "source_branch", "target_branch", "pull_request_url"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("extension source contains forbidden authority field %q", forbidden)
		}
	}
}

func TestMaterializeProviderBridgeExtensionIsImmutable(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "extensions")
	first, err := MaterializeProviderBridgeExtension(directory, testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	second, err := MaterializeProviderBridgeExtension(directory, testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !filepath.IsAbs(first.Path) || len(first.SHA256) != 64 {
		t.Fatalf("materialized extension = %#v then %#v", first, second)
	}
	if err := os.WriteFile(first.Path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeProviderBridgeExtension(directory, testProviderGrants()); err == nil {
		t.Fatal("materialization replaced a conflicting immutable extension")
	}
}

func TestMaterializeProviderBridgeExtensionRejectsExistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink may require an elevated Windows token")
	}
	directory := filepath.Join(t.TempDir(), "extensions")
	source, err := providerBridgeExtensionSource(testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	path := filepath.Join(directory, providerBridgeExtensionPrefix+hex.EncodeToString(digest[:])+providerBridgeExtensionSuffix)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target.ts")
	if err := os.WriteFile(target, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := MaterializeProviderBridgeExtension(directory, testProviderGrants()); err == nil {
		t.Fatal("materialization accepted an existing symlink")
	}
}

func TestPiRPCArgsWithProviderExtensionUsesOnlyDaemonOwnedLoader(t *testing.T) {
	extension := filepath.Join(t.TempDir(), "provider.ts")
	if err := os.WriteFile(extension, []byte("export default function () {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := piRPCArgsWithProviderExtension([]string{"--provider", "openai", "--no-extensions", "--offline"}, nil, extension)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--mode", "rpc", "--provider", "openai", "--offline", "--no-extensions", "--extension", extension}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider extension args = %#v, want %#v", got, want)
	}
	for _, profile := range [][]string{{"--no-tools"}, {"-nt"}, {"--tools", "write"}, {"-t", "write"}} {
		if _, err := piRPCArgsWithProviderExtension(profile, nil, extension); err == nil {
			t.Fatalf("profile tool override %#v was accepted", profile)
		}
	}
}

func TestProviderBridgeExtensionRejectsInvalidInputs(t *testing.T) {
	if _, err := MaterializeProviderBridgeExtension("relative", testProviderGrants()); err == nil {
		t.Fatal("relative extension directory was accepted")
	}
	if _, err := providerBridgeExtensionSource(nil); err == nil {
		t.Fatal("empty grants were accepted")
	}
	duplicate := append(testProviderGrants(), testProviderGrants()[0])
	if _, err := providerBridgeExtensionSource(duplicate); err == nil {
		t.Fatal("duplicate resource grant was accepted")
	}
}

func TestGeneratedProviderBridgeExtensionLoadsInNativePi(t *testing.T) {
	if os.Getenv("SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE") != "1" {
		t.Skip("set SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1 to load the generated extension in native Pi")
	}
	extension, err := MaterializeProviderBridgeExtension(filepath.Join(t.TempDir(), "extensions"), testProviderGrants())
	if err != nil {
		t.Fatal(err)
	}
	args, err := piRPCArgsWithProviderExtension([]string{"--offline"}, nil, extension.Path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := execution.RunBoundedCommand(ctx, execution.Invocation{
		Program: DefaultExecutable,
		Args:    args,
		Dir:     t.TempDir(),
		Env: append(os.Environ(),
			ProviderBridgeURLEnvironment+"=http://127.0.0.1:1/v1/actions",
			ProviderBridgeNonceEnvironment+"="+strings.Repeat("a", 64),
		),
		InitialInput:           []byte("{\"id\":\"state-1\",\"type\":\"get_state\"}\n"),
		CloseInputAfterInitial: true,
	}, 1<<20)
	if err != nil {
		t.Fatalf("native Pi extension smoke error = %v; output=%s", err, output)
	}
	if !bytes.Contains(output, []byte(`"id":"state-1"`)) || !bytes.Contains(output, []byte(`"type":"response"`)) {
		t.Fatalf("native Pi extension smoke omitted correlated get_state response: %s", output)
	}
}
