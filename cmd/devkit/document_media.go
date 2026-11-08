package main

import (
	"bytes"
	"encoding/xml"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

const maxDocumentImageBytes = 8 * 1024 * 1024

// Images are served only from exact registered checkouts, with the same pinned
// directory/no-link rules as documents. Never proxy a remote URL or serve source.
func readDocumentImage(root, path string) ([]byte, string, error) {
	parts := strings.Split(path, "/")
	if len(path) > 512 || len(parts) > 17 || strings.ContainsAny(path, "\\\x00") {
		return nil, "", fail("Invalid image path.")
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") || strings.ContainsFunc(part, unicode.IsControl) {
			return nil, "", fail("Invalid image path.")
		}
	}
	kinds := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml"}
	kind, ok := kinds[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return nil, "", fail("Supported images: PNG, JPEG, GIF, WebP and SVG.")
	}
	dir, err := openDocumentRoot(root)
	if err != nil {
		return nil, "", err
	}
	defer dir.Close()
	parent, err := documentDirectory(dir, parts[:len(parts)-1], false)
	if err != nil {
		return nil, "", err
	}
	defer parent.Close()
	fd, err := syscall.Openat(int(parent.Fd()), parts[len(parts)-1], syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", fail("Image is missing or inaccessible.")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 || info.Size() > maxDocumentImageBytes {
		return nil, "", fail("Only regular images up to 8 MiB are supported; links and special files are excluded.")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentImageBytes+1))
	if err != nil || len(data) > maxDocumentImageBytes {
		return nil, "", fail("Image exceeds 8 MiB or cannot be read.")
	}
	if kind == "image/svg+xml" {
		decoder := xml.NewDecoder(bytes.NewReader(data))
		rootSeen := false
		depth := 0
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, "", fail("Invalid SVG image.")
			}
			switch token := token.(type) {
			case xml.Directive:
				return nil, "", fail("SVG declarations are not supported.")
			case xml.ProcInst:
				if token.Target != "xml" {
					return nil, "", fail("SVG processing instructions are not supported.")
				}
			case xml.StartElement:
				if depth == 0 {
					if rootSeen || token.Name.Local != "svg" || (token.Name.Space != "" && token.Name.Space != "http://www.w3.org/2000/svg") {
						return nil, "", fail("Invalid SVG root.")
					}
					rootSeen = true
				}
				depth++
			case xml.EndElement:
				depth--
			}
		}
		if !rootSeen || depth != 0 {
			return nil, "", fail("Invalid SVG image.")
		}
	} else {
		if http.DetectContentType(data) != kind {
			return nil, "", fail("Image content does not match its file type.")
		}
		if kind != "image/webp" {
			cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32000000 {
				return nil, "", fail("Invalid image or dimensions exceed 32 million pixels.")
			}
		}
	}
	return data, kind, nil
}

func serveDocumentImage(w http.ResponseWriter, r *http.Request) error {
	data, kind, err := readDocumentImage(r.URL.Query().Get("worktree"), r.URL.Query().Get("path"))
	if err != nil {
		return err
	}
	// SVG stays in an image context. Direct navigation is sandboxed as well:
	// no scripts, external resources, forms or same-origin document privileges.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	send(w, http.StatusOK, data, kind, "no-store")
	return nil
}
