// +build !solution

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"io/ioutil"
	"net/http"

	"gitlab.com/slon/shad-go/distbuild/pkg/build"
)


type BuildClient struct {
	endpoint string
	l        *zap.Logger
}

func NewBuildClient(l *zap.Logger, endpoint string) *BuildClient {
	return &BuildClient{endpoint: endpoint, l: l}
}

func (c *BuildClient) LogError(err error) {
	c.l.Error(fmt.Sprintf("%v", err))
	panic(err)
}

func (c *BuildClient) LogInfo(s string) {
	c.l.Info(s)
}

type BuildReader struct {
	resp        *http.Response
	bufioReader *bufio.Reader
}

func (br *BuildReader) Close() error {
	return br.resp.Body.Close()
}

func (br *BuildReader) Next() (*StatusUpdate, error) {
	bytes, err := br.bufioReader.ReadBytes('\n')
	if err == io.EOF {
		return nil, err
	}
	if err != nil {
		panic(err)
	}
	statusUpdate := StatusUpdate{}
	err = json.Unmarshal(bytes, &statusUpdate)
	if err != nil {
		panic(err)
	}
	return &statusUpdate, nil
}

func (c *BuildClient) StartBuild(ctx context.Context, request *BuildRequest) (*BuildStarted, StatusReader, error) {
	body, err := json.Marshal(request)
	if err != nil {
		c.LogError(err)
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/build", bytes.NewReader(body))
	if err != nil {
		c.LogError(err)
		return nil, nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.LogError(err)
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusBadRequest {
		bytes, e := ioutil.ReadAll(resp.Body)
		if e != nil {
			c.LogError(e)
			return nil, nil, e
		}
		err = fmt.Errorf("%s", bytes)
		resp.Body.Close()
		return nil, nil, err
	}
	reader := BuildReader{resp: resp, bufioReader: bufio.NewReader(resp.Body)}
	bytes, err := reader.bufioReader.ReadBytes('\n')
	if err != nil {
		c.LogError(err)
		return nil, nil, err
	}
	buildStarted := BuildStarted{}

	err = json.Unmarshal(bytes, &buildStarted)

	if err != nil {
		c.LogError(err)
		return nil, nil, err
	}
	return &buildStarted, &reader, nil
}

func (c *BuildClient) SignalBuild(ctx context.Context, buildID build.ID, signal *SignalRequest) (*SignalResponse, error) {
	body, err := json.Marshal(signal)
	if err != nil {
		c.LogError(err)
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+fmt.Sprintf("/signal?build_id=%s", buildID.String()), bytes.NewReader(body))
	if err != nil {
		c.LogError(err)
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.LogError(err)
		return nil, err
	}

	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		c.LogError(err)
		return nil, err
	}

	if resp.StatusCode == http.StatusBadRequest {
		err = fmt.Errorf("%s", bytes)
		return nil, err
	}
	signalResponse := SignalResponse{}
	err = json.Unmarshal(bytes, &signalResponse)
	if err != nil {
		c.LogError(err)
		return nil, err
	}
	return &signalResponse, nil
}
