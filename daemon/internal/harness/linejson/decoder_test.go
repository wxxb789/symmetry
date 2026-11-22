package linejson

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

var (
	errTooLarge   = errors.New("test record exceeds limit")
	errIncomplete = errors.New("test stream ended with an incomplete record")
	errFatal      = errors.New("test record is fatal")
)

type lineRecord struct {
	Sequence uint64
	Line     string
	Failure  error
}

func policyFor(maxBytes int) Policy[lineRecord] {
	return Policy[lineRecord]{
		MaxBytes:   maxBytes,
		TooLarge:   func() error { return errTooLarge },
		Incomplete: func() error { return errIncomplete },
		Decode: func(sequence uint64, line []byte) lineRecord {
			return lineRecord{Sequence: sequence, Line: string(line)}
		},
	}
}

func feedLines(t *testing.T, decoder *Decoder[lineRecord], chunks ...string) []lineRecord {
	t.Helper()
	var records []lineRecord
	for _, chunk := range chunks {
		decoded, err := decoder.Feed([]byte(chunk))
		if err != nil {
			t.Fatalf("Feed(%q) error = %v", chunk, err)
		}
		records = append(records, decoded...)
	}
	return records
}

func recordLines(records []lineRecord) []string {
	lines := make([]string, len(records))
	for index, record := range records {
		lines[index] = fmt.Sprintf("%d:%s", record.Sequence, record.Line)
	}
	return lines
}

func wantLines(t *testing.T, records []lineRecord, want ...string) {
	t.Helper()
	if got := recordLines(records); !slices.Equal(got, want) {
		t.Fatalf("records = %q, want %q", got, want)
	}
}

func TestFeedFramesLinesAcrossChunksUnderTheBound(t *testing.T) {
	policy := policyFor(8)
	decoder := New(policy)
	records := feedLines(t, decoder, "ab", "cdefgh\n12345678\n", "x")
	wantLines(t, records, "1:abcdefgh", "2:12345678")
	records = feedLines(t, decoder, "\n")
	wantLines(t, records, "3:x")
}

