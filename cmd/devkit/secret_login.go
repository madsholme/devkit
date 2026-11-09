package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
)

// Only type and environment names belong in registry/list metadata.
type secretEntryMeta struct {
	Type        string `json:"type,omitempty"`
	UsernameEnv string `json:"username_env,omitempty"`
	PasswordEnv string `json:"password_env,omitempty"`
}
type secretEntryData struct {
	secretEntryMeta
	Value       string `json:"value,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

func secretEntryType(kind string) string {
	if kind == "" {
		return "value"
	}
	return kind
}
func injectableSecretName(name string) bool {
	return envName.MatchString(name) && !strings.HasPrefix(name, "DEVKIT_") && !strings.HasPrefix(name, "VAULT_") && !strings.HasPrefix(name, "BAO_")
}
func validateEntryMeta(m secretEntryMeta) error {
	if secretEntryType(m.Type) != "value" && m.Type != "login" {
		return fail("Entry type must be value or login")
	}
	if m.Type != "login" && (m.UsernameEnv != "" || m.PasswordEnv != "") {
		return fail("Environment field mappings require a login entry")
	}
	if (m.UsernameEnv == "") != (m.PasswordEnv == "") {
		return fail("Map both username and password, or leave both unmapped")
	}
	if m.UsernameEnv != "" && (!injectableSecretName(m.UsernameEnv) || !injectableSecretName(m.PasswordEnv) || m.UsernameEnv == m.PasswordEnv) {
		return fail("Login fields need distinct, non-reserved environment names")
	}
	return nil
}
func validateEntryData(d secretEntryData) error {
	if err := validateEntryMeta(d.secretEntryMeta); err != nil {
		return err
	}
	if d.Type == "login" {
		if d.Value != "" || d.Username == "" || d.Password == "" || len(d.Username)+len(d.Password) > 64<<10 || strings.ContainsRune(d.Username+d.Password, 0) {
			return fail("Login needs a username and password totaling at most 65536 bytes, without NUL characters")
		}
	} else if d.Username != "" || d.Password != "" || len(d.Value) == 0 || len(d.Value) > 64<<10 {
		return fail("Single-value secret must contain 1–65536 bytes")
	}
	return nil
}
func readSecretEntry(ctx context.Context, c *vaultClient, path string, k *secretKey) (secretEntryData, error) {
	var out struct {
		Data struct {
			Data     secretEntryData `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := c.call(ctx, "GET", "kv/data/"+path, nil, &out); err != nil {
		return secretEntryData{}, err
	}
	d := out.Data.Data
	if err := validateEntryData(d); err != nil {
		return secretEntryData{}, err
	}
	if out.Data.Metadata.Version != k.Version || secretEntryType(d.Type) != secretEntryType(k.Type) || d.UsernameEnv != k.UsernameEnv || d.PasswordEnv != k.PasswordEnv {
		return secretEntryData{}, fail("Vault/registry entry conflict; preserve both and reconcile explicitly")
	}
	return d, nil
}
func generatedSecret(length int) (string, error) {
	if length == 0 {
		length = 32
	}
	if length < 16 || length > 256 {
		return "", fail("Generated passwords need 16–256 characters")
	}
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:length], nil
}
func prepareSecretEntry(ctx context.Context, c *vaultClient, path string, q secretRequest, key, parent *secretKey) (secretEntryData, error) {
	kind := q.Type
	inherited := key
	if inherited == nil {
		inherited = parent
	}
	if kind == "" && inherited != nil {
		kind = inherited.Type
	}
	kind = secretEntryType(kind)
	if inherited != nil && secretEntryType(inherited.Type) != kind {
		return secretEntryData{}, fail("Entry type cannot change under an existing name; choose a new name")
	}
	d := secretEntryData{secretEntryMeta: q.secretEntryMeta, Value: q.Value, Username: q.Username, Password: q.Password}
	d.Type = kind
	if kind == "value" {
		d.Type = ""
	}
	if kind != "login" && q.ClearEnv {
		return d, fail("Environment field mappings require a login entry")
	}
	if q.ClearEnv && (q.UsernameEnv != "" || q.PasswordEnv != "") {
		return d, fail("Choose mappings or no environment mapping, not both")
	}
	if kind == "login" && key != nil && q.UsernameEnv == "" && q.PasswordEnv == "" && !q.ClearEnv {
		d.UsernameEnv = key.UsernameEnv
		d.PasswordEnv = key.PasswordEnv
	}
	if q.Operation == "rotate" && kind == "login" {
		if q.Username != "" || q.Password != "" || q.Value != "" || q.ClearEnv || q.UsernameEnv != "" || q.PasswordEnv != "" {
			return d, fail("Login rotation preserves username and environment mappings; use update to change them")
		}
		var err error
		d, err = readSecretEntry(ctx, c, path, key)
		if err != nil {
			return d, err
		}
	}
	if q.Operation == "generate" || q.Operation == "rotate" {
		value, err := generatedSecret(q.Length)
		if err != nil {
			return d, err
		}
		if kind == "login" {
			d.Password = value
		} else {
			d.Value = value
		}
	}
	if err := validateEntryData(d); err != nil {
		return d, err
	}
	return d, nil
}
func injectSecretEntries(ctx context.Context, c *vaultClient, r *secretRegistry, effective map[string]*secretKey) (map[string]string, error) {
	destinations := map[string]bool{}
	for name, k := range effective {
		if err := validateEntryMeta(k.secretEntryMeta); err != nil {
			return nil, err
		}
		names := []string{name}
		if k.Type == "login" {
			if k.UsernameEnv == "" {
				continue
			}
			names = []string{k.UsernameEnv, k.PasswordEnv}
		}
		for _, target := range names {
			if !injectableSecretName(target) {
				return nil, fail("Secret name cannot be injected into this environment")
			}
			if destinations[target] {
				return nil, fail("Multiple effective secrets map to the same environment name; choose distinct mappings")
			}
			destinations[target] = true
		}
	}
	values := map[string]string{}
	for name, k := range effective {
		if k.Type == "login" && k.UsernameEnv == "" {
			continue
		}
		path, err := resourceKVPath(r.Resources[k.Resource], name)
		if err != nil {
			return nil, err
		}
		d, err := readSecretEntry(ctx, c, path, k)
		if err != nil {
			return nil, err
		}
		if d.Type == "login" {
			values[d.UsernameEnv] = d.Username
			values[d.PasswordEnv] = d.Password
		} else {
			values[name] = d.Value
		}
	}
	return values, nil
}

// Login stdin is a protected JSON object; never accept values in command arguments.
func readLoginInput(input io.Reader, q *secretRequest) error {
	b, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil || len(b) > 64<<10 {
		return fail("Invalid login input")
	}
	var fields struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&fields) != nil {
		return fail("Login stdin must be JSON with username and password fields")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return fail("Login stdin must contain one JSON object")
	}
	if q.Operation == "generate" && fields.Password != "" {
		return fail("Login generation accepts a username only; omit password")
	}
	q.Username = fields.Username
	q.Password = fields.Password
	return nil
}
