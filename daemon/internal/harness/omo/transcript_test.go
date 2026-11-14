package omo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type transcriptBinary struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type pinnedTranscript struct {
	SourceReceiptSHA256 string `json:"sourceReceiptSha256"`
	Binary              struct {
		transcriptBinary
		IndependentPlatformPin transcriptBinary `json:"independentPlatformPin"`
	} `json:"binary"`
	Platform struct {
		System string `json:"system"`
	} `json:"platform"`
	ExcludedReadOnlyCommands []string `json:"excludedReadOnlyCommands"`
	Transcript               []struct {
		Request *Request `json:"request"`
		Native  *string  `json:"native"`
	} `json:"transcript"`
}

// Each raw transcript runs on its native platform. A retained Windows path is
// not a Linux session handle, and fixture replay must not rewrite either one.
func TestPinnedNativeTranscript(t *testing.T) {
	var expectedBinary transcriptBinary
	var expectedSystem string
	switch runtime.GOOS {
	case "windows":
		expectedSystem = "Windows"
		expectedBinary = transcriptBinary{
			Path: "C:/Users/lhan/.local/bin/omo.exe", Size: 202352128,
			SHA256: "04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464",
		}
	case "linux":
		expectedSystem = "Linux"
		expectedBinary = transcriptBinary{
			Path: "/root/.local/bin/omo", Size: 211998896,
			SHA256: "3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444",
		}
	default:
		t.Skip("OmO native transcript has no pin for this platform")
	}
	filename := filepath.Join("..", "testdata", "omo", "5.1.69", runtime.GOOS+".json")
	encoded, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var fixture pinnedTranscript
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatal(err)
	}
	actualBinary := fixture.Binary.transcriptBinary
	actualBinary.Path = strings.ReplaceAll(actualBinary.Path, "\\", "/")
	independentPin := fixture.Binary.IndependentPlatformPin
	independentPin.Path = strings.ReplaceAll(independentPin.Path, "\\", "/")
	if fixture.Platform.System != expectedSystem || actualBinary != expectedBinary || independentPin != expectedBinary {
		t.Fatalf("fixture does not bind the independent %s executable pin", runtime.GOOS)
	}
	if digest, err := hex.DecodeString(fixture.SourceReceiptSHA256); err != nil || len(digest) != sha256.Size {
		t.Fatal("fixture has no SHA256 source-receipt identity")
	}
	if !reflect.DeepEqual(fixture.ExcludedReadOnlyCommands, []string{"get_session_stats"}) {
		t.Fatal("fixture must disclose its excluded read-only witness command")
	}
	if len(fixture.Transcript) == 0 {
		t.Fatal("native transcript is empty")
	}

	validator := NewValidator(nil)
	decoder := NewDecoder(defaultMaxRecordBytes)
	var expectedFinal json.RawMessage
	sawUnidentifiedDelta := false
	for index, entry := range fixture.Transcript {
		if (entry.Request == nil) == (entry.Native == nil) {
			t.Fatalf("transcript entry %d must be exactly one request or native frame", index)
		}
		if entry.Request != nil {
			if err := validator.Register(*entry.Request); err != nil {
				t.Fatalf("entry %d register %s: %v", index, entry.Request.Type, err)
			}
			continue
		}
		if strings.TrimSpace(*entry.Native) == "" {
			t.Fatalf("entry %d has an empty native frame", index)
		}
		var original struct {
			Type            string          `json:"type"`
			Message         json.RawMessage `json:"message"`
			ClientMessageID *string         `json:"clientMessageId"`
			ClientTurnID    *string         `json:"clientTurnId"`
		}
		if err := json.Unmarshal([]byte(*entry.Native), &original); err != nil {
			t.Fatalf("entry %d native JSON: %v", index, err)
		}
		if original.Type == "message_update" && original.ClientMessageID == nil && original.ClientTurnID == nil {
			sawUnidentifiedDelta = true
		}
		if original.Type == "message_end" {
			var message struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
			}
			if err := json.Unmarshal(original.Message, &message); err != nil {
				t.Fatalf("entry %d complete message: %v", index, err)
			}
			if message.Role == "assistant" && message.StopReason == "stop" {
				expectedFinal = original.Message
			}
		}
		// Split within JSON strings and UTF-8 bytes as a pipe may do. Native
		// payloads are passed through byte-for-byte, with only their LF framing.
		frame := append([]byte(*entry.Native), '\n')
		for offset := 0; offset < len(frame); offset += 7 {
			end := min(offset+7, len(frame))
			records, err := decoder.Feed(frame[offset:end])
			if err != nil {
				t.Fatalf("entry %d decode: %v", index, err)
			}
			for _, record := range records {
				if err := validator.Observe(record); err != nil {
					t.Fatalf("entry %d observe %s: %v", index, record.Type, err)
				}
			}
		}
	}
	if err := decoder.Close(); err != nil {
		t.Fatal(err)
	}
	final, err := validator.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if !sawUnidentifiedDelta {
		t.Fatal("native transcript no longer covers untagged diagnostic deltas")
	}
	if len(final) == 0 || len(expectedFinal) == 0 {
		t.Fatal("native transcript did not produce a complete normal-stop assistant")
	}
	var actualObject, expectedObject any
	if err := json.Unmarshal(final, &actualObject); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expectedFinal, &expectedObject); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualObject, expectedObject) {
		t.Fatal("Finish returned something other than the exact complete native final message")
	}
	assertOriginalArtifactResult(t, final)
}

func assertOriginalArtifactResult(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var message struct {
		Role       string `json:"role"`
		StopReason string `json:"stopReason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	if message.Role != "assistant" || message.StopReason != "stop" || len(message.Content) != 1 || message.Content[0].Type != "text" {
		t.Fatal("native result is not one complete normal assistant text")
	}
	var observed struct {
		OK       bool   `json:"ok"`
		Artifact string `json:"artifact"`
		Bytes    int    `json:"bytes"`
		SHA256   string `json:"sha256"`
	}
	text := json.NewDecoder(bytes.NewBufferString(message.Content[0].Text))
	text.DisallowUnknownFields()
	if !json.Valid([]byte(message.Content[0].Text)) {
		t.Fatal("native final text is not one complete JSON object")
	}
	if err := text.Decode(&observed); err != nil {
		t.Fatal(err)
	}
	const expectedContent = "OmO native durable evidence\n"
	expectedDigest := sha256.Sum256([]byte(expectedContent))
	if !observed.OK || observed.Artifact != "omo-native-original.txt" || observed.Bytes != len(expectedContent) || observed.SHA256 != hex.EncodeToString(expectedDigest[:]) {
		t.Fatalf("native final does not identify the independently requested artifact: %+v", observed)
	}
}