func TestFeedBoundAdmitsCRLFOnlyUnderTheAllowance(t *testing.T) {
	tests := []struct {
		name      string
		allowance bool
		trim      bool
		chunks    []string
		want      []string
		wantErr   error
	}{
		{name: "line at the bound", chunks: []string{"1234\n"}, want: []string{"1:1234"}},
		{name: "line over the bound", chunks: []string{"12345\n"}, wantErr: errTooLarge},
		{name: "bound spans the previous chunk", chunks: []string{"12", "345\n"}, wantErr: errTooLarge},
		{name: "cr counts without the allowance", chunks: []string{"1234\r\n"}, wantErr: errTooLarge},
		{name: "allowance admits the cr", allowance: true, chunks: []string{"1234\r\n"}, want: []string{"1:1234\r"}},
		{name: "allowance trims the cr", allowance: true, trim: true, chunks: []string{"1234\r\n"}, want: []string{"1:1234"}},
		{name: "allowance stops two bytes over", allowance: true, trim: true, chunks: []string{"12345\r\n"}, wantErr: errTooLarge},
		{name: "allowance needs the cr before the lf", allowance: true, trim: true, chunks: []string{"1234X\n"}, wantErr: errTooLarge},
		{name: "allowance reads the cr from the next chunk", allowance: true, trim: true, chunks: []string{"1234", "\r\n"}, want: []string{"1:1234"}},
		{name: "no allowance on an unterminated line", allowance: true, trim: true, chunks: []string{"1234\r"}, wantErr: errTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := policyFor(4)
			policy.CRAllowance = test.allowance
			policy.TrimCR = test.trim
			decoder := New(policy)
			var records []lineRecord
			var err error
			for _, chunk := range test.chunks {
				decoded, feedErr := decoder.Feed([]byte(chunk))
				records = append(records, decoded...)
				if feedErr != nil {
					err = feedErr
					break
				}
			}
			if test.wantErr == nil && err != nil {
				t.Fatalf("Feed() error = %v, want none", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("Feed() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil {
				wantLines(t, records, test.want...)
			}
		})
	}
}

func TestFeedZeroBoundAdmitsOnlyAnEmptyLine(t *testing.T) {
	if _, err := New(policyFor(0)).Feed([]byte("x\n")); !errors.Is(err, errTooLarge) {
		t.Fatalf("zero-bound Feed() error = %v, want errTooLarge", err)
	}
	records := feedLines(t, New(policyFor(0)), "\n")
	wantLines(t, records, "1:")
}

func TestFeedBlankPolicySkipsOrDeliversLines(t *testing.T) {
	skipping := policyFor(8)
	skipping.SkipBlank = true
	records := feedLines(t, New(skipping), "\n", "  \n", "x\n")
	wantLines(t, records, "1:x")

	records = feedLines(t, New(policyFor(8)), "  \n")
	wantLines(t, records, "1:  ")
}

func TestFeedLatchKeepsTheFirstFailureAndTheRetainedSuffix(t *testing.T) {
	latching := policyFor(4)
	latching.Latch = true
	decoder := New(latching)
	records, err := decoder.Feed([]byte("12345\n1234\n"))
	if !errors.Is(err, errTooLarge) || len(records) != 0 {
		t.Fatalf("Feed() = %#v, %v; want no records and the bound error", records, err)
	}
	if records, err = decoder.Feed([]byte("1234\n")); err != errTooLarge || len(records) != 0 {
		t.Fatalf("Feed() after failure = %#v, %v; want the same bound error", records, err)
	}

	decoder = New(policyFor(4))
	records, err = decoder.Feed([]byte("12345\nrest\n"))
	if !errors.Is(err, errTooLarge) || len(records) != 0 {
		t.Fatalf("Feed() = %#v, %v; want no records and the bound error", records, err)
	}
	records, err = decoder.Feed(nil)
	if err != nil {
		t.Fatalf("Feed() of the retained suffix error = %v", err)
	}
	wantLines(t, records, "1:rest")

	records, err = decoder.Feed([]byte("123456"))
	if !errors.Is(err, errTooLarge) || len(records) != 0 {
		t.Fatalf("Feed() = %#v, %v; want no records and the bound error", records, err)
	}
	wantLines(t, feedLines(t, decoder, "ab\n"), "2:ab")
}

func TestFeedReturnsRecordsCompletedBeforeTheBoundFailure(t *testing.T) {
	for _, chunkFirst := range []bool{false, true} {
		policy := policyFor(4)
		policy.ChunkFirst = chunkFirst
		records, err := New(policy).Feed([]byte("ab\ncdefgh\n"))
		if !errors.Is(err, errTooLarge) {
			t.Fatalf("ChunkFirst=%t: Feed() error = %v, want %v", chunkFirst, err, errTooLarge)
		}
		wantLines(t, records, "1:ab")
	}
}

func TestFeedFatalRecordIsReturnedAndFailsTheStream(t *testing.T) {
	for _, latch := range []bool{false, true} {
		t.Run(fmt.Sprintf("latch=%t", latch), func(t *testing.T) {
			policy := policyFor(8)
			policy.Latch = latch
			policy.Decode = func(sequence uint64, line []byte) lineRecord {
				record := lineRecord{Sequence: sequence, Line: string(line)}
				if record.Line == "bad" {
					record.Failure = errFatal
				}
				return record
			}
			policy.Fatal = func(record lineRecord) error { return record.Failure }
			decoder := New(policy)
			records, err := decoder.Feed([]byte("bad\nlater\n"))
			wantLines(t, records, "1:bad")
			if !errors.Is(err, errFatal) {
				t.Fatalf("Feed() error = %v, want %v", err, errFatal)
			}
			if records, err = decoder.Feed([]byte("x\n")); !errors.Is(err, errFatal) || len(records) != 0 {
				t.Fatalf("Feed() after a fatal record = %#v, %v; want the same error", records, err)
			}
		})
	}
}

func TestCloseReportsThePendingSuffixPerPolicy(t *testing.T) {
	skipping := policyFor(8)
	skipping.SkipBlank = true
	decoder := New(skipping)
	feedLines(t, decoder, "ab")
	sequence, pending, err := decoder.Close()
	if sequence != 1 || string(pending) != "ab" || !errors.Is(err, errIncomplete) {
		t.Fatalf("Close() = %d, %q, %v; want the pending suffix", sequence, pending, err)
	}
	if sequence, pending, err = decoder.Close(); sequence != 0 || pending != nil || err != nil {
		t.Fatalf("second Close() = %d, %q, %v; want an empty suffix", sequence, pending, err)
	}

	decoder = New(skipping)
	feedLines(t, decoder, "  ")
	if sequence, pending, err = decoder.Close(); sequence != 0 || pending != nil || err != nil {
		t.Fatalf("blank Close() = %d, %q, %v; want no record", sequence, pending, err)
	}

	decoder = New(skipping)
	feedLines(t, decoder, "  ab")
	if _, pending, _ = decoder.Close(); string(pending) != "  ab" {
		t.Fatalf("pending = %q, want the untrimmed suffix", pending)
	}

	latched := policyFor(8)
	latched.SkipBlank = true
	latched.Latch = true
	decoder = New(latched)
	feedLines(t, decoder, "ab")
	if _, _, err = decoder.Close(); !errors.Is(err, errIncomplete) {
		t.Fatalf("Close() error = %v, want %v", err, errIncomplete)
	}
	if _, err = decoder.Feed([]byte("x\n")); !errors.Is(err, errIncomplete) {
		t.Fatalf("Feed() after Close error = %v, want %v", err, errIncomplete)
	}

	keeping := policyFor(8)
	decoder = New(keeping)
	feedLines(t, decoder, "ab")
	if sequence, pending, err = decoder.Close(); sequence != 1 || string(pending) != "ab" || !errors.Is(err, errIncomplete) {
		t.Fatalf("Close() = %d, %q, %v; want the pending suffix", sequence, pending, err)
	}
	wantLines(t, feedLines(t, decoder, "cd\n"), "2:cd")

	decoder = New(keeping)
	feedLines(t, decoder, "  ")
	if sequence, pending, err = decoder.Close(); sequence != 1 || string(pending) != "  " || !errors.Is(err, errIncomplete) {
		t.Fatalf("Close() = %d, %q, %v; want every suffix byte to be a record", sequence, pending, err)
	}
}
