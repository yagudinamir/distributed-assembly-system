// +build !solution

package filecache

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)

var (
	ErrNotFound    = errors.New("file not found")
	ErrExists      = errors.New("file exists")
	ErrWriteLocked = errors.New("file is locked for write")
	ErrReadLocked  = errors.New("file is locked for read")
)

type Cache struct {
	root     string
	mu       *sync.RWMutex
	canWrite chan bool
	ids      map[build.ID]bool
	reading  int32
}

func New(rootDir string) (*Cache, error) {
	err := os.MkdirAll(rootDir, 0777)
	if err != nil {
		panic(err)
	}
	c := Cache{root: rootDir, mu: &sync.RWMutex{}, canWrite: make(chan bool, 1), ids: make(map[build.ID]bool), reading: 0}
	c.canWrite <- true
	return &c, nil
}

func (c *Cache) GetRoot() string {
	return c.root
}

func (c *Cache) Range(fileFn func(file build.ID) error) error {
	c.mu.RLock()
	atomic.AddInt32(&c.reading, 1)

	defer func() {
		atomic.AddInt32(&c.reading, -1)
		c.mu.RUnlock()
	}()

	for file := range c.ids {
		if err := fileFn(file); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) Remove(file build.ID) error {
	if atomic.LoadInt32(&c.reading) > 0 {
		return ErrReadLocked
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(c.root, file.String())
	os.RemoveAll(path)
	delete(c.ids, file)
	return nil
}

type FileWriteCloser struct {
	file   *os.File
	w      *bufio.Writer
	commit func() error
}

func (f *FileWriteCloser) Write(p []byte) (int, error) {
	return f.w.Write(p)
}

func (f *FileWriteCloser) Close() error {
	if err := f.w.Flush(); err != nil {
		return err
	}
	err := f.file.Close()
	_ = f.commit()
	return err
}

func (c *Cache) Write(file build.ID) (w io.WriteCloser, abort func() error, err error) {
	select {
	case <-c.canWrite:
	default:
		return nil, nil, ErrWriteLocked
	}
	c.mu.Lock()
	//_, ok := c.ids[file] //todo
	//if ok {
	//	c.canWrite <- true
	//	c.mu.Unlock()
	//	fmt.Println("EXISTS")
	//	return nil, nil, ErrExists
	//}
	c.ids[file] = true

	finished := false

	commit := func() error {
		if finished {
			return nil
		}
		finished = true
		c.canWrite <- true
		c.mu.Unlock()
		return nil
	}

	path := filepath.Join(c.root, file.String())

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0777)
	if err != nil {
		panic(err)
	}

	writeCloser := &FileWriteCloser{file: f, w: bufio.NewWriter(f), commit: commit}

	abort = func() error {
		if finished {
			return nil
		}
		finished = true
		f.Close()
		os.RemoveAll(path)
		c.canWrite <- true
		delete(c.ids, file)
		c.mu.Unlock()
		return nil
	}

	return writeCloser, abort, nil
}

func (c *Cache) Get(file build.ID) (path string, unlock func(), err error) {
	c.mu.RLock()
	atomic.AddInt32(&c.reading, 1)

	unlock = func() {
		atomic.AddInt32(&c.reading, -1)
		c.mu.RUnlock()
	}
	_, ok := c.ids[file]
	if !ok {
		unlock()
		return "", nil, ErrNotFound
	}

	return filepath.Join(c.root, file.String()), unlock, nil
}
