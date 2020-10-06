// +build !solution

package artifact

import (
	"context"
	"fmt"
	"gitlab.com/slon/shad-go/distbuild/pkg/tarstream"
	"net/http"

	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)

func LogError(err error) {
	//
}

// Download artifact from remote cache into local cache.
func Download(ctx context.Context, endpoint string, c *Cache, artifactID build.ID) error {
	bytes, err := artifactID.MarshalText()
	LogError(err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+fmt.Sprintf("/artifact?id=%s", string(bytes)), nil)
	if err != nil {
		LogError(err)
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		LogError(err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		err = fmt.Errorf("%s", bytes)
		return err
	}
	path, commit, abort, err := c.Create(artifactID)
	LogError(err)
	if err == ErrExists {
		return nil
	}
	if err := tarstream.Receive(path, resp.Body); err != nil {
		_ = abort()
		LogError(err)
		return err
	}
	_ = commit()
	return nil
}
