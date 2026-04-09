package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/savaki/promised-lan/internal/nm"
)

//go:embed assets/index.html
var assetFS embed.FS

// NewHandler builds the promised-lan http.Handler wired to the given nm.Client.
func NewHandler(client nm.Client, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{client: client, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /scan", h.scan)
	mux.HandleFunc("POST /credentials", h.credentials)
	mux.HandleFunc("GET /", h.index)
	return mux
}

type handler struct {
	client nm.Client
	logger *slog.Logger
}

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	sub, err := fs.Sub(assetFS, "assets")
	if err != nil {
		writeJSONError(w, 500, "asset fs")
		return
	}
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		writeJSONError(w, 500, "asset read")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.client.Status(r.Context())
	if err != nil {
		h.logger.Error("status failed", "err", err)
		writeJSONError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, newStatusResp(st))
}

func (h *handler) scan(w http.ResponseWriter, r *http.Request) {
	nets, err := h.client.Scan(r.Context())
	if err != nil {
		h.logger.Error("scan failed", "err", err)
		writeJSONError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, newScanResp(nets))
}

func (h *handler) credentials(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeJSONError(w, 415, "content-type must be application/json")
		return
	}
	var req CredsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, 400, "invalid json")
		return
	}
	if l := len(req.SSID); l < 1 || l > 32 {
		writeJSONError(w, 400, "ssid length must be 1..32")
		return
	}
	if l := len(req.PSK); l < 8 || l > 63 {
		writeJSONError(w, 400, "psk length must be 8..63")
		return
	}

	st, err := h.client.UpdateAndActivate(r.Context(), req.SSID, req.PSK)
	// Do NOT log req.PSK — even on error.
	resp := CredsResp{Status: newStatusResp(st)}
	if err == nil {
		resp.OK = true
		resp.Message = "connected"
		writeJSON(w, 200, resp)
		return
	}

	var nerr *nm.NMError
	if errors.As(err, &nerr) {
		resp.Message = nerr.Kind.Error()
	} else {
		resp.Message = err.Error()
	}
	h.logger.Warn("credentials update failed", "kind", fmt.Sprintf("%v", nerr), "ssid", req.SSID)
	writeJSON(w, 200, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrResp{Error: msg})
}
