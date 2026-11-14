package omp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func chunkLine(id string, index, count, length int, data []byte) []byte {
	encoded, _ := json.Marshal(map[string]any{"type": "rpc_chunk", "chunkId": id, "index": index, "count": count, "byteLength": length, "data": base64.StdEncoding.EncodeToString(data)})
	return append(encoded, '\n')
}

func TestV2ChunksAreLosslessAndOrdered(t *testing.T) {
	logical := []byte(`{"type":"message_update","text":"λ日本語"}`)
	decoder := frameDecoder{negotiated: true}
	physical := append(chunkLine("rpc-1", 0, 2, len(logical), logical[:17]), chunkLine("rpc-1", 1, 2, len(logical), logical[17:])...)
	var observed []frame
	for _, value := range physical {
		if err := decoder.feed([]byte{value}, func(value frame) error { observed = append(observed, value); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := decoder.close(); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 || string(observed[0].raw) != string(logical) {
		t.Fatalf("decoded frames=%+v", observed)
	}
}

func TestMalformedTransportNeverDeliversLogicalFrames(t *testing.T) {
	logical := []byte(`{"type":"agent_start"}`)
	for _, scenario := range []string{"before-negotiation", "first-index", "interleaved-id", "wrong-count", "wrong-length", "interrupted", "incomplete", "duplicate-field", "duplicate-nested-field", "invalid-base64", "invalid-utf8", "oversized", "duplicate-chunk-id"} {
		t.Run(scenario, func(t *testing.T) {
			decoder := frameDecoder{negotiated: scenario != "before-negotiation"}
			first := chunkLine("a", 0, 2, len(logical), logical[:10])
			last := chunkLine("a", 1, 2, len(logical), logical[10:])
			var physical []byte
			switch scenario {
			case "before-negotiation":
				physical = first
			case "first-index":
				physical = last
			case "interleaved-id":
				physical = append(first, chunkLine("b", 1, 2, len(logical), logical[10:])...)
			case "wrong-count":
				physical = append(first, chunkLine("a", 1, 3, len(logical), logical[10:])...)
			case "wrong-length":
				physical = append(first, chunkLine("a", 1, 2, len(logical)+1, logical[10:])...)
			case "interrupted":
				physical = append(first, []byte("{\"type\":\"agent_end\",\"messages\":[]}\n")...)
			case "incomplete":
				physical = first
			case "duplicate-field":
				physical = []byte("{\"type\":\"agent_start\",\"type\":\"session_settled\"}\n")
			case "duplicate-nested-field":
				physical = []byte("{\"type\":\"message_end\",\"message\":{\"role\":\"user\",\"role\":\"assistant\"}}\n")
			case "invalid-base64":
				physical = []byte("{\"type\":\"rpc_chunk\",\"chunkId\":\"a\",\"index\":0,\"count\":1,\"byteLength\":3,\"data\":\"%%%\"}\n")
			case "invalid-utf8":
				physical = chunkLine("a", 0, 1, 1, []byte{255})
			case "oversized":
				physical = chunkLine("a", 0, 1, maxLogicalBytes+1, logical)
			case "duplicate-chunk-id":
				good := chunkLine("a", 0, 1, len(logical), logical)
				if err := decoder.feed(good, func(frame) error { return nil }); err != nil {
					t.Fatal(err)
				}
				physical = good
			}
			delivered := 0
			err := decoder.feed(physical, func(frame) error { delivered++; return nil })
			if err == nil {
				err = decoder.close()
			}
			if err == nil || delivered != 0 {
				t.Fatalf("malformed transport delivered %d frame(s), error %v", delivered, err)
			}
		})
	}
}

func TestJSONLRequiresBoundedCompleteObject(t *testing.T) {
	for _, data := range []string{"[]\n", "null\n", "{}\n", "{\"type\":\"agent_start\"}", "\n", strings.Repeat("x", maxFrameBytes) + "\n", "{\"type\":\"agent_start\"} {}\n"} {
		decoder := frameDecoder{}
		err := decoder.feed([]byte(data), func(frame) error { return nil })
		if err == nil {
			err = decoder.close()
		}
		if err == nil {
			t.Fatalf("accepted invalid framing %q", data[:min(len(data), 80)])
		}
	}
}

func TestChunkLedgerRejectsReviewOversizedIDs(t *testing.T) {
	logical := []byte(`{"type":"advisor_cost_changed"}`)
	if len(logical) != 31 {
		t.Fatalf("review payload has %d bytes, want 31", len(logical))
	}
	decoder := frameDecoder{negotiated: true}
	delivered := 0
	var err error
	for index := range 84 {
		id := fmt.Sprintf("%08d", index) + strings.Repeat("x", 800000)
		physical := chunkLine(id, 0, 1, 31, logical)
		if len(physical) != 800132 || len(physical) >= maxFrameBytes {
			t.Fatalf("review frame has %d bytes, want 800132 below the physical limit", len(physical))
		}
		err = decoder.feed(physical, func(frame) error { delivered++; return nil })
		if err != nil {
			break
		}
	}
	if !errors.Is(err, errProtocol) || delivered != 0 || len(decoder.seenChunks) != 0 || decoder.chunkID != "" || decoder.chunkData != nil {
		t.Fatalf("oversized IDs: delivered=%d retained=%d active=%d error=%v", delivered, len(decoder.seenChunks), len(decoder.chunkID), err)
	}
}

func TestChunkIDByteLimit(t *testing.T) {
	logical := []byte(`{"type":"advisor_cost_changed"}`)
	for _, scenario := range []struct {
		name string
		id   string
		want bool
	}{
		{"below", strings.Repeat("x", maxChunkIDBytes-1), true},
		{"exact", strings.Repeat("x", maxChunkIDBytes), true},
		{"above", strings.Repeat("x", maxChunkIDBytes+1), false},
		{"multibyte-exact", strings.Repeat("λ", maxChunkIDBytes/2), true},
		{"multibyte-above", strings.Repeat("λ", maxChunkIDBytes/2) + "x", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			decoder := frameDecoder{negotiated: true}
			delivered := 0
			err := decoder.feed(chunkLine(scenario.id, 0, 1, len(logical), logical), func(frame) error { delivered++; return nil })
			if scenario.want {
				if err != nil || delivered != 1 || !decoder.seenChunks[scenario.id] || decoder.seenChunkIDBytes != len(scenario.id) {
					t.Fatalf("valid ID: delivered=%d retained=%d bytes=%d error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, err)
				}
				return
			}
			if !errors.Is(err, errProtocol) || !strings.Contains(err.Error(), "chunk ID too large") || delivered != 0 || len(decoder.seenChunks) != 0 || decoder.seenChunkIDBytes != 0 || decoder.chunkID != "" || decoder.chunkData != nil {
				t.Fatalf("oversized ID: delivered=%d retained=%d bytes=%d active=%d error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, len(decoder.chunkID), err)
			}
		})
	}
}

func TestChunkLedgerEntryLimit(t *testing.T) {
	logical := []byte(`{"type":"advisor_cost_changed"}`)
	decoder := frameDecoder{negotiated: true, seenChunks: make(map[string]bool, maxChunkIDs)}
	for index := range maxChunkIDs - 1 {
		id := fmt.Sprintf("%08d", index)
		decoder.seenChunks[id] = true
		decoder.seenChunkIDBytes += len(id)
	}
	delivered := 0
	consume := func(frame) error { delivered++; return nil }
	for index, data := range [][]byte{logical[:10], logical[10:]} {
		if err := decoder.feed(chunkLine("last", index, 2, len(logical), data), consume); err != nil {
			t.Fatal(err)
		}
	}
	if delivered != 1 || len(decoder.seenChunks) != maxChunkIDs || !decoder.seenChunks["last"] || decoder.seenChunkIDBytes != 8*(maxChunkIDs-1)+len("last") || decoder.seenChunkIDBytes >= maxChunkLedgerBytes {
		t.Fatalf("exact entry bound: delivered=%d retained=%d bytes=%d", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes)
	}
	delivered = 0
	beforeBytes := decoder.seenChunkIDBytes
	err := decoder.feed(chunkLine("next", 0, 1, len(logical), logical), consume)
	if !errors.Is(err, errProtocol) || !strings.Contains(err.Error(), "too many chunk IDs") || delivered != 0 || len(decoder.seenChunks) != maxChunkIDs || decoder.seenChunks["next"] || decoder.seenChunkIDBytes != beforeBytes || decoder.chunkID != "" || decoder.chunkData != nil {
		t.Fatalf("entry overflow: delivered=%d retained=%d bytes=%d active=%q error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, decoder.chunkID, err)
	}
	err = decoder.feed(chunkLine("00000000", 0, 1, len(logical), logical), consume)
	if !errors.Is(err, errProtocol) || !strings.Contains(err.Error(), "replayed") || delivered != 0 || !decoder.seenChunks["00000000"] || len(decoder.seenChunks) != maxChunkIDs || decoder.seenChunkIDBytes != beforeBytes {
		t.Fatalf("historical replay at entry bound: delivered=%d retained=%d bytes=%d error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, err)
	}
}

func TestChunkLedgerByteLimitArithmetic(t *testing.T) {
	logical := []byte(`{"type":"advisor_cost_changed"}`)
	for _, scenario := range []struct {
		name      string
		remaining int
		id        string
		want      bool
	}{
		{"below", 4, "abc", true},
		{"exact", 4, "abcd", true},
		{"above", 4, "abcde", false},
		{"full", 0, "a", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Seed only the byte counter; exercising 64 MiB of history is separate.
			beforeBytes := maxChunkLedgerBytes - scenario.remaining
			decoder := frameDecoder{negotiated: true, seenChunks: map[string]bool{"old": true}, seenChunkIDBytes: beforeBytes}
			delivered := 0
			consume := func(frame) error { delivered++; return nil }
			err := decoder.feed(chunkLine(scenario.id, 0, 1, len(logical), logical), consume)
			if scenario.want {
				if err != nil || delivered != 1 || len(decoder.seenChunks) != 2 || !decoder.seenChunks[scenario.id] || decoder.seenChunkIDBytes != beforeBytes+len(scenario.id) {
					t.Fatalf("valid ledger bytes: delivered=%d retained=%d bytes=%d error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, err)
				}
				if scenario.remaining != len(scenario.id) {
					return
				}
				beforeBytes = decoder.seenChunkIDBytes
				delivered = 0
				err = decoder.feed(chunkLine("next", 0, 1, len(logical), logical), consume)
			}
			wantEntries := 1
			if scenario.want {
				wantEntries++
			}
			if !errors.Is(err, errProtocol) || !strings.Contains(err.Error(), "chunk ID ledger too large") || delivered != 0 || len(decoder.seenChunks) != wantEntries || !decoder.seenChunks["old"] || decoder.seenChunks["next"] || (!scenario.want && decoder.seenChunks[scenario.id]) || decoder.seenChunkIDBytes != beforeBytes || decoder.chunkID != "" || decoder.chunkData != nil {
				t.Fatalf("ledger byte overflow: delivered=%d retained=%d bytes=%d active=%q error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, decoder.chunkID, err)
			}
		})
	}
}

func TestChunkLedgerCumulativeValidIDsAndHistoricalReplay(t *testing.T) {
	logical := []byte(`{"type":"advisor_cost_changed"}`)
	decoder := frameDecoder{negotiated: true}
	delivered := 0
	consume := func(frame) error { delivered++; return nil }
	suffix := strings.Repeat("x", maxChunkIDBytes-8)
	count := maxChunkLedgerBytes / maxChunkIDBytes
	if maxChunkLedgerBytes%maxChunkIDBytes != 0 || count >= maxChunkIDs {
		t.Fatal("cumulative-byte fixture must reach its exact bound before the entry bound")
	}
	for index := range count {
		// Stream one physical frame at a time, rather than allocating the fixture.
		id := fmt.Sprintf("%08d", index) + suffix
		if err := decoder.feed(chunkLine(id, 0, 1, len(logical), logical), consume); err != nil {
			t.Fatalf("valid ID %d: %v", index, err)
		}
		if decoder.seenChunkIDBytes != (index+1)*maxChunkIDBytes || len(decoder.seenChunks) != index+1 {
			t.Fatalf("ID %d: retained=%d bytes=%d", index, len(decoder.seenChunks), decoder.seenChunkIDBytes)
		}
	}
	if err := decoder.close(); err != nil {
		t.Fatal(err)
	}
	if delivered != count || decoder.seenChunkIDBytes != maxChunkLedgerBytes {
		t.Fatalf("exact byte bound: delivered=%d retained=%d bytes=%d", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes)
	}
	delivered = 0
	err := decoder.feed(chunkLine("next", 0, 1, len(logical), logical), consume)
	if !errors.Is(err, errProtocol) || !strings.Contains(err.Error(), "chunk ID ledger too large") || delivered != 0 || len(decoder.seenChunks) != count || decoder.seenChunks["next"] || decoder.seenChunkIDBytes != maxChunkLedgerBytes || decoder.chunkID != "" || decoder.chunkData != nil {
		t.Fatalf("cumulative overflow: delivered=%d retained=%d bytes=%d active=%q error=%v", delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, decoder.chunkID, err)
	}
	for _, index := range []int{0, count / 2, count - 1} {
		id := fmt.Sprintf("%08d", index) + suffix
		err = decoder.feed(chunkLine(id, 0, 1, len(logical), logical), consume)
		if !errors.Is(err, errProtocol) || !strings.Contains(err.Error(), "replayed") || delivered != 0 || !decoder.seenChunks[id] || len(decoder.seenChunks) != count || decoder.seenChunkIDBytes != maxChunkLedgerBytes {
			t.Fatalf("historical ID %d: delivered=%d retained=%d bytes=%d error=%v", index, delivered, len(decoder.seenChunks), decoder.seenChunkIDBytes, err)
		}
	}
}
