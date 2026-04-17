package logbuffer

import (
	"errors"
	"fmt"

	"github.com/cloud-jumpgate/hyperspace/internal/atomic"
)

// Size constants for the log buffer.
const (
	DefaultTermLength  = 16 * 1024 * 1024  // 16 MiB
	MinTermLength      = 64 * 1024         // 64 KiB
	MaxTermLength      = 1024 * 1024 * 1024 // 1 GiB
	LogMetaDataLength  = 4096
	NumPartitions      = 3
)

// Log meta-data field offsets (relative to start of meta-data section).
const (
	metaOffTailCounters        = 0  // int64[3] — 24 bytes
	metaOffActivePartitionIdx  = 24 // int32
	metaOffTimeOfLastStatusMsg = 28 // int64 (note: 28, not 32, to pack tightly per spec)
	metaOffCorrelationID       = 36 // int64
	metaOffInitialTermID       = 44 // int32
	metaOffDefaultFrameHdrLen  = 48 // int32
	metaOffMTULength           = 52 // int32
	metaOffTermLength          = 56 // int32
	metaOffPageSize            = 60 // int32
)

// LogBuffer manages three rotating term buffers and a metadata section.
// Layout:
//
//	[ Term 0 (termLength bytes) ]
//	[ Term 1 (termLength bytes) ]
//	[ Term 2 (termLength bytes) ]
//	[ Meta  (LogMetaDataLength bytes) ]
type LogBuffer struct {
	terms     [NumPartitions]*atomic.AtomicBuffer
	metaBuf   *atomic.AtomicBuffer
	appenders [NumPartitions]*TermAppender
	readers   [NumPartitions]*TermReader
	termLen   int
}

// New creates a LogBuffer backed by buf.
// buf must be exactly 3*termLength + LogMetaDataLength bytes.
// termLength must be a power of two between MinTermLength and MaxTermLength.
func New(buf []byte, termLength int) (*LogBuffer, error) {
	if err := validateTermLength(termLength); err != nil {
		return nil, err
	}
	required := NumPartitions*termLength + LogMetaDataLength
	if len(buf) != required {
		return nil, fmt.Errorf("logbuffer.New: buf length %d != required %d (3*%d+%d)",
			len(buf), required, termLength, LogMetaDataLength)
	}

	lb := &LogBuffer{termLen: termLength}

	for i := 0; i < NumPartitions; i++ {
		start := i * termLength
		lb.terms[i] = atomic.NewAtomicBuffer(buf[start : start+termLength])
	}
	metaStart := NumPartitions * termLength
	lb.metaBuf = atomic.NewAtomicBuffer(buf[metaStart : metaStart+LogMetaDataLength])

	// Store term length in meta so readers can discover it.
	lb.metaBuf.PutInt32LE(metaOffTermLength, int32(termLength))

	for i := 0; i < NumPartitions; i++ {
		lb.appenders[i] = NewTermAppender(lb.terms[i], lb.metaBuf, i)
		lb.readers[i] = NewTermReader(lb.terms[i])
	}

	return lb, nil
}

// validateTermLength checks that termLength is a power of two in [Min, Max].
func validateTermLength(n int) error {
	if n < MinTermLength {
		return fmt.Errorf("logbuffer: termLength %d < MinTermLength %d", n, MinTermLength)
	}
	if n > MaxTermLength {
		return fmt.Errorf("logbuffer: termLength %d > MaxTermLength %d", n, MaxTermLength)
	}
	if n&(n-1) != 0 {
		return errors.New("logbuffer: termLength must be a power of two")
	}
	return nil
}

// ActivePartitionIndex returns which of the three terms is currently active.
func (lb *LogBuffer) ActivePartitionIndex() int32 {
	return lb.metaBuf.GetInt32Volatile(metaOffActivePartitionIdx)
}

// SetActivePartitionIndex updates the active term index in the meta buffer.
func (lb *LogBuffer) SetActivePartitionIndex(idx int32) {
	lb.metaBuf.PutInt32Ordered(metaOffActivePartitionIdx, idx)
}

// InitialTermID returns the initial_term_id stored in meta.
func (lb *LogBuffer) InitialTermID() int32 {
	return lb.metaBuf.GetInt32LE(metaOffInitialTermID)
}

// SetInitialTermID writes the initial_term_id into meta.
func (lb *LogBuffer) SetInitialTermID(id int32) {
	lb.metaBuf.PutInt32LE(metaOffInitialTermID, id)
}

// TermLength returns the per-term byte size.
func (lb *LogBuffer) TermLength() int { return lb.termLen }

// TotalSize returns 3*termLength + LogMetaDataLength.
func (lb *LogBuffer) TotalSize() int { return NumPartitions*lb.termLen + LogMetaDataLength }

// Appender returns the TermAppender for partition idx.
func (lb *LogBuffer) Appender(partIdx int) *TermAppender { return lb.appenders[partIdx] }

// Reader returns the TermReader for partition idx.
func (lb *LogBuffer) Reader(partIdx int) *TermReader { return lb.readers[partIdx] }

// TermBuffer returns the raw AtomicBuffer for partition idx.
func (lb *LogBuffer) TermBuffer(partIdx int) *atomic.AtomicBuffer { return lb.terms[partIdx] }

// MetaBuffer returns the metadata AtomicBuffer.
func (lb *LogBuffer) MetaBuffer() *atomic.AtomicBuffer { return lb.metaBuf }
