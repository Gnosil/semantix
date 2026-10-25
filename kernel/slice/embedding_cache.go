package slice

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Backfill caches are deliberately separate from the slice journal. Two CLI
// processes may hold stale store snapshots; neither may overwrite the other's
// stats/content/deletions just to save a derived vector. Content-addressed keys
// make stale cache writes harmless. Removing this directory only loses speed.
func embeddingCacheName(id, source, contentHash string) string {
	h := sha256.Sum256([]byte(id + "\x00" + source + "\x00" + contentHash))
	return hex.EncodeToString(h[:]) + ".json"
}

func (s *fileStore) embeddingCacheRoot(create bool) (*os.Root, error) {
	s.mu.Lock()
	closed, readOnly, path := s.closed, s.readOnly, s.path+".embeddings"
	s.mu.Unlock()
	if closed {
		return nil, os.ErrClosed
	}
	if create {
		if readOnly {
			return nil, fmt.Errorf("slice: read-only store")
		}
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("slice: invalid embedding cache directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("slice: embedding cache directory changed")
	}
	return root, nil
}

func (s *fileStore) ReadCachedEmbedding(id, source, contentHash string) ([]byte, error) {
	root, err := s.embeddingCacheRoot(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := embeddingCacheName(id, source, contentHash)
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("slice: invalid embedding cache entry")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("slice: embedding cache entry changed")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 8<<20 {
		return nil, fmt.Errorf("slice: oversized embedding cache entry")
	}
	return raw, nil
}

func (s *fileStore) CacheEmbedding(id string, content []byte, vector []float32, model, source, contentHash string) error {
	h := sha256.Sum256(content)
	if hex.EncodeToString(h[:]) != contentHash {
		return fmt.Errorf("slice: embedding content hash mismatch")
	}
	raw, err := json.Marshal(vector)
	if err != nil {
		return err
	}
	root, err := s.embeddingCacheRoot(true)
	if err != nil {
		return err
	}
	defer root.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := ".tmp-" + hex.EncodeToString(nonce[:])
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, embeddingCacheName(id, source, contentHash))
}
