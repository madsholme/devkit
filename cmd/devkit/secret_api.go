package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
)

var errSecretAPIUnavailable = errors.New("OpenBao unavailable or sealed")

func secretsAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != "POST" {
		sendError(w, 405, "Use POST.")
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	// Traefik terminates TLS on the host. A remote cleartext request cannot opt
	// itself into trust by supplying forwarded headers.
	if r.TLS == nil && (ip == nil || !ip.IsLoopback()) {
		sendError(w, 403, "Use HTTPS for API tokens.")
		return
	}
	if r.Header.Get("Origin") != "" {
		sendError(w, 403, "Browser-origin token requests are not supported.")
		return
	}
	if len(r.Header.Values("Authorization")) != 1 {
		sendError(w, 401, errSecretTokenAuth.Error())
		return
	}
	cred, err := parseSecretBearer(r.Header.Get("Authorization"))
	if err != nil {
		sendError(w, 401, errSecretTokenAuth.Error())
		return
	}
	var q secretRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&q) != nil {
		sendError(w, 400, "Invalid secret request.")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		sendError(w, 400, "Provide one JSON request.")
		return
	}
	out, err := executeSecret(context.WithValue(r.Context(), secretTokenContextKey{}, cred), q)
	if err != nil {
		status := 400
		switch {
		case errors.Is(err, errSecretTokenAuth):
			status = 401
		case errors.Is(err, errSecretTokenScope):
			status = 403
		case errors.Is(err, errSecretAPIUnavailable):
			status = 503
		}
		sendError(w, status, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
