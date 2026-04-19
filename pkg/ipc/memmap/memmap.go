// Package memmap provides memory-mapped file utilities for Hyperspace IPC.
// Files are mapped read-write (or read-only) using the mmap syscall.
// This allows multiple processes to share memory regions efficiently.
package memmap

import (
	"fmt"
	"os"
	"path/filepath"

	atomicbuf "github.com/cloud-jumpgate/hyperspace/internal/atomic"
	"golang.org/x/sys/unix"
)

// syscall hooks — overridable in tests to trigger error paths.
var (
	sysFtruncate = func(fd int, size int64) error { return unix.Ftruncate(fd, size) }
	sysMmap      = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		return unix.Mmap(fd, offset, length, prot, flags)
	}
	sysMunmap = func(b []byte) error { return unix.Munmap(b) }
	sysMsync  = func(b []byte, flags int) error { return unix.Msync(b, flags) }
)

// MappedFile represents a memory-mapped file.
type MappedFile struct {
	data     []byte
	file     *os.File
	size     int64
	readOnly bool
}

// Create creates a new file of the given size and maps it read-write.
// The file is pre-allocated (ftruncate) to the given size.
// Parent directories are created if they do not exist.
func Create(path string, size int64) (*MappedFile, error) {
	if size <= 0 {
		return nil, fmt.Errorf("memmap.Create: size must be positive, got %d", size)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("memmap.Create: mkdir %s: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- operator-controlled path from configuration, not user input
	if err != nil {
		return nil, fmt.Errorf("memmap.Create: open %s: %w", path, err)
	}

	if err := sysFtruncate(int(f.Fd()), size); err != nil { // #nosec G115 -- fd is a valid file descriptor from os.OpenFile
		f.Close()
		return nil, fmt.Errorf("memmap.Create: ftruncate %s: %w", path, err)
	}

	data, err := sysMmap(int(f.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED) // #nosec G115 -- fd and size are validated above
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("memmap.Create: mmap %s: %w", path, err)
	}

	return &MappedFile{
		data:     data,
		file:     f,
		size:     size,
		readOnly: false,
	}, nil
}

// Open maps an existing file read-write.
func Open(path string) (*MappedFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600) // #nosec G304 -- operator-controlled path from configuration, not user input
	if err != nil {
		return nil, fmt.Errorf("memmap.Open: open %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("memmap.Open: stat %s: %w", path, err)
	}

	size := info.Size()
	if size == 0 {
		f.Close()
		return nil, fmt.Errorf("memmap.Open: file %s has zero size", path)
	}

	data, err := sysMmap(int(f.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED) // #nosec G115 -- fd is uintptr from os.File.Fd(), safe to convert to int on all supported platforms
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("memmap.Open: mmap %s: %w", path, err)
	}

	return &MappedFile{
		data:     data,
		file:     f,
		size:     size,
		readOnly: false,
	}, nil
}

// OpenReadOnly maps an existing file read-only.
func OpenReadOnly(path string) (*MappedFile, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0o400) // #nosec G304 -- operator-controlled path from configuration, not user input
	if err != nil {
		return nil, fmt.Errorf("memmap.OpenReadOnly: open %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("memmap.OpenReadOnly: stat %s: %w", path, err)
	}

	size := info.Size()
	if size == 0 {
		f.Close()
		return nil, fmt.Errorf("memmap.OpenReadOnly: file %s has zero size", path)
	}

	data, err := sysMmap(int(f.Fd()), 0, int(size), unix.PROT_READ, unix.MAP_SHARED) // #nosec G115 -- fd is uintptr from os.File.Fd(), safe to convert to int on all supported platforms
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("memmap.OpenReadOnly: mmap %s: %w", path, err)
	}

	return &MappedFile{
		data:     data,
		file:     f,
		size:     size,
		readOnly: true,
	}, nil
}

// Bytes returns the full mapped byte slice.
func (m *MappedFile) Bytes() []byte {
	return m.data
}

// AtomicBuffer wraps the mapped region in an AtomicBuffer.
func (m *MappedFile) AtomicBuffer() *atomicbuf.AtomicBuffer {
	return atomicbuf.NewAtomicBuffer(m.data)
}

// Size returns the mapped size in bytes.
func (m *MappedFile) Size() int64 {
	return m.size
}

// Close unmaps and closes the file.
func (m *MappedFile) Close() error {
	var errs []error

	if m.data != nil {
		if err := sysMunmap(m.data); err != nil {
			errs = append(errs, fmt.Errorf("munmap: %w", err))
		}
		m.data = nil
	}

	if m.file != nil {
		if err := m.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("file close: %w", err))
		}
		m.file = nil
	}

	if len(errs) > 0 {
		return fmt.Errorf("memmap.Close: %v", errs)
	}
	return nil
}

// Sync flushes dirty pages to disk (msync MS_SYNC).
// For tmpfs paths (/dev/shm/...) this is a no-op at the OS level but is safe to call.
func (m *MappedFile) Sync() error {
	if m.readOnly || m.data == nil {
		return nil
	}
	if err := sysMsync(m.data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("memmap.Sync: msync: %w", err)
	}
	return nil
}
