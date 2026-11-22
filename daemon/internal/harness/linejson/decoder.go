// Package linejson implements the bounded LF-delimited line framing shared by
// the harness adapter transports. It owns the buffer, the byte bound, line
// splitting, sequencing and close semantics; each adapter owns what a line
// means and which errors it reports.
package linejson

import "bytes"

// Policy selects one adapter's framing behaviour.
type Policy[R any] struct {
	// MaxBytes bounds one raw line, counted before any CR trim. Required.
	MaxBytes int

	// CRAllowance admits a line of MaxBytes+1 bytes when its last byte is CR.
	CRAllowance bool

	// TrimCR removes one trailing CR before Decode sees the line.
	TrimCR bool

	// SkipBlank drops whitespace-only lines, and treats a whitespace-only
	// close suffix as empty.
	SkipBlank bool

	// ChunkFirst copies the whole incoming chunk into the buffer before the
	// bound decides; otherwise the bound is applied to each line before its
	// bytes are copied.
	ChunkFirst bool

	// Latch reports the first bound or incomplete failure from every later
	// call.
	Latch bool

	// TooLarge builds the error reported for a line over the bound.
	TooLarge func() error

	// Incomplete builds the error reported for an unterminated suffix.
	Incomplete func() error

	// Decode builds the record for one framed line. The line is a view into
	// the decoder buffer and must not be retained.
	Decode func(sequence uint64, line []byte) R

	// Fatal reports a decoded record that must fail the stream; the record is
	// still returned and the failure is permanent.
	Fatal func(record R) error
}

// Decoder buffers one transport's output into bounded LF-delimited lines and
// hands each line to the policy's Decode callback.
type Decoder[R any] struct {
	policy   Policy[R]
	buffer   []byte
	sequence uint64
	failed   error
}

// New binds one policy. MaxBytes must be positive.
func New[R any](policy Policy[R]) *Decoder[R] {
	return &Decoder[R]{policy: policy}
}

// Feed appends one transport chunk and returns the records it completed. On a
// failure it returns the records completed so far with the policy's error.
func (decoder *Decoder[R]) Feed(chunk []byte) ([]R, error) {
	if decoder.failed != nil {
		return nil, decoder.failed
	}
	if decoder.policy.ChunkFirst {
		decoder.buffer = append(decoder.buffer, chunk...)
		chunk = nil
	}
	var records []R
	for {
		lineEnd := bytes.IndexByte(decoder.buffer, '\n')
		if lineEnd < 0 {
			chunkEnd := bytes.IndexByte(chunk, '\n')
			if chunkEnd < 0 {
				if len(decoder.buffer)+len(chunk) > decoder.policy.MaxBytes {
					decoder.buffer = nil
					return records, decoder.fail(decoder.policy.TooLarge())
				}
				decoder.buffer = append(decoder.buffer, chunk...)
				break
			}
			lineBytes := len(decoder.buffer) + chunkEnd
			if decoder.overBound(lineBytes, trailerBeforeLF(decoder.buffer, chunk, chunkEnd)) {
				if !decoder.policy.Latch {
					decoder.buffer = append(decoder.buffer[:0], chunk[chunkEnd+1:]...)
				} else {
					decoder.buffer = nil
				}
				return records, decoder.fail(decoder.policy.TooLarge())
			}
			decoder.buffer = append(decoder.buffer, chunk[:chunkEnd+1]...)
			chunk = chunk[chunkEnd+1:]
			continue
		}
		if decoder.overBound(lineEnd, trailerBeforeLF(nil, decoder.buffer, lineEnd)) {
			if !decoder.policy.Latch {
				decoder.buffer = decoder.buffer[lineEnd+1:]
			} else {
				decoder.buffer = nil
			}
			return records, decoder.fail(decoder.policy.TooLarge())
		}
		line := decoder.buffer[:lineEnd]
		decoder.buffer = decoder.buffer[lineEnd+1:]
		if decoder.policy.TrimCR && len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if decoder.policy.SkipBlank && len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		decoder.sequence++
		record := decoder.policy.Decode(decoder.sequence, line)
		records = append(records, record)
		if decoder.policy.Fatal != nil {
			if err := decoder.policy.Fatal(record); err != nil {
				decoder.buffer = nil
				decoder.failed = err
				return records, err
			}
		}
	}
	return records, nil
}

// Close reports an unterminated suffix as its sequence number and raw bytes.
// pending is nil when the suffix is empty under SkipBlank; err is a latched
// earlier failure or the policy's Incomplete error.
func (decoder *Decoder[R]) Close() (sequence uint64, pending []byte, err error) {
	if decoder.failed != nil {
		return 0, nil, decoder.failed
	}
	suffix := decoder.buffer
	if len(suffix) == 0 || (decoder.policy.SkipBlank && len(bytes.TrimSpace(suffix)) == 0) {
		decoder.buffer = nil
		return 0, nil, nil
	}
	decoder.sequence++
	pending = append([]byte(nil), suffix...)
	decoder.buffer = nil
	err = decoder.policy.Incomplete()
	if decoder.policy.Latch {
		decoder.failed = err
	}
	return decoder.sequence, pending, err
}

func (decoder *Decoder[R]) fail(err error) error {
	if decoder.policy.Latch {
		decoder.failed = err
	}
	return err
}

func (decoder *Decoder[R]) overBound(lineBytes int, trailer byte) bool {
	if lineBytes <= decoder.policy.MaxBytes {
		return false
	}
	return !(decoder.policy.CRAllowance && lineBytes == decoder.policy.MaxBytes+1 && trailer == '\r')
}

func trailerBeforeLF(prefix, chunk []byte, lineEnd int) byte {
	if lineEnd > 0 {
		return chunk[lineEnd-1]
	}
	if len(prefix) > 0 {
		return prefix[len(prefix)-1]
	}
	return 0
}
