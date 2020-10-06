// +build !solution

package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)

var (
	ErrNotFound    = errors.New("artifact not found")
	ErrExists      = errors.New("artifact exists")
	ErrWriteLocked = errors.New("artifact is locked for write")
	ErrReadLocked  = errors.New("artifact is locked for read")
)

type Cache struct {
	root      string
	mu        *sync.RWMutex
	canCreate chan bool
	ids       map[build.ID]bool
	reading   int32
}

func NewCache(root string) (*Cache, error) {
	c := Cache{root: root, mu: &sync.RWMutex{}, canCreate: make(chan bool, 1), ids: make(map[build.ID]bool), reading: 0}
	c.canCreate <- true
	return &c, nil
}

func (c *Cache) GetRoot() string {
	return c.root
}

func (c *Cache) Range(artifactFn func(artifact build.ID) error) error {
	c.mu.RLock()
	atomic.AddInt32(&c.reading, 1)

	defer func() {
		atomic.AddInt32(&c.reading, -1)
		c.mu.RUnlock()
	}()

	for artifact := range c.ids {
		if err := artifactFn(artifact); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) Remove(artifact build.ID) error {
	if atomic.LoadInt32(&c.reading) > 0 {
		return ErrReadLocked
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(c.root, artifact.Path())
	os.RemoveAll(path)
	delete(c.ids, artifact)
	return nil
}

func (c *Cache) Create(artifact build.ID) (path string, commit, abort func() error, err error) {
	select {
	case <-c.canCreate:
	default:
		return "", nil, nil, ErrWriteLocked
	}
	c.mu.Lock()
	_, ok := c.ids[artifact]
	if ok {
		c.canCreate <- true
		c.mu.Unlock()
		return "", nil, nil, ErrExists
	}
	c.ids[artifact] = true

	commit = func() error {
		c.canCreate <- true
		c.mu.Unlock()
		return nil
	}
	path = filepath.Join(c.root, artifact.Path())

	abort = func() error {
		os.RemoveAll(path)
		c.canCreate <- true
		delete(c.ids, artifact)
		c.mu.Unlock()
		return nil
	}

	_ = os.MkdirAll(path, os.ModePerm)

	return path, commit, abort, nil
}

func (c *Cache) Get(artifact build.ID) (path string, unlock func(), err error) {
	c.mu.RLock()
	atomic.AddInt32(&c.reading, 1)

	unlock = func() {
		atomic.AddInt32(&c.reading, -1)
		c.mu.RUnlock()
	}
	_, ok := c.ids[artifact]
	if !ok {
		unlock()
		return "", nil, ErrNotFound
	}

	return filepath.Join(c.root, artifact.Path()), unlock, nil
}
