package asr

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"miniscribe/internal/vad"
)

const sampleRate = 16000

// Recognizer wraps the sherpa-onnx OfflineRecognizer.
type Recognizer struct {
	impl      *sherpa.OfflineRecognizer
	modelType string
}

// NewRecognizer initializes the OfflineRecognizer for the given model type.
func NewRecognizer(modelDir, modelType string, threads int) (*Recognizer, error) {
	config := sherpa.OfflineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: sampleRate,
			FeatureDim: 80,
		},
		DecodingMethod: "greedy_search",
	}
	config.ModelConfig.NumThreads = threads
	config.ModelConfig.Debug = 0
	config.ModelConfig.Provider = "cpu"

	switch modelType {
	case "moonshine":
		dir := filepath.Join(modelDir, "sherpa-onnx-moonshine-base-en-int8")
		preprocessor := filepath.Join(dir, "preprocess.onnx")
		encoder := filepath.Join(dir, "encode.int8.onnx")
		uncachedDecoder := filepath.Join(dir, "uncached_decode.int8.onnx")
		cachedDecoder := filepath.Join(dir, "cached_decode.int8.onnx")
		tokens := filepath.Join(dir, "tokens.txt")

		if err := requireFiles(dir, preprocessor, encoder, uncachedDecoder, cachedDecoder, tokens); err != nil {
			return nil, fmt.Errorf("%w\nRun: miniscribe models pull moonshine", err)
		}

		config.ModelConfig.ModelType = "moonshine"
		config.ModelConfig.Tokens = tokens
		config.ModelConfig.Moonshine = sherpa.OfflineMoonshineModelConfig{
			Preprocessor:    preprocessor,
			Encoder:         encoder,
			UncachedDecoder: uncachedDecoder,
			CachedDecoder:   cachedDecoder,
		}

	case "whisper":
		dir := filepath.Join(modelDir, "sherpa-onnx-whisper-small.en")
		encoder := filepath.Join(dir, "small.en-encoder.int8.onnx")
		decoder := filepath.Join(dir, "small.en-decoder.int8.onnx")
		tokens := filepath.Join(dir, "small.en-tokens.txt")

		if err := requireFiles(dir, encoder, decoder, tokens); err != nil {
			return nil, fmt.Errorf("%w\nRun: miniscribe models pull whisper", err)
		}

		config.ModelConfig.ModelType = "whisper"
		config.ModelConfig.Tokens = tokens
		config.ModelConfig.Whisper = sherpa.OfflineWhisperModelConfig{
			Encoder: encoder,
			Decoder: decoder,
		}

	case "parakeet":
		dir := filepath.Join(modelDir, "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8")
		encoder := filepath.Join(dir, "encoder.int8.onnx")
		decoder := filepath.Join(dir, "decoder.int8.onnx")
		joiner := filepath.Join(dir, "joiner.int8.onnx")
		tokens := filepath.Join(dir, "tokens.txt")

		if err := requireFiles(dir, encoder, decoder, joiner, tokens); err != nil {
			return nil, fmt.Errorf("%w\nRun: miniscribe models pull parakeet", err)
		}

		config.ModelConfig.ModelType = "transducer"
		config.ModelConfig.Tokens = tokens
		config.ModelConfig.Transducer = sherpa.OfflineTransducerModelConfig{
			Encoder: encoder,
			Decoder: decoder,
			Joiner:  joiner,
		}

	default:
		return nil, fmt.Errorf("unsupported model type %q (choose: moonshine, whisper, parakeet)", modelType)
	}

	impl := sherpa.NewOfflineRecognizer(&config)
	if impl == nil {
		return nil, fmt.Errorf("failed to create OfflineRecognizer")
	}

	return &Recognizer{impl: impl, modelType: modelType}, nil
}

// Close releases the underlying recognizer resources.
func (r *Recognizer) Close() {
	if r.impl != nil {
		sherpa.DeleteOfflineRecognizer(r.impl)
		r.impl = nil
	}
}

// Transcribe decodes a contiguous float32 audio buffer to text.
func (r *Recognizer) Transcribe(samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	stream := sherpa.NewOfflineStream(r.impl)
	if stream == nil {
		return "", fmt.Errorf("failed to create OfflineStream")
	}
	defer sherpa.DeleteOfflineStream(stream)

	stream.AcceptWaveform(sampleRate, samples)
	r.impl.Decode(stream)

	return strings.TrimSpace(stream.GetResult().Text), nil
}

// TranscribeWithChunking splits long audio using Silero VAD then stitches the results.
func (r *Recognizer) TranscribeWithChunking(samples []float32, vadModelPath string, maxChunkDuration, overlap float32) (string, error) {
	segments, err := vad.DetectSpeechSegments(vadModelPath, samples, 1)
	if err != nil {
		return "", fmt.Errorf("VAD segmentation failed: %w", err)
	}
	if len(segments) == 0 {
		return "", nil
	}

	chunks := vad.GroupSegments(segments, len(samples), maxChunkDuration, overlap)
	if len(chunks) == 0 {
		return "", nil
	}

	results := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		text, err := r.Transcribe(samples[chunk.StartSample:chunk.EndSample])
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk [%.1f-%.1fs]: %w", chunk.StartSec, chunk.EndSec, err)
		}
		if text != "" {
			results = append(results, text)
		}
	}

	return MergeTranscriptions(results), nil
}

// MergeTranscriptions joins chunk results, removing word-level overlap at boundaries.
func MergeTranscriptions(results []string) string {
	switch len(results) {
	case 0:
		return ""
	case 1:
		return results[0]
	}

	merged := make([]string, 0, len(results))
	merged = append(merged, results[0])
	for i := 1; i < len(results); i++ {
		if deduped := detectAndRemoveOverlap(results[i-1], results[i]); deduped != "" {
			merged = append(merged, deduped)
		}
	}
	return strings.Join(merged, " ")
}

// detectAndRemoveOverlap removes the prefix of currText that duplicates the
// suffix of prevText (up to 10 words of look-back).
func detectAndRemoveOverlap(prevText, currText string) string {
	prevWords := strings.Fields(prevText)
	currWords := strings.Fields(currText)
	if len(prevWords) == 0 || len(currWords) == 0 {
		return currText
	}

	maxOverlap := min(len(prevWords), len(currWords), 10)
	overlapFound := 0
	for i := 1; i <= maxOverlap; i++ {
		if prevWords[len(prevWords)-i:] != nil && wordsMatch(prevWords[len(prevWords)-i:], currWords[:i]) {
			overlapFound = i
		}
	}

	if overlapFound > 0 {
		return strings.Join(currWords[overlapFound:], " ")
	}
	return currText
}

func wordsMatch(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// requireFiles returns an error if any of the provided file paths are missing.
func requireFiles(modelDir string, paths ...string) error {
	for _, p := range paths {
		if !isFile(p) {
			return fmt.Errorf("missing model asset %q in %s", filepath.Base(p), modelDir)
		}
	}
	return nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist) && err == nil && !info.IsDir()
}

func min(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
