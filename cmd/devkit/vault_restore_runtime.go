package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// No shell, inherited stdin, raw diagnostic forwarding, or credential arguments.
var restoreCommand = func(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = time.Second
	var out inspectionBuffer
	cmd.Stdout = &out
	if e := cmd.Run(); e != nil || out.overflow {
		return "", fail("Recovery command failed; inspect local Docker/service availability")
	}
	return strings.TrimSpace(out.String()), nil
}

type restoreCredentials struct {
	Share  func(vaultStatus) (string, error)
	Login  func() (string, string, error)
	Shares []string
}

func restoreTerminalCredentials(ctx context.Context, user string) (*restoreCredentials, func(), error) {
	f, e := humanTerminal()
	if e != nil {
		return nil, nil, e
	}
	r := bufio.NewReader(io.LimitReader(f, 2<<20))
	prompt := func(message string) (string, error) {
		type answer struct {
			value string
			err   error
		}
		ch := make(chan answer, 1)
		go func() { v, e := terminalSecret(f, r, message); ch <- answer{v, e} }()
		select {
		case a := <-ch:
			return a.value, a.err
		case <-ctx.Done():
			return "", fail("Recovery input interrupted")
		}
	}
	return &restoreCredentials{
		Share: func(s vaultStatus) (string, error) { return prompt("Original backup unseal share") },
		Login: func() (string, string, error) {
			p, e := prompt("Original backup native administrator password")
			return user, p, e
		},
	}, func() { cmd := exec.Command("stty", "echo"); cmd.Stdin = f; _ = cmd.Run(); f.Close() }, nil
}
func restoreUnseal(ctx context.Context, c *vaultClient, input *restoreCredentials, reuse bool) error {
	index := 0
	for count := 0; count < 255; count++ {
		status, e := c.status(ctx)
		if e != nil {
			return e
		}
		if status.State == "ready" {
			return nil
		}
		if status.State != "sealed" || status.Threshold > 255 {
			return fail("Restored vault is not awaiting supported unseal shares")
		}
		var share string
		if reuse && index < len(input.Shares) {
			share = input.Shares[index]
			index++
		} else {
			if input.Share == nil {
				return fail("Original unseal shares are required")
			}
			share, e = input.Share(status)
			if e != nil {
				return e
			}
			if !reuse {
				input.Shares = append(input.Shares, share)
			}
		}
		if len(share) == 0 || len(share) > 4096 {
			return fail("Invalid unseal input")
		}
		if e = c.call(ctx, "POST", "sys/unseal", map[string]string{"key": share}, nil); e != nil {
			return fail("Original backup unseal share was rejected")
		}
	}
	return fail("Original unseal threshold was not satisfied")
}
func restoreLogin(ctx context.Context, c *vaultClient, input *restoreCredentials) error {
	if input.Login == nil {
		return fail("Native administrator authentication is required")
	}
	user, password, e := input.Login()
	if e != nil {
		return e
	}
	if !vaultSegment.MatchString(user) || len(password) == 0 || len(password) > 64<<10 {
		return fail("Invalid native administrator input")
	}
	var out struct {
		Auth struct {
			Token string `json:"client_token"`
		} `json:"auth"`
	}
	if e = c.call(ctx, "POST", "auth/userpass/login/"+user, map[string]string{"password": password}, &out); e != nil || out.Auth.Token == "" {
		return fail("Restored native administrator authentication failed")
	}
	c.token = out.Auth.Token
	return nil
}
func restoreBackend(ctx context.Context, admin *vaultClient) (string, error) {
	var child struct {
		Auth struct {
			Token string `json:"client_token"`
		} `json:"auth"`
	}
	if e := admin.call(ctx, "POST", "auth/token/create", map[string]any{"policies": []string{"devkit-backend"}, "no_default_policy": true, "period": "24h", "renewable": true, "no_parent": true}, &child); e != nil || child.Auth.Token == "" {
		return "", fail("Cannot provision a fresh recovery backend identity")
	}
	c := *admin
	c.token = child.Auth.Token
	var identity struct {
		Data struct {
			Policies []string `json:"policies"`
		} `json:"data"`
	}
	if e := c.call(ctx, "GET", "auth/token/lookup-self", nil, &identity); e != nil || len(identity.Data.Policies) != 1 || identity.Data.Policies[0] != "devkit-backend" {
		return "", fail("Recovered backend policy verification failed")
	}
	return child.Auth.Token, nil
}
func restoreRotateSession(ctx context.Context, c *vaultClient) error {
	_, version, e := c.read(ctx, "devkit/system/web-session-signing-key")
	if e != nil {
		return fail("Cannot inspect restored browser session key")
	}
	value := make([]byte, 32)
	if _, e = rand.Read(value); e != nil {
		return e
	}
	_, e = c.write(ctx, "devkit/system/web-session-signing-key", base64.RawStdEncoding.EncodeToString(value), version)
	return e
}
func verifyRestoredRegistry(ctx context.Context, c *vaultClient, r secretRegistry) error {
	for _, id := range sortedKeys(r.Keys) {
		k := r.Keys[id]
		resource := r.Resources[k.Resource]
		path, e := resourceKVPath(resource, k.Name)
		if e != nil {
			return e
		}
		var meta struct {
			Data struct {
				Current  int `json:"current_version"`
				Versions map[string]struct {
					Destroyed bool   `json:"destroyed"`
					Deleted   string `json:"deletion_time"`
				} `json:"versions"`
			} `json:"data"`
		}
		if e = c.call(ctx, "GET", "kv/metadata/"+path, nil, &meta); e != nil || meta.Data.Current != k.Version {
			return fail("Restored vault and registry versions do not match")
		}
		v, ok := meta.Data.Versions[strconv.Itoa(k.Version)]
		if !ok {
			return fail("Restored version history is incomplete")
		}
		for _, version := range k.Versions {
			retained, ok := meta.Data.Versions[strconv.Itoa(version)]
			if !ok || retained.Destroyed {
				return fail("Recoverable version history is incomplete")
			}
		}
		if k.Deleted && (v.Destroyed || v.Deleted == "") {
			return fail("Deleted key recovery state does not match registry")
		}
		if !k.Deleted && resource.State == "active" {
			if v.Destroyed || v.Deleted != "" {
				return fail("An active recovered key is unavailable")
			}
			if _, e = readSecretEntry(ctx, c, path, k); e != nil {
				return fail("Recovered secret content does not match its metadata")
			}
		}
	}
	return nil
}

