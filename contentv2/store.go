// store.go
package main

import (
	"context"
	"fmt"

	"conch-content/client"

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
	lockClient *client.LockClient
}

// NewStore creates a coordinated content store.
func NewStore(hostRoot, mergedRoot, nodeID string, lockClient *client.LockClient) (*Store, error) {
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
		lockClient: lockClient,
	}, nil
}

func (s *Store) Info(ctx context.Context, dgst digest.Digest) (content.Info, error) {
	return s.readStore.Info(ctx, dgst)
}

func (s *Store) ReaderAt(ctx context.Context, desc ocispec.Descriptor) (content.ReaderAt, error) {
	return s.readStore.ReaderAt(ctx, desc)
}

// TODO
func (s *Store) Writer(ctx context.Context, opts ...content.WriterOpt) (content.Writer, error) {
	var wOpts content.WriterOpts
	for _, opt := range opts {
		if err := opt(&wOpts); err != nil {
			return nil, err
		}
	}

	dgst := wOpts.Desc.Digest
	if err := dgst.Validate(); err != nil {
		return nil, fmt.Errorf("invalid digest in descriptor: %w", err)
	}

	resourceID := dgst.String()
	req := &client.Request{
		Type:       client.OperationTypePull,
		ResourceID: resourceID,
		NodeID:     s.nodeID,
	}

	fmt.Printf("[writer]digest=%s\n", resourceID)
	result, err := client.ClusterLock(ctx, s.lockClient, req)

	if err != nil {
		return nil, fmt.Errorf("distributed lock failed for %s: %w", resourceID, err)
	}

	// 检查是否有错误
	if result.Error != nil {
		return nil, fmt.Errorf("distributed lock error for %s: %w", resourceID, result.Error)
	}

	// 如果获得锁，需要该节点真实写入
	if result.Acquired {
		fmt.Printf("获得锁 resourceID=%q, nodeID=%q\n", resourceID, s.nodeID)
		w, err := s.writeStore.Writer(ctx, opts...)
		if err != nil {
			req.Error = err.Error()
			// Success 会在客户端自动根据 Error 推断，不需要手动设置
			_ = client.ClusterUnLock(ctx, s.lockClient, req)
			return nil, err
		}
		return &distributedWriter{
			writer:     w,
			lockClient: s.lockClient,
			request:    req,
			digest:     dgst,
		}, nil
	}

	// 理论上不应该到达这里，因为 waitForLock 会一直等待直到获得锁
	return nil, fmt.Errorf("unexpected lock result: acquired=%v", result.Acquired)
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
