// +build !solution

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"go.uber.org/zap"
)

type HeartbeatClient struct {
	l        *zap.Logger
	endpoint string
}

func NewHeartbeatClient(l *zap.Logger, endpoint string) *HeartbeatClient {
	return &HeartbeatClient{endpoint: endpoint, l: l}
}

func (c *HeartbeatClient) LogError(err error) {
	if !errors.Is(err, context.Canceled) {
		panic(err)
	}
}

func (c *HeartbeatClient) Heartbeat(ctx context.Context, heart *HeartbeatRequest) (*HeartbeatResponse, error) {
	body, err := json.Marshal(heart)
	if err != nil {
		c.LogError(err)
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/heartbeat", bytes.NewReader(body))
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

	heartbeatResponse := HeartbeatResponse{}
	err = json.Unmarshal(bytes, &heartbeatResponse)
	if err != nil {
		c.LogError(err)
		return nil, err
	}
	return &heartbeatResponse, nil
}