type restoreCandidate struct {
	Operation, Root, Data, ID, Network, Address, Image, Backend string
}

func (c *restoreCandidate) close(removeData bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// A cancelled docker run/create can succeed without returning an ID. Discover
	// only this attempt's random label before deleting its storage.
	ids, e := restoreCommand(ctx, "docker", "ps", "-aq", "--no-trunc", "--filter", "label=devkit.restore="+c.Operation)
	if e != nil {
		return fail("Cannot verify recovery cleanup; retain attempt %s at %s", c.Operation, c.Root)
	}
	for _, id := range strings.Fields(ids) {
		if !fullContainerID.MatchString(id) {
			return fail("Invalid recovery cleanup identity")
		}
		raw, e := restoreCommand(ctx, "docker", "inspect", id)
		var rows []container
		if e != nil || json.Unmarshal([]byte(raw), &rows) != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Config.Labels["devkit.restore"] != c.Operation {
			return fail("Recovery container ownership changed")
		}
		dataOK := false
		for _, m := range rows[0].Mounts {
			if m.Type == "bind" && m.Source == c.Data && m.Destination == "/bao/data" {
				dataOK = true
			}
		}
		if !dataOK {
			return fail("Recovery container storage ownership changed")
		}
		remove := []string{"docker", "rm", "-f", id}
		if !removeData {
			if _, e = restoreCommand(ctx, "docker", "stop", id); e != nil {
				return fail("Cannot quiesce tested recovery data; retain attempt %s", c.Operation)
			}
			remove = []string{"docker", "rm", id}
		}
		if _, e = restoreCommand(ctx, remove...); e != nil {
			return fail("Recovery container cleanup failed; retain attempt %s", c.Operation)
		}
	}
	c.ID = ""
	networks, e := restoreCommand(ctx, "docker", "network", "ls", "-q", "--no-trunc", "--filter", "label=devkit.restore="+c.Operation)
	if e != nil {
		return fail("Cannot verify recovery network cleanup")
	}
	for _, id := range strings.Fields(networks) {
		if !fullContainerID.MatchString(id) {
			return fail("Invalid recovery network identity")
		}
		raw, e := restoreCommand(ctx, "docker", "network", "inspect", id)
		var rows []struct {
			ID, Name string
			Internal bool
			Labels   map[string]string
		}
		if e != nil || json.Unmarshal([]byte(raw), &rows) != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Name != "devkit-restore-"+c.Operation || !rows[0].Internal || rows[0].Labels["devkit.restore"] != c.Operation {
			return fail("Recovery network ownership changed")
		}
		if _, e = restoreCommand(ctx, "docker", "network", "rm", id); e != nil {
			return fail("Recovery network cleanup failed; retain attempt %s", c.Operation)
		}
	}
	c.Network = ""
	if removeData {
		if e := os.RemoveAll(c.Root); e != nil {
			return fail("Recovery temporary storage cleanup failed")
		}
	}
	return nil
}
func waitRestoreState(ctx context.Context, c *vaultClient, want string) error {
	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		s, e := c.status(deadline)
		if e == nil && s.State == want {
			return nil
		}
		select {
		case <-deadline.Done():
			return fail("Isolated restore did not reach the required vault state")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func restoreLocalDocker(ctx context.Context) error {
	if endpoint := os.Getenv("DOCKER_HOST"); endpoint != "" && !strings.HasPrefix(endpoint, "unix:///") {
		return fail("Restore requires a local Docker Unix socket")
	}
	endpoint, e := restoreCommand(ctx, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if e != nil || !strings.HasPrefix(endpoint, "unix:///") {
		return fail("Restore requires a local Docker Unix socket context")
	}
	return nil
}
func rehearseVaultRestore(ctx context.Context, b restoreBackup, plain *restoreContents, input *restoreCredentials, parent string) (candidate *restoreCandidate, err error) {
	if e := restoreLocalDocker(ctx); e != nil {
		return nil, e
	}
	root, e := os.MkdirTemp(parent, "devkit-restore-candidate-")
	if e != nil {
		return nil, fail("Cannot reserve isolated recovery storage")
	}
	c := &restoreCandidate{Operation: secretID(), Root: root, Data: filepath.Join(root, "data"), Image: b.Manifest.Image}
	success := false
	defer func() {
		if !success {
			if e := c.close(true); e != nil {
				err = e
			}
		}
	}()
	if e = os.Mkdir(c.Data, 0700); e != nil {
		return nil, e
	}
	_, config := vaultFiles(vaultConfig{Address: "http://127.0.0.1:8200"}, root)
	config["ui"] = false
	config["log_level"] = "error"
	config["listener"] = map[string]any{"tcp": map[string]any{"address": "0.0.0.0:8200", "tls_disable": true, "cluster_address": "0.0.0.0:8201"}}

	configBytes, _ := json.Marshal(config)
	if e = writePrivate(filepath.Join(root, "server.json"), configBytes); e != nil {
		return nil, e
	}
	network := "devkit-restore-" + c.Operation
	if _, e = restoreCommand(ctx, "docker", "network", "create", "--internal", "--label", "devkit.restore="+c.Operation, network); e != nil {
		return nil, e
	}
	c.Network = network
	id, e := restoreCommand(ctx, "docker", "run", "-d", "--name", network, "--label", "devkit.restore="+c.Operation, "--network", network, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()), "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--ulimit", "core=0:0", "--memory", "512m", "--memory-swap", "512m", "--mount", "type=bind,src="+c.Data+",dst=/bao/data", "--mount", "type=bind,src="+filepath.Join(root, "server.json")+",dst=/bao/config/server.json,readonly", "--entrypoint", "bao", c.Image, "server", "-config=/bao/config/server.json")
	if e != nil {
		return nil, e
	}
	if !fullContainerID.MatchString(id) {
		return nil, fail("Invalid isolated recovery container identity")
	}
	c.ID = id
	raw, e := restoreCommand(ctx, "docker", "inspect", id)
	if e != nil {
		return nil, e
	}
	var rows []container
	if json.Unmarshal([]byte(raw), &rows) != nil || len(rows) != 1 || rows[0].ID != id || rows[0].Config.Labels["devkit.restore"] != c.Operation || len(rows[0].NetworkSettings.Networks) != 1 || len(rows[0].HostConfig.PortBindings) != 0 {
		return nil, fail("Isolated recovery network ownership changed")
	}
	endpoint, ok := rows[0].NetworkSettings.Networks[network]
	ip := net.ParseIP(endpoint.IPAddress)
	if !ok || ip == nil || !ip.IsPrivate() {
		return nil, fail("Recovery network has no private endpoint")
	}
	// Internal Docker bridges do not publish ports. Reach the exact inspected endpoint
	// from this host; never loosen the managed vault's loopback-address contract.
	client, _ := newVaultClient("http://127.0.0.1:8200", "")
	c.Address = "http://" + net.JoinHostPort(ip.String(), "8200")
	client.address = c.Address
	if e = waitRestoreState(ctx, client, "uninitialized"); e != nil {
		return nil, e
	}
	s, e := client.status(ctx)
	expected := strings.Split(strings.Split(c.Image, ":")[1], "@")[0]
	if e != nil || s.Version != expected {
		return nil, fail("Isolated vault does not match backup image version")
	}
	var initial struct {
		Keys  []string `json:"keys_base64"`
		Token string   `json:"root_token"`
	}
	if e = client.call(ctx, "POST", "sys/init", map[string]int{"secret_shares": 1, "secret_threshold": 1}, &initial); e != nil || len(initial.Keys) != 1 || initial.Token == "" {
		return nil, fail("Cannot initialize isolated recovery instance")
	}
	if e = client.call(ctx, "POST", "sys/unseal", map[string]string{"key": initial.Keys[0]}, nil); e != nil {
		return nil, e
	}
	if e = waitRestoreState(ctx, client, "ready"); e != nil {
		return nil, e
	}
	file, e := restoreOpenPrivate(filepath.Join(plain.Root, "raft.snap"), restoreSnapshotLimit)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.Address+"/v1/sys/storage/raft/snapshot-force", file)
	if e != nil {
		file.Close()
		return nil, e
	}
	req.Header.Set("X-Vault-Token", initial.Token)
	transport := *client.http
	transport.Timeout = 2 * time.Minute
	response, e := transport.Do(req)
	file.Close()
	if e != nil {
		return nil, fail("Isolated snapshot restore failed")
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 204 {
		return nil, fail("Isolated snapshot restore was rejected")
	}
	if e = waitRestoreState(ctx, client, "sealed"); e != nil {
		return nil, e
	}
	if e = restoreUnseal(ctx, client, input, false); e != nil {
		return nil, e
	}
	if e = restoreLogin(ctx, client, input); e != nil {
		return nil, e
	}
	defer client.call(context.Background(), "POST", "auth/token/revoke-self", nil, nil)
	c.Backend, e = restoreBackend(ctx, client)
	if e != nil {
		return nil, e
	}
	backend := *client
	backend.token = c.Backend
	if e = verifyRestoredRegistry(ctx, &backend, plain.Registry); e != nil {
		return nil, e
	}
	if e = restoreRotateSession(ctx, &backend); e != nil {
		return nil, e
	}
	success = true
	return c, nil
}
