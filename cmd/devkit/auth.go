package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The dashboard can be read by anyone on the tailnet; acting needs the password set with
// devkit web password. Only a salted PBKDF2 hash is stored, in the host config. A signed-in
// browser holds a cookie signed with a host secret and the current password hash, so changing
// the password signs every browser out.

const (
	passwordRounds = 600_000
	sessionCookie  = "devkit_session"
	sessionLife    = 7 * 24 * time.Hour
)

var b64 = base64.RawURLEncoding

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordRounds, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

func checkPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	salt, err1 := b64.DecodeString(parts[2])
	want, err2 := b64.DecodeString(parts[3])
	if err != nil || err1 != nil || err2 != nil || rounds < 1 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, rounds, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

// readConfigFile returns the host config as generic JSON, keeping sections devkit web password doesn't know.
func readConfigFile() (map[string]any, error) {
	data, err := os.ReadFile(configPath())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeObject(data)
}

// passwordHash is the stored hash, or "" when no password is set.
func passwordHash() string {
	cfg, err := readConfigFile()
	if err != nil {
		return ""
	}
	return str(object(cfg, "web"), "password_hash")
}

// sessionSecret is the host secret that signs session cookies; it is created on first use.
func sessionSecret() ([]byte, error) {
	vc, err := loadVaultConfig()
	if err != nil {
		return nil, err
	}
	if vc.Provider == "openbao" {
		value, err := readVaultReference("openbao://devkit/system/web-session-signing-key")
		if err != nil {
			return nil, err
		}
		key, err := base64.RawStdEncoding.DecodeString(value)
		if err != nil || len(key) < 32 {
			return nil, fail("Invalid vault-backed session key")
		}
		return key, nil
	}
	path := filepath.Join(configDir(), "web-secret")
	if data, err := os.ReadFile(path); err == nil && len(data) >= 32 {
		return data, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return nil, err
	}
	return secret, os.WriteFile(path, secret, 0o600)
}

func sign(secret []byte, payload, hash string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload + "\x00" + hash))
	return b64.EncodeToString(mac.Sum(nil))
}

func newSession(secret []byte, hash string, now time.Time) string {
	nonce := make([]byte, 16)
	rand.Read(nonce)
	expiry := make([]byte, 8)
	binary.BigEndian.PutUint64(expiry, uint64(now.Add(sessionLife).Unix()))
	payload := b64.EncodeToString(append(expiry, nonce...))
	return payload + "." + sign(secret, payload, hash)
}

func validSession(secret []byte, hash, value string, now time.Time) bool {
	payload, mac, ok := strings.Cut(value, ".")
	if !ok || hash == "" || !hmac.Equal([]byte(mac), []byte(sign(secret, payload, hash))) {
		return false
	}
	raw, err := b64.DecodeString(payload)
	if err != nil || len(raw) < 8 {
		return false
	}
	return now.Unix() < int64(binary.BigEndian.Uint64(raw[:8]))
}

// signedIn reports whether the request carries a valid session for the current password.
func signedIn(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	secret, err := sessionSecret()
	return err == nil && validSession(secret, passwordHash(), cookie.Value, time.Now())
}

// sameOrigin rejects requests another site's page could make: the browser's Origin must be this host.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" && fromProxy(r) {
		host = forwarded
	}
	return origin == "https://"+host || origin == "http://"+host
}

// fromProxy is true for requests relayed by Traefik on this host.
func fromProxy(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip == "127.0.0.1" || ip == "::1"
}

// clientIP is the browser's address: Traefik's X-Forwarded-For when relayed, the peer otherwise.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" && fromProxy(r) {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// loginLimiter locks an address out for a while after repeated wrong passwords.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

const (
	loginFailures = 5
	loginWindow   = 5 * time.Minute
)

func (l *loginLimiter) recent(ip string, now time.Time) []time.Time {
	var kept []time.Time
	for _, t := range l.failures[ip] {
		if now.Sub(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	l.failures[ip] = kept
	return kept
}

func (l *loginLimiter) blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, now)) >= loginFailures
}

func (l *loginLimiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[ip] = append(l.recent(ip, now), now)
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true,
		Secure: r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
}

// readSecret reads a line from the terminal without echoing it; piped input is read as is.
func readSecret(prompt string, in *bufio.Reader) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if isTerminal(os.Stdin) {
		stty := func(arg string) {
			cmd := exec.Command("stty", arg)
			cmd.Stdin = os.Stdin
			cmd.Run()
		}
		stty("-echo")
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt)
		go func() {
			if _, ok := <-interrupt; ok {
				stty("echo")
				fmt.Fprintln(os.Stderr)
				os.Exit(130)
			}
		}()
		defer func() {
			signal.Stop(interrupt)
			close(interrupt)
			stty("echo")
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// setWebPassword is devkit web password: it stores a new password hash, signing every browser out.
func setWebPassword() error {
	in := bufio.NewReader(os.Stdin)
	password, err := readSecret("New dashboard password: ", in)
	if err != nil {
		return err
	}
	if len(password) < 10 {
		return fail("Use at least 10 characters.")
	}
	again, err := readSecret("Repeat it: ", in)
	if err != nil {
		return err
	}
	if again != password {
		return fail("The passwords don't match.")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	cfg, err := readConfigFile()
	if err != nil {
		return err
	}
	ensureObject(cfg, "web")["password_hash"] = hash
	if err := writeConfigFile(cfg); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Dashboard password set. Browsers signed in before are signed out.")
	return nil
}
