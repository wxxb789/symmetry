// Package omp implements Oh My Pi's independently versioned RPC transport.
package omp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const maxFrameBytes = 1 << 20
const maxLogicalBytes = 64 << 20

var errProtocol = errors.New("invalid OMP RPC protocol")

type frame struct {
	kind   string
	fields map[string]json.RawMessage
	raw    json.RawMessage
}

func parseFrame(data []byte) (frame, error) {
	if !utf8.Valid(data) {
		return frame{}, fmt.Errorf("%w: invalid UTF-8", errProtocol)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(decoder); err != nil {
		return frame{}, fmt.Errorf("%w: %v", errProtocol, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return frame{}, fmt.Errorf("%w: trailing JSON", errProtocol)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return frame{}, fmt.Errorf("%w: expected object", errProtocol)
	}
	var kind string
	if err := required(fields, "type", &kind); err != nil || strings.TrimSpace(kind) == "" {
		return frame{}, fmt.Errorf("%w: missing frame type", errProtocol)
	}
	return frame{kind: kind, fields: fields, raw: data}, nil
}

// uniqueValue rejects duplicate fields at every nesting level before a DTO can
// discard them. encoding/json alone otherwise accepts conflicting identities.
func uniqueValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate or invalid object field")
			}
			seen[name] = true
			if err := uniqueValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected closing delimiter")
	}
	_, err = decoder.Token()
	return err
}

func required(fields map[string]json.RawMessage, name string, value any) error {
	data, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("%w: missing %s", errProtocol, name)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("%w: invalid %s", errProtocol, name)
	}
	return nil
}

type frameDecoder struct {
	buffer                              []byte
	chunkID                             string
	chunkCount, chunkIndex, chunkLength int
	chunkData                           []byte
	seenChunks                          map[string]bool
	negotiated                          bool
}

func (decoder *frameDecoder) feed(data []byte, consume func(frame) error) error {
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			if len(data) > maxFrameBytes-1-len(decoder.buffer) {
				return fmt.Errorf("%w: physical frame too large", errProtocol)
			}
			decoder.buffer = append(decoder.buffer, data...)
			return nil
		}
		if end > maxFrameBytes-1-len(decoder.buffer) {
			return fmt.Errorf("%w: physical frame too large", errProtocol)
		}
		decoder.buffer = append(decoder.buffer, data[:end]...)
		data = data[end+1:]
		line := bytes.TrimSuffix(decoder.buffer, []byte{'\r'})
		if len(bytes.TrimSpace(line)) == 0 {
			return fmt.Errorf("%w: empty frame", errProtocol)
		}
		value, err := parseFrame(line)
		decoder.buffer = nil // The emitted frame owns this exact JSON byte slice.
		if err != nil {
			return err
		}
		logical, err := decoder.reassemble(value)
		if err != nil {
			return err
		}
		if logical != nil {
			if err := consume(*logical); err != nil {
				return err
			}
		}
	}
	return nil
}

func (decoder *frameDecoder) reassemble(value frame) (*frame, error) {
	if value.kind != "rpc_chunk" {
		if decoder.chunkID != "" {
			return nil, fmt.Errorf("%w: interrupted chunk sequence", errProtocol)
		}
		return &value, nil
	}
	if !decoder.negotiated {
		return nil, fmt.Errorf("%w: chunk before v2 negotiation", errProtocol)
	}
	var id, encoded string
	var index, count, length int
	for _, field := range []struct {
		name  string
		value any
	}{{"chunkId", &id}, {"index", &index}, {"count", &count}, {"byteLength", &length}, {"data", &encoded}} {
		if err := required(value.fields, field.name, field.value); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(id) == "" || count <= 0 || count > length || length <= 0 || length > maxLogicalBytes || index < 0 || index >= count {
		return nil, fmt.Errorf("%w: invalid chunk metadata", errProtocol)
	}
	if decoder.chunkID == "" {
		if index != 0 || decoder.seenChunks[id] {
			return nil, fmt.Errorf("%w: replayed or unordered chunk", errProtocol)
		}
		decoder.chunkID, decoder.chunkCount, decoder.chunkLength = id, count, length
		decoder.chunkData = make([]byte, 0, length)
	}
	if id != decoder.chunkID || count != decoder.chunkCount || length != decoder.chunkLength || index != decoder.chunkIndex {
		return nil, fmt.Errorf("%w: interleaved chunk sequence", errProtocol)
	}
	if strings.ContainsAny(encoded, "\r\n") {
		return nil, fmt.Errorf("%w: noncanonical base64", errProtocol)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > length-len(decoder.chunkData) {
		return nil, fmt.Errorf("%w: invalid chunk data", errProtocol)
	}
	decoder.chunkData = append(decoder.chunkData, decoded...)
	decoder.chunkIndex++
	if decoder.chunkIndex != count {
		return nil, nil
	}
	if len(decoder.chunkData) != length {
		return nil, fmt.Errorf("%w: chunk length mismatch", errProtocol)
	}
	logical, err := parseFrame(decoder.chunkData)
	if err != nil {
		return nil, err
	}
	if logical.kind == "rpc_chunk" {
		return nil, fmt.Errorf("%w: nested chunk", errProtocol)
	}
	if decoder.seenChunks == nil {
		decoder.seenChunks = make(map[string]bool)
	}
	decoder.seenChunks[id] = true
	decoder.chunkID, decoder.chunkData, decoder.chunkIndex = "", nil, 0
	return &logical, nil
}

func (decoder *frameDecoder) close() error {
	if len(decoder.buffer) != 0 || decoder.chunkID != "" {
		return fmt.Errorf("%w: incomplete output", errProtocol)
	}
	return nil
}
