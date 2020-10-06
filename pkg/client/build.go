// +build !solution

package client

import (
	"context"
	"fmt"
	"gitlab.com/slon/shad-go/distbuild/pkg/api"
	"gitlab.com/slon/shad-go/distbuild/pkg/filecache"
	"path/filepath"

	"go.uber.org/zap"

	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)

type Client struct {
	l           *zap.Logger
	apiEndpoint string
	sourceDir   string
}

func NewClient(
	l *zap.Logger,
	apiEndpoint string,
	sourceDir string,
) *Client {
	return &Client{
		l:           l,
		apiEndpoint: apiEndpoint,
		sourceDir:   sourceDir,
	}
}

func (c *Client) LogError(err error) {
	if err != nil {
		panic(err)
	}
}

type BuildListener interface {
	OnJobStdout(jobID build.ID, stdout []byte) error
	OnJobStderr(jobID build.ID, stderr []byte) error

	OnJobFinished(jobID build.ID) error
	OnJobFailed(jobID build.ID, code int, error string) error
}

func (c *Client) Build(ctx context.Context, graph build.Graph, lsn BuildListener) error {
	buildRequest := &api.BuildRequest{Graph: graph}
	buildClient := api.NewBuildClient(c.l, c.apiEndpoint)

	buildStarted, statusReader, err := buildClient.StartBuild(ctx, buildRequest)
	defer statusReader.Close()
	c.LogError(err)
	c.l.Info("build started")
	c.l.Info(buildStarted.ID.String())

	filecacheClient := filecache.NewClient(c.l.Named("filecache"), c.apiEndpoint)
	graphID := buildStarted.ID
	for _, missingFileID := range buildStarted.MissingFiles {
		missingFile := graph.SourceFiles[missingFileID]
		err = filecacheClient.Upload(ctx, missingFileID, filepath.Join(c.sourceDir, missingFile))
		if err != nil {
			panic(err)
		}
	}

	_, err = buildClient.SignalBuild(ctx, graphID, &api.SignalRequest{})
	if err != nil {
		panic(err)
	}

	for {
		statusUpdate, err := statusReader.Next()
		c.l.Info("got status update")
		c.LogError(err)
		if statusUpdate.JobFinished != nil {
			c.l.Info("JOB FINISHED UPDATE")
			jobResult := statusUpdate.JobFinished
			id := jobResult.ID
			if jobResult.Error != nil {
				c.l.Info("job failed")
				_ = lsn.OnJobFailed(id, jobResult.ExitCode, *jobResult.Error)
				continue
			}
			if len(jobResult.Stderr) > 0 {
				c.l.Info("job stderr")
				_ = lsn.OnJobStderr(id, jobResult.Stderr)
			}
			if len(jobResult.Stdout) > 0 {
				c.l.Info("job stdout")
				_ = lsn.OnJobStdout(id, jobResult.Stdout)
			}
			_ = lsn.OnJobFinished(id)
		} else if statusUpdate.BuildFinished != nil {
			c.l.Info("BUILD FINISHED UPDATE")
			break
		} else if statusUpdate.BuildFailed != nil {
			return fmt.Errorf(statusUpdate.BuildFailed.Error)
		} else {
			panic("Unreachable")
		}
	}
	c.l.Info("BUILD RETURN")
	return nil
}
