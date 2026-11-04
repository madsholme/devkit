package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	vms := newVMInventory()
	go reapLoop()
	actions := newActionHandler()
	actions.jobs.after = func(p plannedAction) {
		if p.target == "vm lifecycle" {
			vms.invalidate()
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain := "text/plain; charset=utf-8"
		if a, ok := assets[r.URL.Path]; ok && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			send(w, http.StatusOK, a.body, a.kind, a.cache)
			return
		}
		if actions.handles(r.URL.Path) {
			actions.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			send(w, http.StatusNotFound, []byte("Not found"), "text/plain", "no-store")
			return
		}
		query := r.URL.Query()
		var err error
		switch r.URL.Path {
		case "/api/containers":
			var data containersData
			if data, err = containersOverview(r.Context()); err == nil {
				sendJSON(w, data)
			}
		case "/api/projects":
			var data projectsData
			if data, err = projectsOverview(); err == nil {
				sendJSON(w, data)
			}
		case "/api/vms":
			sendJSON(w, vms.snapshot(r.Context()))
		case "/api/overview":
			var data overviewData
			if data, err = overview(); err == nil {
				sendJSON(w, data)
			}
		case "/api/history":
			limit, _ := strconv.Atoi(query.Get("limit"))
			if limit < 1 || limit > 1000 {
				limit = 300
			}
			var events []event
			if events, err = readHistory(query.Get("preview"), limit); err == nil {
				sendJSON(w, map[string]any{"events": events})
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

// actionHandler serves signing in and out, and the actions a signed-in browser may start.
type actionHandler struct {
	limiter *loginLimiter
	jobs    *jobs
}

func newActionHandler() *actionHandler {
	return &actionHandler{&loginLimiter{failures: map[string][]time.Time{}}, &jobs{list: map[string]*job{}}}
}

func (h *actionHandler) handles(path string) bool {
	switch path {
	case "/api/session", "/api/login", "/api/logout", "/api/action", "/api/job", "/api/pair":
		return true
	}
	return false
}

func sendError(w http.ResponseWriter, code int, message string) {
	send(w, code, []byte(message), "text/plain; charset=utf-8", "no-store")
}

// session describes what the page may offer: whether a password is set, whether this browser is
// signed in, and the share options of this host.
func session(r *http.Request) map[string]any {
	result := map[string]any{"password_set": passwordHash() != "", "signed_in": signedIn(r), "share": nil, "pair": false}
	if cfg, err := loadHostConfig(); err == nil && cfg.Harness != nil && cfg.Harness.Name == t3Harness.Name && cfg.Harness.Host != "" {
		result["pair"] = true
	}
	if cfg, ok, err := loadShareConfig(); err == nil && ok {
		result["share"] = map[string]any{"domain": cfg.Domain, "prefix": *cfg.Prefix, "hosts": append([]string{}, cfg.Hosts...),
			"default_hours": cfg.DefaultHours, "max_hours": cfg.MaxHours}
	}
	return result
}

func (h *actionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/session" && r.Method == http.MethodGet {
		sendJSON(w, session(r))
		return
	}
	if r.URL.Path == "/api/job" && r.Method == http.MethodGet {
		if !signedIn(r) {
			sendError(w, http.StatusUnauthorized, "Sign in first.")
			return
		}
		j := h.jobs.get(r.URL.Query().Get("id"))
		if j == nil {
			sendError(w, http.StatusNotFound, "No such job.")
			return
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("since"))
		output, next, done, ok := j.since(offset)
		sendJSON(w, map[string]any{"title": j.Title, "output": output, "next": next, "done": done, "ok": ok})
		return
	}
	if r.Method != http.MethodPost {
		sendError(w, http.StatusMethodNotAllowed, "Use POST.")
		return
	}
	// Only the dashboard's own page may post; the session cookie is SameSite=Strict as well.
	if !sameOrigin(r) {
		sendError(w, http.StatusForbidden, "Cross-site request refused.")
		return
	}
	var body struct {
		Password string `json:"password"`
		actionRequest
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil && r.URL.Path != "/api/logout" {
		sendError(w, http.StatusBadRequest, "Send JSON.")
		return
	}
	switch r.URL.Path {
	case "/api/login":
		ip := clientIP(r)
		if h.limiter.blocked(ip, time.Now()) {
			sendError(w, http.StatusTooManyRequests, "Too many wrong passwords; try again in a few minutes.")
			return
		}
		hash := passwordHash()
		if hash == "" {
			sendError(w, http.StatusConflict, "No password is set; run devkit web password on the host.")
			return
		}
		if !checkPassword(body.Password, hash) {
			h.limiter.fail(ip, time.Now())
			time.Sleep(500 * time.Millisecond)
			sendError(w, http.StatusUnauthorized, "Wrong password.")
			return
		}
		h.limiter.reset(ip)
		secret, err := sessionSecret()
		if err != nil {
			sendError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setSessionCookie(w, r, newSession(secret, hash, time.Now()), int(sessionLife.Seconds()))
		sendJSON(w, map[string]any{"signed_in": true})
	case "/api/logout":
		setSessionCookie(w, r, "", -1)
		sendJSON(w, map[string]any{"signed_in": false})
	case "/api/action":
		if !signedIn(r) {
			sendError(w, http.StatusUnauthorized, "Sign in first.")
			return
		}
		plan, err := planAction(body.actionRequest)
		if err != nil {
			sendError(w, http.StatusBadRequest, err.Error())
			return
		}
		j, err := h.jobs.start(plan)
		if err != nil {
			sendError(w, http.StatusConflict, err.Error())
			return
		}
		fmt.Printf("devkit web: %s started %q\n", clientIP(r), j.Title)
		sendJSON(w, map[string]any{"id": j.ID, "title": j.Title})
	case "/api/pair":
		// Not a job: the pairing link is a secret, so it goes to this page only and is not kept.
		if !signedIn(r) {
			sendError(w, http.StatusUnauthorized, "Sign in first.")
			return
		}
		out, err := runPair()
		if err != nil {
			sendError(w, http.StatusBadGateway, err.Error())
			return
		}
		fmt.Printf("devkit web: %s paired a device with T3 Code\n", clientIP(r))
		send(w, http.StatusOK, out, "application/json", "no-store")
	}
}

// runPair runs devkit pair --json as the dashboard; tests replace it.
var runPair = func() ([]byte, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(self, "pair", "--json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Env = withEnv(os.Environ(), map[string]string{"DEVKIT_BY": "dashboard"})
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stderr.String()), "devkit: "))
		if message == "" {
			message = err.Error()
		}
		return nil, fail("%s", message)
	}
	return stdout.Bytes(), nil
}

// reapLoop removes expired shares every minute, sweeps leftover Cloudflare resources every ten
// and, unless the host config turns it off, prunes Docker once a day, starting an hour in.
func reapLoop() {
	for tick := 0; ; tick++ {
		removed, err := reapExpired(time.Now())
		for _, r := range removed {
			fmt.Printf("devkit web: unshared expired %s\n", r)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "devkit web: expiring shares: %v\n", err)
		}
		if tick%10 == 0 {
			if cfg, ok, err := loadShareConfig(); err == nil && ok {
				swept, err := sweepOrphans(cfg, 10*time.Minute)
				for _, r := range swept {
					fmt.Printf("devkit web: removed leftover %s\n", r)
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "devkit web: sweeping shares: %v\n", err)
				}
			}
		}
		if tick%(24*60) == 60 && dailyPrune() {
			if err := prune(true); err != nil {
				failed("prune", "", err)
				fmt.Fprintf(os.Stderr, "devkit web: pruning Docker: %v\n", err)
			}
		}
		time.Sleep(time.Minute)
	}
}

// dailyPrune says whether the web service prunes Docker by itself; it reads the config each time
// so a change takes effect without a restart.
func dailyPrune() bool {
	cfg, err := loadHostConfig()
	return err == nil && *cfg.Prune.Daily
}

// serve runs the web overview. Bind to localhost; Traefik publishes it on the tailnet.
func serve(host string, port int) error {
	// What this process does by itself, such as expiring shares, shows as "auto" in the history.
	actor = "auto"
	handler, err := newHandler()
	if err != nil {
		return err
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	fmt.Printf("devkit web on http://%s\n", address)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	return server.Serve(listener)
}
