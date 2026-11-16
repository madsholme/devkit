package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var restoreIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`)
var restoreOperationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const restoreSnapshotLimit int64 = 1 << 30

var restoreComponentLimits = map[string]int64{
	"raft.snap.age": restoreSnapshotLimit, "registry.json.age": 16 << 20,
	"cleanup.json.age": 65536, "host.json.age": 65536, "acme.tar.age": 32 << 20,
}

type restoreBackup struct {
	Manifest    vaultBackupManifest
	Directory   string
	Fingerprint string
}
type restoreHostMetadata struct {
	Domain        string      `json:"domain"`
	Vault         vaultConfig `json:"vault"`
	ACME          bool        `json:"acme_included"`
	DataDirectory string      `json:"data_directory"`
}
type restoreContents struct {
	Root     string
	Registry secretRegistry
	Cleanup  cleanupConfig
	Host     restoreHostMetadata
}

// Reject symlinks in every existing path component, not just the final filename.
func restoreRealPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fail("Recovery paths must be clean absolute paths")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, e := os.Lstat(current)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return fail("Recovery path is missing or contains a symlink")
		}
	}
	return nil
}

// Local metadata may be absent during disaster recovery; preserve that distinction.
func restoreReadOptional(path string, limit int64) ([]byte, error) {
	if _, e := os.Lstat(path); e != nil {
		return nil, e
	}
	return restoreReadPrivate(path, limit)
}
func restorePrivateDirectory(path string) error {
	ancestor := path
	for {
		_, e := os.Lstat(ancestor)
		if e == nil {
			break
		}
		if !os.IsNotExist(e) || ancestor == filepath.Dir(ancestor) {
			return fail("Unsafe recovery parent")
		}
		ancestor = filepath.Dir(ancestor)
	}
	if e := restoreRealPath(ancestor); e != nil {
		return e
	}
	if e := privateDir(path); e != nil {
		return e
	}
	return restoreRealPath(path)
}
func restoreOpenPrivate(path string, limit int64) (*os.File, error) {
	if e := restoreRealPath(path); e != nil {
		return nil, e
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, fail("Cannot open private recovery file")
	}
	f := os.NewFile(uintptr(fd), path)
	st, e := f.Stat()
	if e == nil {
		s, ok := st.Sys().(*syscall.Stat_t)
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || !ok || int(s.Uid) != os.Getuid() || st.Size() > limit || st.Size() == 0 {
			e = fail("Recovery file must be bounded, nonempty, owner-only and regular")
		}
	}
	if e != nil {
		f.Close()
		return nil, fail("Unsafe recovery file")
	}
	return f, nil
}
func restoreReadPrivate(path string, limit int64) ([]byte, error) {
	f, e := restoreOpenPrivate(path, limit)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, fail("Cannot read bounded recovery file")
	}
	return b, nil
}
func restoreDecode(b []byte, out any) error {
	tokens := json.NewDecoder(bytes.NewReader(b))
	if e := restoreJSONValue(tokens, 0); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return fail("Invalid recovery metadata")
	}
	return nil
}

// Reject duplicate keys and excessive nesting before typed decoding. JSON's usual
// last-key-wins behavior is inappropriate for recovery identity and checksums.
func restoreJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return fail("Recovery metadata nesting exceeds limit")
	}
	token, e := d.Token()
	if e != nil {
		return fail("Invalid recovery JSON")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			name, ok := key.(string)
			if e != nil || !ok || seen[name] {
				return fail("Ambiguous recovery metadata")
			}
			seen[name] = true
			if e = restoreJSONValue(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = restoreJSONValue(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return fail("Invalid recovery JSON")
	}
	if _, e = d.Token(); e != nil {
		return fail("Invalid recovery JSON")
	}
	return nil
}
func inspectRestoreBackup(cfg vaultConfig, id string) (restoreBackup, error) {
	var result restoreBackup
	if !restoreIDPattern.MatchString(id) {
		return result, fail("Invalid backup ID")
	}
	dir := filepath.Join(cfg.Backup.Directory, id)
	if !filepath.IsAbs(cfg.Backup.Directory) || restoreRealPath(dir) != nil {
		return result, fail("Unsafe backup directory")
	}
	identity, e := restoreDirIdentity(dir)
	if e != nil || identity.Inode == 0 {
		return result, fail("Backup directory must be private")
	}
	b, e := restoreReadPrivate(filepath.Join(dir, "manifest.json"), 65536)
	if e != nil {
		return result, e
	}
	m := vaultBackupManifest{}
	if restoreDecode(b, &m) != nil || m.ID != id || !restoreOperationPattern.MatchString(m.Installation) || !pinnedVaultImage.MatchString(m.Image) {
		return result, fail("Invalid backup identity or image")
	}
	for _, name := range []string{"raft.snap.age", "registry.json.age", "cleanup.json.age", "host.json.age"} {
		if m.Files[name] == "" {
			return result, fail("Backup lacks required recovery components")
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != len(m.Files)+1 {
		return result, fail("Unexpected backup files")
	}
	fingerprint := sha256.New()
	fingerprint.Write(b)
	for _, name := range sortedKeys(m.Files) {
		limit, ok := restoreComponentLimits[name]
		if !ok {
			return result, fail("Unsupported backup component")
		}
		expected, e := hex.DecodeString(m.Files[name])
		if e != nil || len(expected) != sha256.Size {
			return result, fail("Invalid backup checksum")
		}
		f, e := restoreOpenPrivate(filepath.Join(dir, name), limit+65536)
		if e != nil {
			return result, e
		}
		hash := sha256.New()
		n, e := io.Copy(hash, io.LimitReader(f, limit+65537))
		f.Close()
		if e != nil || n > limit+65536 || !bytes.Equal(hash.Sum(nil), expected) {
			return result, fail("Backup checksum mismatch")
		}
		fingerprint.Write(hash.Sum(nil))
	}
	return restoreBackup{Manifest: m, Directory: dir, Fingerprint: hex.EncodeToString(fingerprint.Sum(nil))}, nil
}

// All plaintext components live on tmpfs, never in the configured vault or backup directory.
func restoreMemoryDirectory() (string, error) {
	var st syscall.Statfs_t
	if syscall.Statfs("/dev/shm", &st) != nil || st.Type != 0x01021994 {
		return "", fail("Restore requires private memory-backed /dev/shm storage")
	}
	p, e := os.MkdirTemp("/dev/shm", "devkit-restore-")
	if e != nil {
		return "", fail("Cannot reserve private recovery memory")
	}
	return p, nil
}

type restoreBoundedWriter struct {
	target    io.Writer
	remaining int64
}

func (w *restoreBoundedWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > w.remaining {
		return 0, fail("Decrypted component exceeds recovery limit")
	}
	n, e := w.target.Write(b)
	w.remaining -= int64(n)
	return n, e
}

func decryptRestoreBackup(ctx context.Context, b restoreBackup, identity string) (*restoreContents, error) {
	key, e := restoreOpenPrivate(identity, 64<<10)
	if e != nil {
		return nil, e
	}
	defer key.Close()
	root, e := restoreMemoryDirectory()
	if e != nil {
		return nil, e
	}
	result := &restoreContents{Root: root}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(root)
		}
	}()
	for _, name := range sortedKeys(b.Manifest.Files) {
		input, e := restoreOpenPrivate(filepath.Join(b.Directory, name), restoreComponentLimits[name]+65536)
		if e != nil {
			return nil, e
		}
		// Verify the same descriptor consumed by age; a replaced pathname cannot substitute inputs.
		hash := sha256.New()
		_, e = io.Copy(hash, input)
		if e != nil || hex.EncodeToString(hash.Sum(nil)) != b.Manifest.Files[name] {
			input.Close()
			return nil, fail("Backup changed before decryption")
		}
		input.Seek(0, io.SeekStart)
		key.Seek(0, io.SeekStart)
		output, e := os.OpenFile(filepath.Join(root, strings.TrimSuffix(name, ".age")), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			input.Close()
			return nil, fail("Cannot reserve decrypted component")
		}
		cmd := exec.CommandContext(ctx, "age", "--decrypt", "--identity", "/proc/self/fd/3")
		cmd.ExtraFiles = []*os.File{key}
		cmd.Stdin = input
		cmd.Stdout = &restoreBoundedWriter{output, restoreComponentLimits[name]}
		e = cmd.Run()
		input.Close()
		closeErr := output.Close()
		if e != nil || closeErr != nil {
			return nil, fail("Backup decryption failed; check identity, integrity and memory capacity")
		}
	}
	if e = validateRestoreContents(result, b); e != nil {
		return nil, e
	}
	success = true
	return result, nil
}
func validateRestoreContents(c *restoreContents, b restoreBackup) error {
	read := func(name string, limit int64, out any) error {
		v, e := restoreReadPrivate(filepath.Join(c.Root, name), limit)
		if e != nil {
			return e
		}
		return restoreDecode(v, out)
	}
	if e := read("host.json", 65536, &c.Host); e != nil {
		return e
	}
	if c.Host.Vault.Image != b.Manifest.Image || validateVaultConfig(c.Host.Vault) != nil || !filepath.IsAbs(c.Host.DataDirectory) {
		return fail("Backup host metadata does not match its manifest")
	}
	if e := read("registry.json", 16<<20, &c.Registry); e != nil {
		return e
	}
	r := &c.Registry
	if (r.Schema != 1 && r.Schema != 2) || r.Installation != b.Manifest.Installation || r.Resources == nil || r.Keys == nil || r.Pending != nil || len(r.Resources) > 10000 || len(r.Keys) > 10000 {
		return fail("Unsupported or incomplete recovery registry")
	}
	for id, v := range r.Resources {
		if v == nil || id != v.ID || !restoreOperationPattern.MatchString(id) || (v.State != "active" && v.State != "retired" && v.State != "destroyed") {
			return fail("Invalid recovered resource")
		}
		if v.Kind != "project" && v.Kind != "branch" && v.Kind != "worktree" {
			return fail("Invalid recovered resource kind")
		}
		parent := r.Resources[v.Project]
		if parent == nil || parent.Kind != "project" || (v.Kind == "project" && v.Project != id) {
			return fail("Invalid recovered project relationship")
		}
		if v.Branch != "" {
			branch := r.Resources[v.Branch]
			if branch == nil || branch.Kind != "branch" || branch.Project != v.Project {
				return fail("Invalid recovered branch relationship")
			}
		}
	}
	for id, k := range r.Keys {
		if k == nil || id != secretKeyID(k.Resource, k.Name) || k.Version < 1 || validateEntryMeta(k.secretEntryMeta) != nil || (k.Type == "login" && r.Schema < 2) {
			return fail("Invalid recovered key metadata")
		}
		if _, e := resourceKVPath(r.Resources[k.Resource], k.Name); e != nil {
			return fail("Invalid recovered key ownership")
		}
	}
	if e := read("cleanup.json", 65536, &c.Cleanup); e != nil {
		return e
	}
	if validateCleanupConfig(c.Cleanup) != nil {
		return fail("Invalid recovered cleanup configuration")
	}
	if c.Host.ACME != (b.Manifest.Files["acme.tar.age"] != "") {
		return fail("Backup certificate declaration mismatch")
	}
	if c.Host.ACME {
		v, e := restoreReadPrivate(filepath.Join(c.Root, "acme.tar"), 32<<20)
		if e != nil {
			return e
		}
		if e = validateRestoreACME(v); e != nil {
			return e
		}
	}
	return nil
}
func validateRestoreACME(b []byte) error {
	reader := tar.NewReader(bytes.NewReader(b))
	seen := map[string]bool{}
	for {
		h, e := reader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail("Invalid certificate archive")
		}
		if (h.Name == "./" || h.Name == ".") && h.Typeflag == tar.TypeDir && h.Size == 0 {
			continue
		}
		name := strings.TrimPrefix(h.Name, "./")
		if seen[name] || (name != "acme.json" && name != "acme-staging.json") || h.Typeflag != tar.TypeReg || h.Mode != 0600 || h.Size < 0 || h.Size > 16<<20 {
			return fail("Unsafe certificate archive entry")
		}
		seen[name] = true
		// Traefik initializes an unused production or staging store as an empty 0600 file.
		if h.Size == 0 {
			continue
		}
		value, e := io.ReadAll(io.LimitReader(reader, (16<<20)+1))
		if e != nil || len(value) > 16<<20 {
			return fail("Invalid certificate archive")
		}
		var resolvers map[string]struct {
			Certificates []struct{ Certificate, Key []byte }
		}
		// Traefik also stores account fields; only inspect certificate/key consistency here.
		if json.Unmarshal(value, &resolvers) != nil || resolvers == nil {
			return fail("Invalid certificate metadata")
		}
		for _, resolver := range resolvers {
			for _, cert := range resolver.Certificates {
				if _, e := tls.X509KeyPair(cert.Certificate, cert.Key); e != nil {
					return fail("Invalid certificate/key pair in backup")
				}
			}
		}
	}
	if len(seen) == 0 {
		return fail("Certificate archive is empty")
	}
	return nil
}
