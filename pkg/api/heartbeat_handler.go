// +build !solution

package api

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type HeartbeatHandler struct {
	l *zap.Logger
	s HeartbeatService
}

func NewHeartbeatHandler(l *zap.Logger, s HeartbeatService) *HeartbeatHandler {
	return &HeartbeatHandler{l, s}
}

func (h *HeartbeatHandler) LogError(err error) {
	if err != nil {
		panic(err)
	}
}

func (h *HeartbeatHandler) RequestHandler(w http.ResponseWriter, r *http.Request) {
	heartbeatRequest := HeartbeatRequest{}
	bytes, err := ioutil.ReadAll(r.Body)
	h.LogError(err)

	err = json.Unmarshal(bytes, &heartbeatRequest)
	h.LogError(err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond * 30) // todo change
		cancel()
	}()

	heartbeatResponse, err := h.s.Heartbeat(ctx, &heartbeatRequest)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	} else {
		body, err := json.Marshal(heartbeatResponse)
		h.LogError(err)
		_, _ = w.Write(body)
	}
}

func (h *HeartbeatHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/heartbeat", h.RequestHandler)
}
