package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type openCodeProbeReceipt struct {
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

// assertProbeReceiptBindsDigest keeps a promoted digest receipt-backed: the committed 2.4.1
// probe receipt and README must carry the digest, identity and version line it gates.
func assertProbeReceiptBindsDigest(t *testing.T, fileName, magic, asset, digest string) {
	t.Helper()
	directory := filepath.Join("..", "testdata", "opencode", TestedVersion)
	encoded, err := os.ReadFile(filepath.Join(directory, fileName))
	if err != nil {
		t.Fatal(err)
	}
	var receipt openCodeProbeReceipt
	if err := json.Unmarshal(encoded, &receipt); err != nil {
		t.Fatalf("probe receipt: %v", err)
	}
	if got, want := "sha256:"+receipt.Binary.SHA256, digest; got != want {
		t.Fatalf("receipt blob digest = %q, want %q", got, want)
	}
	if receipt.Binary.Size <= 0 || receipt.Binary.Magic != magic || receipt.Binary.Asset != asset {
		t.Fatalf("receipt blob identity = %+v, want the pinned %s asset", receipt.Binary, asset)
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
		t.Fatalf("2.4.1 README does not carry the receipt's %s digest", asset)
	}
}

func TestOpenCodeTestdataBindsThePromotedWindowsDigest(t *testing.T) {
	assertProbeReceiptBindsDigest(t, "probe-windows.json", "4d5a", "opencode-windows-x64.zip", TestedExecutableHash)
}

func TestOpenCodeTestdataBindsTheRecordedLinuxDigest(t *testing.T) {
	assertProbeReceiptBindsDigest(t, "probe-linux.json", "7f454c46", "opencode-linux-x64.tar.gz", testedLinuxExecutableHash)
}
