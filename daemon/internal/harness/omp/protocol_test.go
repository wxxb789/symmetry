package omp

import (
	"encoding/base64"
	"encoding/json"
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
