// +build !solution

package filecache

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"go.uber.org/zap"

	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)

type Client struct {
	l        *zap.Logger
	endpoint string
}

func NewClient(l *zap.Logger, endpoint string) *Client {
	return &Client{l: l, endpoint: endpoint}
}

func (c *Client) LogError(err error) {
	if err != nil {
		panic(err)
	}
}

func (c *Client) Upload(ctx context.Context, id build.ID, localPath string) error {
	bytes, err := id.MarshalText()
	c.LogError(err)

	file, err := os.Open(localPath)
	if err != nil {
		c.LogError(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint+fmt.Sprintf("/file?id=%s", string(bytes)), file)
	if err != nil {
		c.LogError(err)
		return err
	}

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		c.LogError(err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		err = fmt.Errorf("%s", bytes)
		return err
	}
	return nil
}

func (c *Client) Download(ctx context.Context, localCache *Cache, id build.ID) error {
	bytes, err := id.MarshalText()
	c.LogError(err)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+fmt.Sprintf("/file?id=%s", string(bytes)), nil)
	if err != nil {
		c.LogError(err)
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.LogError(err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		err = fmt.Errorf("%s", bytes)
		return err
	}
	writeCloser, abort, err := localCache.Write(id)
	c.LogError(err)
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		_ = abort()
		c.LogError(err)
	}
	_, _ = writeCloser.Write(content)
	_ = writeCloser.Close()
	return nil
}
