package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"github.com/steenfeldt/devkit/web"
)

type asset struct {
	body  []byte
	kind  string
	cache string
}

func loadAssets() (map[string]asset, error) {
	assets := map[string]asset{}
	for path, file := range map[string]struct{ name, kind, cache string }{
		"/":                     {"index.html", "text/html; charset=utf-8", "no-store"},
		"/logo.svg":             {"logo.svg", "image/svg+xml", "max-age=3600"},
		"/favicon.ico":          {"favicon.ico", "image/x-icon", "max-age=3600"},
		"/apple-touch-icon.png": {"apple-touch-icon.png", "image/png", "max-age=3600"},
	} {
		body, err := web.Files.ReadFile(file.name)
		if err != nil {
			return nil, err
		}
		// Browsers keep favicons in their own cache and may skip no-store responses.
		assets[path] = asset{body, file.kind, file.cache}
	}
	return assets, nil
}

func send(w http.ResponseWriter, code int, body []byte, kind, cache string) {
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", cache)
	w.WriteHeader(code)
	w.Write(body)
}

func sendJSON(w http.ResponseWriter, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		send(w, http.StatusInternalServerError, []byte(err.Error()), "text/plain; charset=utf-8", "no-store")
		return
	}
	send(w, http.StatusOK, body, "application/json", "no-store")
}

// optional returns a query parameter, or nil when it is absent (an empty value is kept).
func optional(values map[string][]string, key string) *string {
	if v, ok := values[key]; ok && len(v) > 0 {
		return &v[0]
	}
	return nil
}

// stream relays docker logs --follow until the browser disconnects.
func stream(w http.ResponseWriter, r *http.Request, args []string) {
	cmd := exec.CommandContext(r.Context(), args[0], args[1:]...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		send(w, http.StatusBadRequest, []byte(err.Error()), "text/plain; charset=utf-8", "no-store")
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		send(w, http.StatusBadRequest, []byte(err.Error()), "text/plain; charset=utf-8", "no-store")
		return
	}
	defer cmd.Wait()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 65536)
	for {
		n, err := out.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
	}
}

// previewLogs returns the recent log of a preview container, stdout and stderr combined.
func previewLogs(name string, svc *string) (string, error) {
	id, err := previewContainer(name, svc)
	if err != nil {
		return "", err
	}
	args := logsCommand(id, false)
	out, _ := exec.Command(args[0], args[1:]...).CombinedOutput()
	return string(out), nil
}

func newHandler() (http.Handler, error) {
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	res := newResources()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain := "text/plain; charset=utf-8"
		if a, ok := assets[r.URL.Path]; ok && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			send(w, http.StatusOK, a.body, a.kind, a.cache)
			return
		}
		if r.Method != http.MethodGet {
			send(w, http.StatusNotFound, []byte("Not found"), "text/plain", "no-store")
			return
		}
		query := r.URL.Query()
		var err error
		switch r.URL.Path {
		case "/api/overview":
			var data overviewData
			if data, err = overview(); err == nil {
				sendJSON(w, data)
			}
		case "/api/resources":
			sendJSON(w, res.snapshot(query.Get("full") == "1"))
		case "/api/container":
			var details map[string]any
			if details, err = containerDetails(query.Get("preview"), optional(query, "service")); err == nil {
				sendJSON(w, details)
			}
		case "/api/logs":
			name, svc := query.Get("preview"), optional(query, "service")
			if query.Get("follow") == "1" {
				var id string
				if id, err = previewContainer(name, svc); err == nil {
					stream(w, r, logsCommand(id, true))
				}
			} else {
				var text string
				if text, err = previewLogs(name, svc); err == nil {
					send(w, http.StatusOK, []byte(text), plain, "no-store")
				}
			}
		default:
			send(w, http.StatusNotFound, []byte("Not found"), "text/plain", "no-store")
		}
		if err != nil {
			send(w, http.StatusBadRequest, []byte(err.Error()), plain, "no-store")
		}
	}), nil
}

// serve runs the read-only web overview. Bind to localhost; Traefik publishes it on the tailnet.
func serve(host string, port int) error {
	handler, err := newHandler()
	if err != nil {
		return err
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	fmt.Printf("devbox web on http://%s\n", address)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return server.Serve(listener)
}
