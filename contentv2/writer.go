package main

import (
	"context"
	"fmt"

	"conch-content/client"

	"github.com/containerd/containerd/content"
	"github.com/opencontainers/go-digest"
)

// distributedWriter wraps a local content.Writer and adds distributed locking semantics.
// - If isOwner=true: acts as a normal writer, and releases the lock on Commit/Close.
// - If isOwner=false: all write operations are no-ops; Commit succeeds immediately.
type distributedWriter struct {
	writer     content.Writer
	lockClient *client.LockClient
	request    *client.Request
	digest     digest.Digest
	err        string
}

func (dw *distributedWriter) Write(p []byte) (int, error) {
	return dw.writer.Write(p)
}

func (dw *distributedWriter) Close() error {
	var closeErr error
	if dw.writer != nil {
		closeErr = dw.writer.Close()
	}
	// unlock in Close()
	if dw.lockClient != nil && dw.request != nil {
		dw.request.Error = dw.err
		fmt.Printf("解锁 resourceID=%q, nodeID=%q\n", dw.request.ResourceID, dw.request.NodeID)
		_ = client.ClusterUnLock(context.Background(), dw.lockClient, dw.request)
	}
	return closeErr
}

func (dw *distributedWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...content.Opt) error {
	if dw.writer == nil {
		return fmt.Errorf("underlying writer is nil")
	}
	commitErr := dw.writer.Commit(ctx, size, expected, opts...)
	if commitErr != nil {
		dw.err = commitErr.Error()
	} else {
		dw.err = ""
	}
	return commitErr
}

func (dw *distributedWriter) Digest() digest.Digest {
	return dw.digest
}

func (dw *distributedWriter) Truncate(size int64) error {
	if dw.writer == nil {
		return fmt.Errorf("underlying writer is nil")
	}
	return dw.writer.Truncate(size)
}

func (dw *distributedWriter) Status() (content.Status, error) {
	if dw.writer == nil {
		return content.Status{}, fmt.Errorf("underlying writer is nil")
	}
	return dw.writer.Status()
}

// Compile-time check
var _ content.Writer = (*distributedWriter)(nil)
