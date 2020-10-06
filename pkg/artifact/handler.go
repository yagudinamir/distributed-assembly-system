// +build !solution

package artifact

import (
	"gitlab.com/slon/shad-go/distbuild/pkg/build"
	"gitlab.com/slon/shad-go/distbuild/pkg/tarstream"
	"net/http"

	"go.uber.org/zap"
)

type Handler struct {
	l *zap.Logger
	c *Cache
}

func NewHandler(l *zap.Logger, c *Cache) *Handler {
	return &Handler{l: l, c: c}
}

func (h *Handler) LogError(err error) {
	if err != nil {
		panic(err)
	}
}

func (h *Handler) ArtifactHandler(w http.ResponseWriter, r *http.Request) {
	artifact := build.ID{}
	_ = artifact.UnmarshalText([]byte(r.URL.Query().Get("id")))

	path, unlock, err := h.c.Get(artifact)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
	} else {
		err := tarstream.Send(path, w)
		unlock()
		LogError(err)
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/artifact", h.ArtifactHandler)
}
