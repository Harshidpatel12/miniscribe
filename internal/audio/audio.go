package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
)

// DecodeToPCM decodes any input audio file to 16kHz mono float32 PCM samples using ffmpeg.
// Samples are in the range [-1.0, 1.0].
func DecodeToPCM(inputPath string) ([]float32, error) {
	// Check if ffmpeg is in PATH
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH. Please install ffmpeg: %w", err)
	}

	// Extract raw float32 little-endian PCM
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

	// Read all stdout bytes
	data, err := io.ReadAll(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to read ffmpeg stdout: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg process failed: %w, stderr: %s", err, stderr.String())
	}

	if len(data)%4 != 0 {
		return nil, fmt.Errorf("decoded PCM data length is not a multiple of 4 bytes: got %d", len(data))
	}

	numSamples := len(data) / 4
	samples := make([]float32, numSamples)
	for i := 0; i < numSamples; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		samples[i] = math.Float32frombits(bits)
	}

	return samples, nil
}
