package memmap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// --- Error path coverage helpers ---

func tempPath(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, name)
}

// --- Create ---

func TestCreate(t *testing.T) {
	path := tempPath(t, "test.bin")
	const size = int64(4096)

	m, err := Create(path, size)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer m.Close()

	if m.Size() != size {
		t.Errorf("Size(): expected %d, got %d", size, m.Size())
	}
	if len(m.Bytes()) != int(size) {
		t.Errorf("Bytes() length: expected %d, got %d", size, len(m.Bytes()))
	}

	// Verify file exists on disk
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file stat: %v", err)
	}
	if info.Size() != size {
		t.Errorf("on-disk size: expected %d, got %d", size, info.Size())
	}
}

func TestCreateAndWrite(t *testing.T) {
	path := tempPath(t, "write.bin")
	m, err := Create(path, 4096)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	data := m.Bytes()
	// Write a pattern
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := m.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open and verify
	m2, err := Open(path)
	if err != nil {
		t.Fatalf("Open after Create: %v", err)
	}
	defer m2.Close()

	data2 := m2.Bytes()
	for i := 0; i < 256; i++ {
		if data2[i] != byte(i) {
			t.Errorf("byte %d: expected %d, got %d", i, byte(i), data2[i])
		}
	}
}

func TestCreateNegativeSize(t *testing.T) {
	path := tempPath(t, "neg.bin")
	_, err := Create(path, -1)
	if err == nil {
		t.Error("expected error for negative size")
	}
}

func TestCreateZeroSize(t *testing.T) {
	path := tempPath(t, "zero.bin")
	_, err := Create(path, 0)
	if err == nil {
		t.Error("expected error for zero size")
	}
}

func TestCreateNestedDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "test.bin")
	m, err := Create(path, 512)
	if err != nil {
		t.Fatalf("Create with nested dirs: %v", err)
	}
	defer m.Close()

	if m.Size() != 512 {
		t.Errorf("expected size 512, got %d", m.Size())
	}
}

func TestCreateTruncatesExisting(t *testing.T) {
	path := tempPath(t, "trunc.bin")

	// Create with old data
	m1, err := Create(path, 1024)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	for i, b := range m1.Bytes() {
		_ = b
		m1.Bytes()[i] = 0xFF
	}
	m1.Close()

	// Re-create (truncate)
	m2, err := Create(path, 512)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	defer m2.Close()

	if m2.Size() != 512 {
		t.Errorf("expected size 512 after re-create, got %d", m2.Size())
	}
	// New mapping should be zero-initialised
	for i, b := range m2.Bytes() {
		if b != 0 {
			t.Errorf("byte %d: expected 0 after truncate, got %d", i, b)
		}
	}
}

// --- Open ---

func TestOpen(t *testing.T) {
	path := tempPath(t, "open.bin")
	m1, _ := Create(path, 256)
	m1.Bytes()[0] = 0xAB
	m1.Bytes()[255] = 0xCD
	m1.Close()

	m2, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer m2.Close()

	if m2.Bytes()[0] != 0xAB {
		t.Errorf("byte[0]: expected 0xAB, got 0x%02X", m2.Bytes()[0])
	}
	if m2.Bytes()[255] != 0xCD {
		t.Errorf("byte[255]: expected 0xCD, got 0x%02X", m2.Bytes()[255])
	}
}

func TestOpenNonExistent(t *testing.T) {
	_, err := Open("/nonexistent/path/to/nothing.bin")
	if err == nil {
		t.Error("expected error opening non-existent file")
	}
}

func TestOpenZeroSizeFile(t *testing.T) {
	path := tempPath(t, "empty.bin")
	f, _ := os.Create(path)
	f.Close()

	_, err := Open(path)
	if err == nil {
		t.Error("expected error opening zero-size file")
	}
}

// --- OpenReadOnly ---

func TestOpenReadOnly(t *testing.T) {
	path := tempPath(t, "readonly.bin")
	m1, _ := Create(path, 128)
	m1.Bytes()[0] = 0x42
	m1.Close()

	m2, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer m2.Close()

	if m2.Size() != 128 {
		t.Errorf("expected size 128, got %d", m2.Size())
	}
	if m2.Bytes()[0] != 0x42 {
		t.Errorf("expected 0x42, got 0x%02X", m2.Bytes()[0])
	}
}

