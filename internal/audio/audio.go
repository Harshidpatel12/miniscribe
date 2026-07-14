package audio

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"unsafe"
)

// DecodeToPCM decodes any input audio file to 16kHz mono float32 PCM samples
// using ffmpeg. Samples are in the range [-1.0, 1.0].
func DecodeToPCM(inputPath string) ([]float32, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", inputPath,
		"-f", "f32le",
		"-ac", "1",
		"-ar", "16000",
		"-",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	data, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to read ffmpeg stdout: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w\nstderr: %s", err, stderr.String())
	}

	if len(data)%4 != 0 {
		return nil, fmt.Errorf("decoded PCM byte length %d is not a multiple of 4", len(data))
	}

	// Reinterpret the raw []byte as []float32 without copying.
	// f32le output from ffmpeg is native float32 little-endian which is the
	// same representation as Go's float32 on all supported platforms (x86, ARM).
	numSamples := len(data) / 4
	samples := unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), numSamples)

	// Copy into a new slice so the GC can free the original []byte backing array.
	out := make([]float32, numSamples)
	copy(out, samples)
	return out, nil
}
