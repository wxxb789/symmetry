package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenCodeTestdataBindsThePromotedWindowsDigest keeps TestedExecutableHash receipt-backed:
// the committed 2.4.1 probe receipt and README must carry the digest and version line it gates.
func TestOpenCodeTestdataBindsThePromotedWindowsDigest(t *testing.T) {
	directory := filepath.Join("..", "testdata", "opencode", TestedVersion)
	encoded, err := os.ReadFile(filepath.Join(directory, "probe-windows.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Binary struct {
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
			Magic  string `json:"magic"`
			Asset  string `json:"asset"`
		} `json:"binary"`
		Observations []struct {
			Command  []string `json:"command"`
			ExitCode int      `json:"exit_code"`
			Stdout   string   `json:"stdout"`
		} `json:"observations"`
	}
	if err := json.Unmarshal(encoded, &receipt); err != nil {
		t.Fatalf("probe receipt: %v", err)
	}
	if got, want := "sha256:"+receipt.Binary.SHA256, TestedExecutableHash; got != want {
		t.Fatalf("receipt blob digest = %q, want TestedExecutableHash %q", got, want)
	}
	if receipt.Binary.Size <= 0 || receipt.Binary.Magic != "4d5a" || receipt.Binary.Asset != "opencode-windows-x64.zip" {
		t.Fatalf("receipt blob identity = %+v, want the pinned Windows PE asset", receipt.Binary)
	}
	if len(receipt.Observations) != 2 {
		t.Fatalf("receipt observations = %d, want the version and help captures", len(receipt.Observations))
	}
	if exitCode := receipt.Observations[0].ExitCode; exitCode != 0 || strings.TrimSpace(receipt.Observations[0].Stdout) != "opencode v"+TestedVersion {
		t.Fatalf("receipt version observation = %+v, want the tested version line", receipt.Observations[0])
	}
	// The promoted version is derived from the committed line through the same parser
	// the probe uses, not restated from the constant.
	if parsed := parseVersion(receipt.Observations[0].Stdout); parsed != TestedVersion {
		t.Fatalf("receipt version line parses to %q, want %q", parsed, TestedVersion)
	}
	if receipt.Observations[1].ExitCode != 0 || !hasServeHelp(receipt.Observations[1].Stdout) {
		t.Fatalf("receipt help observation = %+v, want the recorded serve help gate", receipt.Observations[1])
	}
	readme, err := os.ReadFile(filepath.Join(directory, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), receipt.Binary.SHA256) {
		t.Fatal("2.4.1 README does not carry the receipt's promoted blob digest")
	}
}
