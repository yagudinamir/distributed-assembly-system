// +build !solution

package filecache

import (
	"fmt"
	"gitlab.com/slon/shad-go/distbuild/pkg/build"
	"golang.org/x/sync/singleflight"

	"io/ioutil"
	"net/http"

	"go.uber.org/zap"
)

type Handler struct {
	l *zap.Logger
	c *Cache
	g *singleflight.Group
}

func (h *Handler) LogError(err error) {
	if err != nil {
		panic(err)
	}
}

func NewHandler(l *zap.Logger, cache *Cache) *Handler {
	return &Handler{l: l, c: cache, g: &singleflight.Group{}}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/file", h.FileHandler)
}

func (h *Handler) FileHandler(w http.ResponseWriter, r *http.Request) {
	id := build.ID{}
	_ = id.UnmarshalText([]byte(r.URL.Query().Get("id")))

	switch r.Method {
	case http.MethodGet:
		path, unlock, err := h.c.Get(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
		} else {
			content, err := ioutil.ReadFile(path)
			_, _ = w.Write(content)
			unlock()
			h.LogError(err)
		}
	case http.MethodPut:
		content, err := ioutil.ReadAll(r.Body)
		h.LogError(err)

		h.l.Info("file received")

		fn := func() (interface{}, error) {
			writeCloser, _, er := h.c.Write(id)
			if er == nil {
				_, e := writeCloser.Write(content)
				h.LogError(e)
				e = writeCloser.Close()
				h.LogError(e)
			}
			return nil, err
		}
		_, err, _ = h.g.Do(id.String(), fn)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(err.Error()))
		}
	default:
		h.LogError(fmt.Errorf("unknown request method"))
	}
}
