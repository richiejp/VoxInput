package localvqe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ModelVariant identifies a published LocalVQE GGUF by version and size,
// e.g. "v1.2-1.3M". The empty value selects DefaultModel.
type ModelVariant string

const (
	ModelV13 ModelVariant = "v1.3-4.8M"
	ModelV12 ModelVariant = "v1.2-1.3M"

	// The compact GTCRN-AEC line for lower-power CPUs. Self-contained
	// ggufs: the v1.4-AEC front-end is embedded, so they load through
	// localvqe_new like the main line. pi-v1 is the full enhancer
	// (AEC + NS + dereverb); pi-aec-v1 cancels echo but keeps noise.
	ModelPiV1    ModelVariant = "pi-v1-49k"
	ModelPiAECV1 ModelVariant = "pi-aec-v1-49k"

	// DefaultModel is used when no version is requested. All variants are
	// bundled at build time; v1.2 is the lighter default.
	DefaultModel ModelVariant = ModelV12
)

const modelBaseURL = "https://huggingface.co/LocalAI-io/LocalVQE/resolve/main"

// modelFileName is the canonical GGUF file name for a variant, shared by the
// bundled-file lookup, the cache path, and the download URL.
func modelFileName(variant ModelVariant) string {
	return fmt.Sprintf("localvqe-%s-f32.gguf", variant)
}

// SupportedModelVariants lists the selectable variants, main line newest
// first, then the compact line.
var SupportedModelVariants = []ModelVariant{ModelV13, ModelV12, ModelPiV1, ModelPiAECV1}

// SHA256 of each variant's f32 GGUF, used to verify the build-time download
// and any runtime cache download.
var modelHashes = map[ModelVariant]string{
	ModelV13:     "c4f7912485c32cfc206c536f2f050b52513f2f613fdbc616391f6b26ab1d51ec",
	ModelV12:     "4856ecf5f522b23fb2bc5caeac81f323c0ef1c4c156a9c7d40a6adbe092ba9ce",
	ModelPiV1:    "0e0c82a8e9703e818b64dedd0fc306394cf5bbb59fcec1ccca82099d352d0c26",
	ModelPiAECV1: "b80b75b9038d0d28079a84afe7d4f0b8f6404f723af4633c05ed4f96fb30b7b8",
}

// modelAliases lets users pass a bare version (e.g. "v1.3") in place of the
// fully-qualified version-size variant.
var modelAliases = map[ModelVariant]ModelVariant{
	"v1.2":      ModelV12,
	"v1.3":      ModelV13,
	"pi-v1":     ModelPiV1,
	"pi-aec-v1": ModelPiAECV1,
}

func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable symlinks: %w", err)
	}
	return filepath.Dir(exe), nil
}

func findFirst(candidates []string) string {
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func libName() string {
	if runtime.GOOS == "darwin" {
		return "liblocalvqe.dylib"
	}
	return "liblocalvqe.so"
}

// EnsureLib returns a path to the localvqe shared library.
// If libPath is non-empty, it's returned as-is (user override).
// Otherwise looks for the library relative to the executable.
func EnsureLib(libPath string) (string, error) {
	if libPath != "" {
		return libPath, nil
	}

	name := libName()
	dir, err := exeDir()
	if err != nil {
		return "", err
	}

	if p := findFirst([]string{
		filepath.Join(dir, name),
		filepath.Join(dir, "..", "lib", name),
		filepath.Join(dir, "lib", name),
	}); p != "" {
		return p, nil
	}

	// Fall back to bare name (system library path / dlopen search)
	return name, nil
}

// EnsureModel returns a path to the GGUF model file.
//
// Resolution order:
//   - modelPath non-empty: returned as-is (explicit user override).
//   - the requested variant (or DefaultModel when none is requested) bundled
//     next to the executable at build time.
//   - failing that, the variant fetched from HuggingFace into the user cache
//     on first use and reused thereafter (covers a plain `go build`).
func EnsureModel(modelPath string, variant ModelVariant) (string, error) {
	if modelPath != "" {
		if _, err := os.Stat(modelPath); err != nil {
			return "", fmt.Errorf("model file not found: %w", err)
		}
		return modelPath, nil
	}

	v, err := resolveVariant(variant)
	if err != nil {
		return "", err
	}

	filename := modelFileName(v)

	dir, err := exeDir()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(dir, "share", "voxinput", filename),
		filepath.Join(dir, "..", "share", "voxinput", filename),
	}
	if p := findFirst(candidates); p != "" {
		return p, nil
	}

	return ensureCached(v)
}

// resolveVariant maps an empty value to DefaultModel and a bare-version alias
// (e.g. "v1.3") to its fully-qualified variant, rejecting unknown values.
func resolveVariant(variant ModelVariant) (ModelVariant, error) {
	if variant == "" {
		return DefaultModel, nil
	}
	if _, ok := modelHashes[variant]; ok {
		return variant, nil
	}
	if full, ok := modelAliases[variant]; ok {
		return full, nil
	}

	names := make([]string, len(SupportedModelVariants))
	for i, v := range SupportedModelVariants {
		names[i] = string(v)
	}
	return "", fmt.Errorf("unknown LocalVQE model version %q (supported: %s)", variant, strings.Join(names, ", "))
}

// ensureCached returns a cached copy of variant, downloading and verifying it
// on first use. variant must already be resolved (present in modelHashes).
func ensureCached(variant ModelVariant) (string, error) {
	wantSum := modelHashes[variant]

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine cache directory: %w", err)
	}
	dst := filepath.Join(cacheDir, "voxinput", modelFileName(variant))

	// Reuse the cached file when it is intact.
	if verifyChecksum(dst, wantSum) == nil {
		return dst, nil
	}

	url := fmt.Sprintf("%s/%s", modelBaseURL, modelFileName(variant))
	log.Printf("localvqe: downloading model %s from %s", variant, url)
	if err := downloadModel(url, dst, wantSum); err != nil {
		return "", fmt.Errorf("downloading LocalVQE model %s: %w", variant, err)
	}
	return dst, nil
}

// verifyChecksum reports whether path exists and its SHA256 matches wantSum.
func verifyChecksum(path, wantSum string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return fmt.Errorf("checksum mismatch for %s: got %s want %s", path, got, wantSum)
	}
	return nil
}

// downloadModel streams url to a temp file in dst's directory, verifies its
// checksum, then atomically renames it into place.
func downloadModel(url, dst, wantSum string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http get: unexpected status %s", resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "localvqe-*.part")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("writing model: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != wantSum {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, wantSum)
	}

	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("installing model: %w", err)
	}
	return nil
}
