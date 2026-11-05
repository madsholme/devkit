package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const maxDocumentBytes = 256 << 10

type documentEntry struct {
	Path     string `json:"path"`
	Editable bool   `json:"editable"`
}
type documentList struct {
	Files    []documentEntry `json:"files"`
	Warnings []string        `json:"warnings"`
}
type documentData struct {
	documentEntry
	Content string `json:"content"`
	Version string `json:"version"`
}

func agentDocument(path string) bool {
	return strings.EqualFold(path, "AGENTS.md") || strings.EqualFold(path, "AGENT.md")
}
func validDocumentPath(path string, write bool) bool {
	if agentDocument(path) {
		return !write
	}
	if len(path) > 512 || !strings.HasPrefix(path, "docs/") || !strings.EqualFold(filepath.Ext(path), ".md") || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) > 17 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") || strings.ContainsFunc(part, unicode.IsControl) {
			return false
		}
	}
	return true
}
func documentText(text string) bool {
	return len(text) <= maxDocumentBytes && utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}
func documentVersion(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Exact discovered checkouts only. Folder projects need not have a Git repository.
func openDocumentRoot(root string) (*os.File, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fail("Choose a registered project checkout.")
	}
	cfg, err := loadHostConfig()
	if err != nil {
		return nil, err
	}
	roots, _ := projectDirectories(cfg)
	roots = append(roots, cfg.VMProjects...)
	known := false
	for _, r := range roots {
		if resolve(expandHome(r)) == root {
			known = true
			break
		}
	}
	if !known {
		known = knownGitCheckout(root)
	}
	if !known {
		return nil, fail("This checkout is no longer registered with devkit.")
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fail("The project checkout is unavailable or is a symbolic link.")
	}
	return os.NewFile(uintptr(fd), root), nil
}

