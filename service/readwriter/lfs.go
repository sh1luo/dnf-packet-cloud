package readwriter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"packet_cloud/biz/model/hertz/packet"
)

// LocalFileSystem supports one process and one instance per file. The mutex
// cannot coordinate other writers; external edits are detected by file metadata,
// but must not run concurrently with mutations. IDs remain monotonic for the
// lifetime of the instance; reopening derives the high-water mark from the file.
type LocalFileSystem struct {
	mu       sync.Mutex
	path     string
	snapshot []*packet.CloudPacket
	info     os.FileInfo
	maxID    int32
}

func NewLocalFileSystem(path string) (*LocalFileSystem, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("packets file path is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s := &LocalFileSystem{path: path}
	if err := s.loadLocked(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *LocalFileSystem) Close() error { return nil }

func (s *LocalFileSystem) loadLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.path == "" {
		return errors.New("file storage must be initialized with NewLocalFileSystem")
	}
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.snapshot = []*packet.CloudPacket{}
		s.info = nil
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("packets path is not a regular file: %s", s.path)
	}
	if s.info != nil && os.SameFile(info, s.info) &&
		info.Size() == s.info.Size() && info.ModTime().Equal(s.info.ModTime()) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var packets []*packet.CloudPacket
	if err := json.Unmarshal(b, &packets); err != nil {
		return fmt.Errorf("decode packets: %w: %v", ErrCorruptStorage, err)
	}
	if packets == nil {
		return fmt.Errorf("%w: file must contain a JSON array", ErrCorruptStorage)
	}
	if err := validatePackets(packets); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptStorage, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.snapshot, s.info = packets, info
	s.updateMaxLocked(packets)
	return nil
}

func (s *LocalFileSystem) updateMaxLocked(packets []*packet.CloudPacket) {
	for _, p := range packets {
		if p.Id > s.maxID {
			s.maxID = p.Id
		}
	}
}

func (s *LocalFileSystem) List(ctx context.Context) ([]*packet.CloudPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(ctx); err != nil {
		return nil, err
	}
	out := make([]*packet.CloudPacket, len(s.snapshot))
	for i, p := range s.snapshot {
		out[i] = packetMetadata(p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out, nil
}

func (s *LocalFileSystem) Get(ctx context.Context, id int32) (*packet.CloudPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(ctx); err != nil {
		return nil, err
	}
	for _, p := range s.snapshot {
		if p.Id == id {
			return proto.Clone(p).(*packet.CloudPacket), nil
		}
	}
	return nil, ErrNotFound
}

func (s *LocalFileSystem) Append(ctx context.Context, packets []*packet.CloudPacket) ([]int32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePackets(packets); err != nil {
		return nil, err
	}
	in := clonePackets(packets)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(ctx); err != nil {
		return nil, err
	}
	if int64(len(in)) > int64(math.MaxInt32)-int64(s.maxID) {
		return nil, ErrIDExhausted
	}
	ids := make([]int32, len(in))
	if len(in) == 0 {
		return ids, nil
	}
	for i, p := range in {
		p.Id = s.maxID + int32(i+1)
		ids[i] = p.Id
	}
	next := make([]*packet.CloudPacket, 0, len(s.snapshot)+len(in))
	next = append(next, s.snapshot...)
	next = append(next, in...)
	if err := s.saveLocked(ctx, next); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *LocalFileSystem) DeleteRange(ctx context.Context, start, end int32) ([]int32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if start > end {
		return nil, fmt.Errorf("%w: range start exceeds end", ErrInvalidPacket)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(ctx); err != nil {
		return nil, err
	}
	ids := make([]int32, 0)
	next := make([]*packet.CloudPacket, 0, len(s.snapshot))
	for _, p := range s.snapshot {
		if p.Id >= start && p.Id <= end {
			ids = append(ids, p.Id)
		} else {
			next = append(next, p)
		}
	}
	if len(ids) > 0 {
		if err := s.saveLocked(ctx, next); err != nil {
			return nil, err
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func (s *LocalFileSystem) ReadPacket() ([]*packet.CloudPacket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(context.Background()); err != nil {
		return nil, err
	}
	return clonePackets(s.snapshot), nil
}

// SavePacket is a legacy administrative replacement, not a request mutation.
func (s *LocalFileSystem) SavePacket(packets []*packet.CloudPacket) error {
	if err := validatePackets(packets); err != nil {
		return err
	}
	in := clonePackets(packets)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(context.Background()); err != nil {
		return err
	}
	return s.saveLocked(context.Background(), in)
}

func (s *LocalFileSystem) saveLocked(ctx context.Context, packets []*packet.CloudPacket) error {
	b, err := json.Marshal(packets)
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if s.info != nil {
		mode = s.info.Mode().Perm()
	}
	info, err := atomicReplace(ctx, s.path, b, mode)
	if err != nil {
		return err
	}
	s.snapshot, s.info = packets, info
	s.updateMaxLocked(packets)
	return nil
}

// Backup makes an immediate durable copy. Scheduling belongs to the caller.
func (s *LocalFileSystem) Backup() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := context.Background()
	if err := s.loadLocked(ctx); err != nil {
		return err
	}
	b, err := json.Marshal(s.snapshot)
	if err != nil {
		return err
	}
	path := s.path + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000")
	_, err = atomicReplace(ctx, path, b, 0600)
	return err
}

func atomicReplace(ctx context.Context, path string, data []byte, mode os.FileMode) (os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(mode); err != nil {
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return nil, err
	}
	// Rename is the commit point. Directory sync is best effort: an error after
	// commit must not be reported as a failed mutation that callers could retry.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return info, nil
}