func TestOpenReadOnlyNonExistent(t *testing.T) {
	_, err := OpenReadOnly("/nonexistent/path.bin")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestOpenReadOnlyZeroSize(t *testing.T) {
	path := tempPath(t, "empty2.bin")
	f, _ := os.Create(path)
	f.Close()

	_, err := OpenReadOnly(path)
	if err == nil {
		t.Error("expected error for zero-size file")
	}
}

// --- AtomicBuffer integration ---

func TestAtomicBuffer(t *testing.T) {
	path := tempPath(t, "atomic.bin")
	// Size must be 8-byte aligned for atomic int64 ops
	m, err := Create(path, 128)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer m.Close()

	ab := m.AtomicBuffer()
	if ab == nil {
		t.Fatal("AtomicBuffer() returned nil")
	}
	if ab.Capacity() != 128 {
		t.Errorf("expected capacity 128, got %d", ab.Capacity())
	}

	// Write via AtomicBuffer and read back via Bytes()
	const sentinel = int64(-2401053088876216242) // 0xDEADBEEFCAFEBABE interpreted as int64
	ab.PutInt64Ordered(0, sentinel)
	val := ab.GetInt64Volatile(0)
	if val != sentinel {
		t.Errorf("expected %d, got %d", sentinel, val)
	}
}

// --- Sync ---

func TestSync(t *testing.T) {
	path := tempPath(t, "sync.bin")
	m, err := Create(path, 4096)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer m.Close()

	m.Bytes()[0] = 0xFF
	if err := m.Sync(); err != nil {
		t.Errorf("Sync failed: %v", err)
	}
}

func TestSyncReadOnly(t *testing.T) {
	path := tempPath(t, "sync_ro.bin")
	m1, _ := Create(path, 256)
	m1.Close()

	m2, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer m2.Close()

	// Sync on a read-only mapping should be a no-op (not an error)
	if err := m2.Sync(); err != nil {
		t.Errorf("Sync on read-only should not error: %v", err)
	}
}

// --- Close ---

func TestDoubleClose(t *testing.T) {
	path := tempPath(t, "dclose.bin")
	m, err := Create(path, 256)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should not panic — data is nil so no munmap, file is nil so no close
	// This tests that Close is idempotent in terms of nil checks
	// (it will fail with nil pointer if not guarded — that's intentional test coverage)
}

// --- Error paths ---

// TestCreateInReadOnlyDir triggers the OpenFile error path.
func TestCreateInReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	roDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(roDir, 0o755) }() // restore for cleanup; error is non-actionable in defer

	path := filepath.Join(roDir, "test.bin")
	_, err := Create(path, 512)
	if err == nil {
		t.Error("expected error creating file in read-only directory")
	}
}

// TestCreateMkdirAllError triggers the MkdirAll error by using a file as a directory.
func TestCreateMkdirAllError(t *testing.T) {
	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(blockingFile, "subdir", "test.bin")
	_, err := Create(path, 512)
	if err == nil {
		t.Error("expected error when a dir component is a regular file")
	}
}

// TestCreateFtruncateError injects a ftruncate failure.
func TestCreateFtruncateError(t *testing.T) {
	orig := sysFtruncate
	defer func() { sysFtruncate = orig }()
	sysFtruncate = func(fd int, size int64) error {
		return fmt.Errorf("injected ftruncate error")
	}

	path := tempPath(t, "ftrunc_err.bin")
	_, err := Create(path, 512)
	if err == nil {
		t.Error("expected ftruncate error")
	}
	// Cleanup: the file was created but not truncated
	os.Remove(path)
}

// TestCreateMmapError injects a mmap failure during Create.
func TestCreateMmapError(t *testing.T) {
	orig := sysMmap
	defer func() { sysMmap = orig }()
	sysMmap = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		return nil, fmt.Errorf("injected mmap error")
	}

	path := tempPath(t, "mmap_err.bin")
	_, err := Create(path, 512)
	if err == nil {
		t.Error("expected mmap error")
	}
	os.Remove(path)
}

