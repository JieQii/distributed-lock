package main

import (
	"context"
	"fmt"

	"conchContent-v3/lockintegration"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/content/local"
	"github.com/containerd/containerd/errdefs"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Store implements content.Store with read-write separation:
// - Reads from 'merged' (shared global view via mergefs)
// - Writes to 'host' (local temporary storage)
// - On commit, syncs blob from host → merged to make it globally visible.
type Store struct {
	readStore  content.Store // .../merged
	writeStore content.Store // .../host
	hostRoot   string
	mergedRoot string

	nodeID     string
	lockServer string
}

// NewStore creates a coordinated content store.
func NewStore(hostRoot, mergedRoot, nodeID, lockServer string) (*Store, error) {
	writeStore, err := local.NewStore(hostRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create host write store: %w", err)
	}

	readStore, err := local.NewStore(mergedRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to create merged read store: %w", err)
	}

	return &Store{
		readStore:  readStore,
		writeStore: writeStore,
		hostRoot:   hostRoot,
		mergedRoot: mergedRoot,
		nodeID:     nodeID,
		lockServer: lockServer,
	}, nil
}

// --- content.Store interface implementation ---

func (s *Store) Info(ctx context.Context, dgst digest.Digest) (content.Info, error) {
	return s.readStore.Info(ctx, dgst)
}

func (s *Store) ReaderAt(ctx context.Context, desc ocispec.Descriptor) (content.ReaderAt, error) {
	return s.readStore.ReaderAt(ctx, desc)
}

// lockingWriter 包装底层 content.Writer，加上分布式锁生命周期
type lockingWriter struct {
	content.Writer
	lock *lockintegration.Writer
}

func (w *lockingWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...content.Opt) error {
	// 先提交底层写入
	err := w.Writer.Commit(ctx, size, expected, opts...)
	success := (err == nil)

	// 再同步到锁集成层（更新本地计数 + 释放锁）
	if commitErr := w.lock.Commit(ctx, success, err); commitErr != nil {
		if err == nil {
			return commitErr
		}
		return fmt.Errorf("blob commit err=%v, lock commit err=%v", err, commitErr)
	}
	return err
}

func (w *lockingWriter) Close() error {
	err := w.Writer.Close()
	ctx := context.Background()

	if closeErr := w.lock.Close(ctx); closeErr != nil {
		if err == nil {
			return closeErr
		}
		return fmt.Errorf("writer close err=%v, lock close err=%v", err, closeErr)
	}
	return err
}

// noopWriter 用于“跳过操作”的场景，保证 containerd 调用链不报错
type noopWriter struct{}

func (n *noopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (n *noopWriter) Close() error                { return nil }
func (n *noopWriter) Digest() digest.Digest       { return "" }
func (n *noopWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...content.Opt) error {
	return nil
}
func (n *noopWriter) Status() (content.Status, error) { return content.Status{}, nil }
func (n *noopWriter) Truncate(size int64) error       { return nil }

// Writer 实现写入逻辑 + 分布式锁集成
func (s *Store) Writer(ctx context.Context, opts ...content.WriterOpt) (content.Writer, error) {
	// 简化：这里暂时用固定 resourceID，实际使用中应从 opts/Descriptor 中提取 digest 字符串
	resourceID := "blob-unknown"

	// 1. 使用本地计数 + 锁 server 决定是否需要执行操作
	lw, err := lockintegration.OpenWriter(ctx, s.lockServer, s.nodeID, resourceID)
	if err != nil {
		return nil, fmt.Errorf("OpenWriter 失败: %w", err)
	}

	if lw.Skipped() {
		// 本地计数认为可以跳过，返回一个 no-op writer
		return &noopWriter{}, nil
	}

	// 2. 创建真正的底层 writer（写入 host）
	inner, err := s.writeStore.Writer(ctx, opts...)
	if err != nil {
		_ = lw.Close(ctx)
		return nil, err
	}

	// 3. 返回包装后的 writer，后续 Commit/Close 时会更新计数并释放锁
	return &lockingWriter{
		Writer: inner,
		lock:   lw,
	}, nil
}

func (s *Store) Abort(ctx context.Context, ref string) error {
	return s.writeStore.Abort(ctx, ref)
}

func (s *Store) Status(ctx context.Context, ref string) (content.Status, error) {
	return s.writeStore.Status(ctx, ref)
}

func (s *Store) ListStatuses(ctx context.Context, filters ...string) ([]content.Status, error) {
	return s.writeStore.ListStatuses(ctx, filters...)
}

// Delete is disabled in shared content store.
func (s *Store) Delete(ctx context.Context, dgst digest.Digest) error {
	return errdefs.ErrFailedPrecondition
}

// Update is not supported.
func (s *Store) Update(ctx context.Context, info content.Info, fieldpaths ...string) (content.Info, error) {
	return content.Info{}, errdefs.ErrNotImplemented
}

// Walk is not supported.
func (s *Store) Walk(ctx context.Context, fn content.WalkFunc, filters ...string) error {
	return errdefs.ErrNotImplemented
}
