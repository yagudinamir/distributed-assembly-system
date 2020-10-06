// +build !solution

package api

import (
	"context"
	"encoding/json"
	"gitlab.com/slon/shad-go/distbuild/pkg/build"
	"io/ioutil"
	"net/http"
	"time"

	"go.uber.org/zap"
)

func NewBuildService(l *zap.Logger, s Service) *BuildHandler { //Service - mock
	return &BuildHandler{l, s}
}

type BuildHandler struct {
	l *zap.Logger
	s Service
}

func (h *BuildHandler) LogError(err error) {
	if err != nil {
		//h.l.Error(fmt.Sprintf("%v", err))
		//log.Fatal(err)
		panic(err)
	}
}

func (h *BuildHandler) LogInfo(s string) {
	h.l.Info(s)
}

type BuildWriter struct {
	w    *http.ResponseWriter
	used bool
}

func (bw *BuildWriter) Started(rsp *BuildStarted) error {
	bw.used = true
	body, err := json.Marshal(rsp)
	if err != nil {
		panic(err)
	}
	_, err = (*bw.w).Write(append(body, '\n'))
	(*bw.w).(http.Flusher).Flush()
	return err
}

func (bw *BuildWriter) Updated(update *StatusUpdate) error {
	bw.used = true
	body, err := json.Marshal(update)
	if err != nil {
		panic(err)
	}
	_, err = (*bw.w).Write(append(body, '\n'))
	(*bw.w).(http.Flusher).Flush()
	return err
}

func (bw *BuildWriter) Used() bool {
	return bw.used
}

func (h *BuildHandler) BuildRequestHandler(w http.ResponseWriter, r *http.Request) {
	h.l.Info("Build request got")
	buildRequest := BuildRequest{}
	bytes, err := ioutil.ReadAll(r.Body)
	h.LogError(err)

	err = json.Unmarshal(bytes, &buildRequest)
	h.LogError(err)

	writer := BuildWriter{w: &w, used: false}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(time.Millisecond * 30) // todo change
		cancel()
	}()

	h.l.Info("starting build in service")
	err = h.s.StartBuild(ctx, &buildRequest, &writer) //todo context

	if !writer.Used() && err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	if err != nil {
		_ = writer.Updated(&StatusUpdate{BuildFailed: &BuildFailed{Error: err.Error()}})
	}

	w.(http.Flusher).Flush()
}

func (h *BuildHandler) SignalRequestHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	idstr := q.Get("build_id")
	buildID := build.ID{}
	err := buildID.UnmarshalText([]byte(idstr))
	h.LogError(err)

	signalRequest := SignalRequest{}
	bytes, err := ioutil.ReadAll(r.Body)
	h.LogError(err)

	err = json.Unmarshal(bytes, &signalRequest)
	h.LogError(err)

	signalResponse, err := h.s.SignalBuild(context.Background(), buildID, &signalRequest) //todo context

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	} else {
		body, err := json.Marshal(signalResponse)
		h.LogError(err)
		_, _ = w.Write(body)
	}
}

func (h *BuildHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/build", h.BuildRequestHandler)
	mux.HandleFunc("/signal", h.SignalRequestHandler)
}