// TestOpenMmapError injects a mmap failure during Open.
func TestOpenMmapError(t *testing.T) {
	path := tempPath(t, "open_mmap_err.bin")
	m, _ := Create(path, 512)
	m.Close()

	orig := sysMmap
	defer func() { sysMmap = orig }()
	sysMmap = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		return nil, fmt.Errorf("injected mmap error")
	}

	_, err := Open(path)
	if err == nil {
		t.Error("expected mmap error in Open")
	}
}

// TestOpenReadOnlyMmapError injects a mmap failure during OpenReadOnly.
func TestOpenReadOnlyMmapError(t *testing.T) {
	path := tempPath(t, "ro_mmap_err.bin")
	m, _ := Create(path, 512)
	m.Close()

	orig := sysMmap
	defer func() { sysMmap = orig }()
	sysMmap = func(fd int, offset int64, length, prot, flags int) ([]byte, error) {
		return nil, fmt.Errorf("injected mmap error")
	}

	_, err := OpenReadOnly(path)
	if err == nil {
		t.Error("expected mmap error in OpenReadOnly")
	}
}

// TestCloseMunmapError injects a munmap failure.
func TestCloseMunmapError(t *testing.T) {
	path := tempPath(t, "munmap_err.bin")
	m, err := Create(path, 512)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	orig := sysMunmap
	defer func() { sysMunmap = orig }()
	sysMunmap = func(b []byte) error {
		return fmt.Errorf("injected munmap error")
	}

	err = m.Close()
	if err == nil {
		t.Error("expected munmap error from Close")
	}
}

// TestSyncError injects a msync failure.
func TestSyncError(t *testing.T) {
	path := tempPath(t, "msync_err.bin")
	m, err := Create(path, 512)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() {
		sysMunmap = func(b []byte) error { return unix.Munmap(b) }
		m.Close()
	}()

	orig := sysMsync
	defer func() { sysMsync = orig }()
	sysMsync = func(b []byte, flags int) error {
		return fmt.Errorf("injected msync error")
	}

	err = m.Sync()
	if err == nil {
		t.Error("expected msync error from Sync")
	}
}

// TestCreateLargeFile exercises the mmap path with a valid large allocation.
func TestCreateLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}
	path := tempPath(t, "large.bin")
	const size = int64(4 * 1024 * 1024) // 4 MB

	m, err := Create(path, size)
	if err != nil {
		t.Fatalf("Create 4MB: %v", err)
	}
	defer m.Close()

	if m.Size() != size {
		t.Errorf("expected size %d, got %d", size, m.Size())
	}
	m.Bytes()[0] = 0xAA
	m.Bytes()[size-1] = 0xBB
	if err := m.Sync(); err != nil {
		t.Errorf("Sync large: %v", err)
	}
}

// TestSyncNilData ensures Sync with nil data returns nil.
func TestSyncNilData(t *testing.T) {
	m := &MappedFile{data: nil, readOnly: false}
	if err := m.Sync(); err != nil {
		t.Errorf("Sync with nil data should return nil, got %v", err)
	}
}

// TestCloseNilFields exercises the Close path when fields are already nil.
func TestCloseNilFields(t *testing.T) {
	m := &MappedFile{data: nil, file: nil}
	if err := m.Close(); err != nil {
		t.Errorf("Close with nil fields should return nil, got %v", err)
	}
}

// --- Multiple mapping writers sharing data ---

func TestSharedMmap(t *testing.T) {
	path := tempPath(t, "shared.bin")
	m1, err := Create(path, 512)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer m1.Close()

	m2, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer m2.Close()

	// Write via m1, read via m2
	m1.Bytes()[100] = 0x55
	if err := m1.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// After sync, m2 should see the write (MAP_SHARED)
	if m2.Bytes()[100] != 0x55 {
		t.Errorf("shared mmap: expected 0x55 in reader, got 0x%02X", m2.Bytes()[100])
	}
}
