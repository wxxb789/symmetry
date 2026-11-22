package linejson

import "bytes"

type Policy[R any] struct {
	MaxBytes    int
	CRAllowance bool
	TrimCR      bool
	SkipBlank   bool
	Latch       bool
	TooLarge    func() error
	Incomplete  func() error
	// The line is a view into the decoder buffer and must not be retained.
	Decode func(sequence uint64, line []byte) R
	Fatal  func(record R) error
}

type Decoder[R any] struct {
	policy   Policy[R]
	buffer   []byte
	sequence uint64
	failed   error
}

func New[R any](policy Policy[R]) *Decoder[R] {
	return &Decoder[R]{policy: policy}
}

func (decoder *Decoder[R]) Feed(chunk []byte) ([]R, error) {
	if decoder.failed != nil {
		return nil, decoder.failed
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
				decoder.buffer = append(decoder.buffer[lineEnd+1:], chunk...)
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
	return !decoder.policy.CRAllowance || lineBytes != decoder.policy.MaxBytes+1 || trailer != '\r'
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
