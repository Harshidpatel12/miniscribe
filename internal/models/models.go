package models

import (
	"archive/tar"
	"compress/bzip2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// ModelInfo describes a model asset to download.
type ModelInfo struct {
	Name        string
	URL         string
	Format      string // "tar.bz2" or "onnx"
	Folder      string // directory name inside the cache (tar.bz2 models)
	Filename    string // file name inside the cache root (onnx models)
	Description string
}

// Catalog lists all supported models.
var Catalog = map[string]ModelInfo{
	"moonshine": {
		Name:        "moonshine",
		URL:         "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-moonshine-base-en-int8.tar.bz2",
		Format:      "tar.bz2",
		Folder:      "sherpa-onnx-moonshine-base-en-int8",
		Description: "Moonshine base English int8 (fast, accurate ASR)",
	},
	"whisper": {
		Name:        "whisper",
		URL:         "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-whisper-small.en.tar.bz2",
		Format:      "tar.bz2",
		Folder:      "sherpa-onnx-whisper-small.en",
		Description: "Whisper small English (very robust ASR)",
	},
	"parakeet": {
		Name:        "parakeet",
		URL:         "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8.tar.bz2",
		Format:      "tar.bz2",
		Folder:      "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8",
		Description: "NeMo Parakeet TDT 0.6b English int8 (high accuracy)",
	},
	"silero-vad": {
		Name:        "silero-vad",
		URL:         "https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx",
		Format:      "onnx",
		Filename:    "silero_vad.onnx",
		Description: "Silero Voice Activity Detection (required for chunking)",
	},
	"diarization-seg": {
		Name:        "diarization-seg",
		URL:         "https://github.com/k2-fsa/sherpa-onnx/releases/download/speaker-segmentation-models/sherpa-onnx-pyannote-segmentation-3-0.tar.bz2",
		Format:      "tar.bz2",
		Folder:      "sherpa-onnx-pyannote-segmentation-3-0",
		Description: "Pyannote Segmentation 3.0 (diarization segmentation)",
	},
	"diarization-emb": {
		Name:        "diarization-emb",
		URL:         "https://github.com/k2-fsa/sherpa-onnx/releases/download/speaker-recongition-models/3dspeaker_speech_eres2net_base_sv_zh-cn_3dspeaker_16k.onnx",
		Format:      "onnx",
		Filename:    "3dspeaker_speech_eres2net_base_sv_zh-cn_3dspeaker_16k.onnx",
		Description: "3D-Speaker ERes2Net sv-zh-cn 16k (diarization embeddings)",
	},
}

// GetCacheDir returns the model cache directory (~/.cache/speech/models or SPEECH_MODEL_DIR).
func GetCacheDir(customDir string) (string, error) {
	if customDir != "" {
		return filepath.Clean(customDir), nil
	}
	if envDir := os.Getenv("SPEECH_MODEL_DIR"); envDir != "" {
		return filepath.Clean(envDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "speech", "models"), nil
}

// IsInstalled reports whether a named model (or model group) is locally cached.
func IsInstalled(cacheDir, modelName string) (bool, error) {
	switch modelName {
	case "moonshine", "whisper", "parakeet":
		return dirExists(filepath.Join(cacheDir, Catalog[modelName].Folder))
	case "silero-vad":
		return fileExists(filepath.Join(cacheDir, Catalog["silero-vad"].Filename))
	case "diarization":
		segOK, err := dirExists(filepath.Join(cacheDir, Catalog["diarization-seg"].Folder))
		if err != nil || !segOK {
			return false, err
		}
		return fileExists(filepath.Join(cacheDir, Catalog["diarization-emb"].Filename))
	default:
		return false, fmt.Errorf("unknown model: %s", modelName)
	}
}

// Pull downloads and installs the named model (or model group) into cacheDir.
func Pull(cacheDir, modelName string, progress io.Writer) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Silero VAD is always required; auto-pull it when missing.
	if modelName != "silero-vad" {
		if ok, _ := IsInstalled(cacheDir, "silero-vad"); !ok {
			fmt.Fprintln(progress, "Auto-pulling Silero VAD (required)...")
			if err := downloadAndExtract(cacheDir, Catalog["silero-vad"], progress); err != nil {
				return fmt.Errorf("failed to auto-pull Silero VAD: %w", err)
			}
		}
	}

	if modelName == "diarization" {
		fmt.Fprintln(progress, "Pulling Diarization Pyannote Segmentation...")
		if err := downloadAndExtract(cacheDir, Catalog["diarization-seg"], progress); err != nil {
			return fmt.Errorf("failed to pull diarization segmentation: %w", err)
		}
		fmt.Fprintln(progress, "Pulling Diarization Speaker Embedding Extractor...")
		if err := downloadAndExtract(cacheDir, Catalog["diarization-emb"], progress); err != nil {
			return fmt.Errorf("failed to pull diarization embedding: %w", err)
		}
		return nil
	}

	info, ok := Catalog[modelName]
	if !ok {
		return fmt.Errorf("unknown model: %s", modelName)
	}

	fmt.Fprintf(progress, "Pulling %s...\n", modelName)
	return downloadAndExtract(cacheDir, info, progress)
}

func downloadAndExtract(cacheDir string, info ModelInfo, progress io.Writer) error {
	fmt.Fprintf(progress, "Downloading from %s...\n", info.URL)

	resp, err := http.Get(info.URL) //nolint:gosec // URL comes from the hard-coded catalog.
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	if info.Format == "onnx" {
		destPath := filepath.Join(cacheDir, info.Filename)
		out, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", destPath, err)
		}
		defer out.Close()

		n, err := io.Copy(out, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}
		fmt.Fprintf(progress, "Downloaded to %s (%d bytes).\n", destPath, n)
		return nil
	}

	// Stream-decompress the .tar.bz2 directly without writing to disk.
	tarReader := tar.NewReader(bzip2.NewReader(resp.Body))
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read error: %w", err)
		}

		target := filepath.Join(cacheDir, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", target, err)
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, header.FileInfo().Mode())
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", target, err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("failed to extract %s: %w", target, err)
			}
			outFile.Close()
		}
	}

	fmt.Fprintf(progress, "Extracted model to %s\n", filepath.Join(cacheDir, info.Folder))
	return nil
}

func dirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}
