// Package tools resolves the bundled media tools, extracting them from the
// executable on first use and falling back to PATH for development builds.
package tools

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// BuildHash identifies the embedded tool set. It is overridden at release
// time via -ldflags.
var BuildHash = "dev"

//go:generate true

// ToolNames lists the executables mins can invoke.
var ToolNames = []string{
	"ffmpeg", "ffprobe", "visqol",
	"cwebp", "dwebp", "img2webp", "anim_dump",
	"avifenc", "avifdec",
	"cjxl", "djxl",
	"heif-convert", "heif-info",
}

// ModelName is the bundled ViSQOL SVR model.
const ModelName = "libsvm_nu_svr_model.txt"

// Resolver finds tools and owns the extraction cache.
type Resolver struct {
	prepMu     sync.Mutex
	mu         sync.Mutex
	cacheDir   string
	extracted  bool
	extractErr error
	embedded   fs.FS
	subdir     string
	paths      map[string]string
	modelPath  string
}

// New returns a resolver for the current platform. embeddedFS is the
// platform-specific asset tree selected by the build-tag file.
func New(embeddedFS fs.FS, subdir string) *Resolver {
	return &Resolver{embedded: embeddedFS, subdir: subdir, paths: map[string]string{}}
}

// CacheDir returns the extraction directory.
func (r *Resolver) CacheDir() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cacheDirLocked()
}

func (r *Resolver) cacheDirLocked() string {
	if r.cacheDir != "" {
		return r.cacheDir
	}
	if v := os.Getenv("MINS_TOOLS_DIR"); v != "" {
		r.cacheDir = v
		return r.cacheDir
	}
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	r.cacheDir = filepath.Join(base, "mins", "2.0.0-"+BuildHash)
	return r.cacheDir
}

func exeName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

// Prepare extracts embedded assets once. Missing assets are not an error:
// development builds fall back to PATH.
//
// When the platform asset tree carries a sha256sums.txt manifest, every
// cached file is verified against it and re-extracted when it is missing or
// does not match, so a half-written or tampered cache self-heals.
func (r *Resolver) Prepare() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extracted {
		return r.extractErr
	}
	r.extracted = true
	if r.subdir == "" {
		return nil
	}
	src := "assets/" + r.subdir
	entries, err := fs.ReadDir(r.embedded, src)
	if err != nil {
		return nil
	}
	dir := r.cacheDirLocked()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.extractErr = err
		return err
	}
	sums, _ := r.readManifest(src)
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".keep" || e.Name() == manifestName {
			continue
		}
		outName := strings.TrimSuffix(e.Name(), ".gz")
		dst := filepath.Join(dir, outName)
		want := sums[outName]
		if _, err := os.Stat(dst); err == nil {
			if want == "" {
				continue
			}
			if got, err := HashFile(dst); err == nil && got == want {
				continue
			}
			_ = os.Remove(dst)
		}
		data, err := fs.ReadFile(r.embedded, src+"/"+e.Name())
		if err != nil {
			continue
		}
		if strings.HasSuffix(e.Name(), ".gz") {
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				continue
			}
			raw, err := io.ReadAll(zr)
			_ = zr.Close()
			if err != nil {
				continue
			}
			data = raw
		}
		if want != "" {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != want {
				r.extractErr = fmt.Errorf("embedded %s failed SHA-256 verification", outName)
				return r.extractErr
			}
		}
		tmp := dst + ".part"
		if err := os.WriteFile(tmp, data, 0o755); err != nil {
			continue
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			continue
		}
	}
	return nil
}

const manifestName = "sha256sums.txt"

// readManifest parses an embedded sha256sums.txt ("<hex>  <name>" per line).
func (r *Resolver) readManifest(src string) (map[string]string, error) {
	data, err := fs.ReadFile(r.embedded, src+"/"+manifestName)
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	return sums, nil
}

// Path resolves one tool. Resolution order: MINS_<NAME> override,
// MINS_TOOLS_DIR, the extraction cache, then PATH.
func (r *Resolver) Path(name string) (string, error) {
	r.mu.Lock()
	if p, ok := r.paths[name]; ok {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()
	_ = r.Prepare()
	envKey := "MINS_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name))
	if v := os.Getenv(envKey); v != "" {
		if _, err := os.Stat(v); err == nil {
			r.remember(name, v)
			return v, nil
		}
		return "", fmt.Errorf("%s is set to %q but that file does not exist", envKey, v)
	}
	// MINS_TOOLS_DIR and the extraction cache.
	cache := r.CacheDir()
	candidates := []string{
		filepath.Join(cache, exeName(name)),
		filepath.Join(cache, name),
	}
	if v := os.Getenv("MINS_TOOLS_DIR"); v != "" {
		candidates = append([]string{filepath.Join(v, exeName(name)), filepath.Join(v, name)}, candidates...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			r.remember(name, c)
			return c, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		r.remember(name, p)
		return p, nil
	}
	return "", fmt.Errorf("required tool not found: %s (bundle it, set %s, or put it on PATH)", name, envKey)
}

func (r *Resolver) remember(name, path string) {
	r.mu.Lock()
	r.paths[name] = path
	r.mu.Unlock()
}

// MustPath is Path for callers that treat a missing tool as fatal.
func (r *Resolver) MustPath(name string) string {
	p, err := r.Path(name)
	if err != nil {
		panic(err)
	}
	return p
}

// Model resolves the ViSQOL SVR model path.
func (r *Resolver) Model() (string, error) {
	r.mu.Lock()
	if r.modelPath != "" {
		p := r.modelPath
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()
	if v := os.Getenv("MINS_VISQOL_MODEL"); v != "" {
		r.setModel(v)
		return v, nil
	}
	if v := os.Getenv("VISQOL_MODEL"); v != "" {
		r.setModel(v)
		return v, nil
	}
	_ = r.Prepare()
	p := filepath.Join(r.CacheDir(), ModelName)
	if _, err := os.Stat(p); err == nil {
		r.setModel(p)
		return p, nil
	}
	home, _ := os.UserHomeDir()
	p = filepath.Join(home, "src", "visqol", "model", ModelName)
	if _, err := os.Stat(p); err == nil {
		r.setModel(p)
		return p, nil
	}
	return "", fmt.Errorf("ViSQOL model file not found (bundle it, set MINS_VISQOL_MODEL, or --visqol-model)")
}

func (r *Resolver) setModel(p string) {
	r.mu.Lock()
	r.modelPath = p
	r.mu.Unlock()
}

// HashFile returns a hex SHA-256 for verification/reporting.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