// Open each component relative to a pinned directory, never following symlinks.
func documentDirectory(root *os.File, parts []string, create bool) (*os.File, error) {
	fd, err := syscall.Dup(int(root.Fd()))
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	dir := os.NewFile(uintptr(fd), root.Name())
	for _, part := range parts {
		next, err := syscall.Openat(int(dir.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err == syscall.ENOENT && create {
			if e := syscall.Mkdirat(int(dir.Fd()), part, 0755); e != nil && e != syscall.EEXIST {
				dir.Close()
				return nil, e
			}
			next, err = syscall.Openat(int(dir.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		dir.Close()
		if err != nil {
			return nil, fail("Document folder is missing, inaccessible or a symbolic link.")
		}
		dir = os.NewFile(uintptr(next), part)
	}
	return dir, nil
}
func readDocumentAt(dir *os.File, name string) ([]byte, os.FileInfo, error) {
	fd, err := syscall.Openat(int(dir.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 || info.Size() > maxDocumentBytes {
		return nil, nil, fail("Only regular Markdown files up to 256 KiB are supported; links and special files are excluded.")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if !documentText(string(data)) {
		return nil, nil, fail("Document must be UTF-8 text up to 256 KiB without NUL bytes.")
	}
	return data, info, nil
}
func readDocument(root, path string) (documentData, error) {
	result := documentData{documentEntry: documentEntry{Path: path, Editable: !agentDocument(path)}}
	if !validDocumentPath(path, false) {
		return result, fail("Choose a Markdown file under docs/ or the root agent guidance file.")
	}
	dir, err := openDocumentRoot(root)
	if err != nil {
		return result, err
	}
	defer dir.Close()
	parts := strings.Split(path, "/")
	parent, err := documentDirectory(dir, parts[:len(parts)-1], false)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	data, _, err := readDocumentAt(parent, parts[len(parts)-1])
	if err != nil {
		return result, fail("Cannot read this document: it is missing, linked, too large or not valid text.")
	}
	result.Content = string(data)
	result.Version = documentVersion(data)
	return result, nil
}
func listDocuments(root string) (documentList, error) {
	result := documentList{Files: []documentEntry{}, Warnings: []string{}}
	dir, err := openDocumentRoot(root)
	if err != nil {
		return result, err
	}
	defer dir.Close()
	visited := 0
	limited := false
	skipped := false
	var walk func(*os.File, string, int)
	walk = func(folder *os.File, prefix string, depth int) {
		if depth > 16 {
			limited = true
			return
		}
		for {
			entries, err := folder.ReadDir(128)
			for _, entry := range entries {
				visited++
				if visited > 5000 || len(result.Files) >= 1000 {
					limited = true
					return
				}
				name := entry.Name()
				if strings.HasPrefix(name, ".") {
					continue
				}
				path := prefix + name
				if entry.Type()&os.ModeSymlink != 0 {
					skipped = true
					continue
				}
				if entry.IsDir() && (prefix != "" || name == "docs") {
					child, e := documentDirectory(folder, []string{name}, false)
					if e != nil {
						skipped = true
						continue
					}
					walk(child, path+"/", depth+1)
					child.Close()
				} else if validDocumentPath(path, false) {
					if _, _, e := readDocumentAt(folder, name); e == nil {
						result.Files = append(result.Files, documentEntry{path, !agentDocument(path)})
					} else {
						skipped = true
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					skipped = true
				}
				return
			}
			if limited {
				return
			}
		}
	}
	walk(dir, "", 0)
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	if limited {
		result.Warnings = append(result.Warnings, "Document listing reached its file, depth or scan limit.")
	}
	if skipped {
		result.Warnings = append(result.Warnings, "Some linked, unreadable, oversized or non-text files were omitted.")
	}
	return result, nil
}
func validateDocumentSave(path, expected, content string) error {
	if !validDocumentPath(path, true) {
		return fail("Only Markdown files under docs/ can be created or edited. Agent guidance is read-only.")
	}
	if expected != "new" {
		v, e := hex.DecodeString(expected)
		if e != nil || len(v) != sha256.Size {
			return fail("Read the document first and supply its version, or use new for a new file.")
		}
	}
	if !documentText(content) {
		return fail("Document must be UTF-8 text up to 256 KiB without NUL bytes.")
	}
	return nil
}
func checkDocumentVersion(dir *os.File, name, expected string) (os.FileInfo, error) {
	data, info, err := readDocumentAt(dir, name)
	if expected == "new" && err == syscall.ENOENT {
		return nil, nil
	}
	if err != nil {
		return nil, fail("Document is unavailable or unsafe; reload before saving.")
	}
	if expected == "new" {
		return nil, fail("A file with this name already exists. Choose another name.")
	}
	if documentVersion(data) != expected {
		return nil, fail("Document changed on disk. Your draft was not saved; reload and reconcile your changes.")
	}
	return info, nil
}
func saveDocument(root, path, expected, content string) error {
	if err := validateDocumentSave(path, expected, content); err != nil {
		return err
	}
	dir, err := openDocumentRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	// Directory locks serialize devkit writers without creating a tracked lock file.
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail("Another document save is running in this checkout; try again.")
	}
	defer syscall.Flock(int(dir.Fd()), syscall.LOCK_UN)
	parts := strings.Split(path, "/")
	parent, err := documentDirectory(dir, parts[:len(parts)-1], expected == "new")
	if err != nil {
		return err
	}
	defer parent.Close()
	name := parts[len(parts)-1]
	before, err := checkDocumentVersion(parent, name, expected)
	if err != nil {
		return err
	}
	token := make([]byte, 12)
	if _, err = rand.Read(token); err != nil {
		return err
	}
	temp := ".devkit-doc-" + hex.EncodeToString(token)
	fd, err := syscall.Openat(int(parent.Fd()), temp, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temp)
	defer file.Close()
	defer syscall.Unlinkat(int(parent.Fd()), temp)
	mode := os.FileMode(0644)
	if before != nil {
		mode = before.Mode().Perm()
	}
	if _, err = file.WriteString(content); err != nil {
		return err
	}
	if err = file.Chmod(mode); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	// Revalidate membership and the visible directory after preparing the temporary file.
	current, err := openDocumentRoot(root)
	if err != nil {
		return err
	}
	defer current.Close()
	visible, err := documentDirectory(current, parts[:len(parts)-1], false)
	if err != nil {
		return err
	}
	defer visible.Close()
	a, _ := parent.Stat()
	b, e := visible.Stat()
	if e != nil || a == nil || !os.SameFile(a, b) {
		return fail("Document folder changed during save.")
	}
	after, err := checkDocumentVersion(parent, name, expected)
	if err != nil {
		return err
	}
	if before != nil && (!os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode()) {
		return fail("Document changed on disk. Reload before saving.")
	}
	if expected == "new" {
		// Hard-link publication is exclusive: it cannot overwrite a concurrently created path.
		err = os.Link(fmt.Sprintf("/proc/self/fd/%d/%s", parent.Fd(), temp), fmt.Sprintf("/proc/self/fd/%d/%s", parent.Fd(), name))
	} else {
		err = syscall.Renameat(int(parent.Fd()), temp, int(parent.Fd()), name)
	}
	if err != nil {
		return fail("Could not publish the document; no existing file was overwritten by creation.")
	}
	happened("document-save", path, "Saved Markdown document without committing.")
	fmt.Fprintf(stdout, "Saved %s. Changes remain uncommitted.\n", path)
	return nil
}
func planDocumentSave(req actionRequest) (plannedAction, error) {
	if err := validateDocumentSave(req.Path, req.Expected, req.Content); err != nil {
		return plannedAction{}, err
	}
	dir, err := openDocumentRoot(req.Worktree)
	if err != nil {
		return plannedAction{}, err
	}
	dir.Close()
	if req.Expected != "new" {
		data, err := readDocument(req.Worktree, req.Path)
		if err != nil {
			return plannedAction{}, err
		}
		if data.Version != req.Expected {
			return plannedAction{}, fail("Document changed on disk. Reload before saving.")
		}
	}
	return plannedAction{args: []string{"docs", "save", req.Path, "--expected", req.Expected, "--file", "-"}, dir: req.Worktree, stdin: req.Content, target: "documents " + req.Worktree, title: "Save " + req.Path}, nil
}
func documentsCLI(args []string) (int, error) {
	if len(args) == 0 {
		return 2, usageError("usage: devkit docs list | read PATH | save PATH --expected VERSION|new --file FILE|-")
	}
	root, err := os.Getwd()
	if err != nil {
		return 1, err
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return 2, usageError("docs list takes no arguments")
		}
		data, e := listDocuments(root)
		if e != nil {
			return 1, e
		}
		return 0, json.NewEncoder(stdout).Encode(data)
	case "read":
		if len(args) != 2 {
			return 2, usageError("docs read needs one path")
		}
		data, e := readDocument(root, args[1])
		if e != nil {
			return 1, e
		}
		return 0, json.NewEncoder(stdout).Encode(data)
	case "save":
		pos, values, e := splitArgs(args[1:], map[string]bool{"--expected": true, "--file": true}, nil)
		if e != nil {
			return 2, e
		}
		if len(pos) != 1 || values["--file"] == "" {
			return 2, usageError("docs save needs PATH --expected VERSION|new --file FILE|-")
		}
		var input io.Reader = os.Stdin
		if values["--file"] != "-" {
			f, e := os.Open(values["--file"])
			if e != nil {
				return 1, fail("Cannot open the Markdown input file.")
			}
			defer f.Close()
			input = f
		}
		data, e := io.ReadAll(io.LimitReader(input, maxDocumentBytes+1))
		if e != nil {
			return 1, e
		}
		return 0, noted("document-save", pos[0], saveDocument(root, pos[0], values["--expected"], string(data)))
	}
	return 2, usageError("Use devkit docs list, read or save.")
}
