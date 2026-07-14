package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"miniscribe/internal/vad"
)

// Recognizer wraps the OfflineRecognizer.
type Recognizer struct {
	impl      *sherpa.OfflineRecognizer
	modelType string
}

// NewRecognizer initializes the OfflineRecognizer based on the modelType and modelDir.
func NewRecognizer(modelDir string, modelType string, threads int) (*Recognizer, error) {
	config := sherpa.OfflineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: 16000,
			FeatureDim: 80,
		},
		DecodingMethod: "greedy_search",
	}

	config.ModelConfig.NumThreads = threads
	config.ModelConfig.Debug = 0
	config.ModelConfig.Provider = "cpu"

	switch modelType {
	case "moonshine":
		folder := "sherpa-onnx-moonshine-base-en-int8"
		dir := filepath.Join(modelDir, folder)

		preprocessor := filepath.Join(dir, "preprocess.onnx")
		encoder := filepath.Join(dir, "encode.int8.onnx")
		uncachedDecoder := filepath.Join(dir, "uncached_decode.int8.onnx")
		cachedDecoder := filepath.Join(dir, "cached_decode.int8.onnx")
		tokens := filepath.Join(dir, "tokens.txt")

		if !fileExists(preprocessor) || !fileExists(encoder) || !fileExists(uncachedDecoder) || !fileExists(cachedDecoder) || !fileExists(tokens) {
			return nil, fmt.Errorf("missing Moonshine model assets in %s. Please run: speech models pull moonshine", dir)
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
		folder := "sherpa-onnx-whisper-small.en"
		dir := filepath.Join(modelDir, folder)

		encoder := filepath.Join(dir, "small.en-encoder.int8.onnx")
		decoder := filepath.Join(dir, "small.en-decoder.int8.onnx")
		tokens := filepath.Join(dir, "small.en-tokens.txt")

		if !fileExists(encoder) || !fileExists(decoder) || !fileExists(tokens) {
			return nil, fmt.Errorf("missing Whisper model assets in %s. Please run: speech models pull whisper", dir)
		}

		config.ModelConfig.ModelType = "whisper"
		config.ModelConfig.Tokens = tokens
		config.ModelConfig.Whisper = sherpa.OfflineWhisperModelConfig{
			Encoder: encoder,
			Decoder: decoder,
		}

	case "parakeet":
		folder := "sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8"
		dir := filepath.Join(modelDir, folder)

		encoder := filepath.Join(dir, "encoder.int8.onnx")
		decoder := filepath.Join(dir, "decoder.int8.onnx")
		joiner := filepath.Join(dir, "joiner.int8.onnx")
		tokens := filepath.Join(dir, "tokens.txt")

		if !fileExists(encoder) || !fileExists(decoder) || !fileExists(joiner) || !fileExists(tokens) {
			return nil, fmt.Errorf("missing Parakeet model assets in %s. Please run: speech models pull parakeet", dir)
		}

		config.ModelConfig.ModelType = "transducer"
		config.ModelConfig.Tokens = tokens
		config.ModelConfig.Transducer = sherpa.OfflineTransducerModelConfig{
			Encoder: encoder,
			Decoder: decoder,
			Joiner:  joiner,
		}

	default:
		return nil, fmt.Errorf("unsupported model type: %s (choose moonshine, whisper, or parakeet)", modelType)
	}

	impl := sherpa.NewOfflineRecognizer(&config)
	if impl == nil {
		return nil, fmt.Errorf("failed to create OfflineRecognizer")
	}

	return &Recognizer{
		impl:      impl,
		modelType: modelType,
	}, nil
}

// Close releases the recognizer resources.
func (r *Recognizer) Close() {
	if r.impl != nil {
		sherpa.DeleteOfflineRecognizer(r.impl)
		r.impl = nil
	}
}

// Transcribe transcribes a single contiguous audio buffer.
func (r *Recognizer) Transcribe(samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	stream := sherpa.NewOfflineStream(r.impl)
	if stream == nil {
		return "", fmt.Errorf("failed to create OfflineStream")
	}
	defer sherpa.DeleteOfflineStream(stream)

	stream.AcceptWaveform(16000, samples)
	r.impl.Decode(stream)

	res := stream.GetResult()
	return strings.TrimSpace(res.Text), nil
}

// TranscribeWithChunking splits long audio using VAD and transcribes chunks in memory.
func (r *Recognizer) TranscribeWithChunking(samples []float32, vadModelPath string, maxChunkDuration float32, overlap float32) (string, error) {
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

	var results []string
	for _, chunk := range chunks {
		chunkSamples := samples[chunk.StartSample:chunk.EndSample]
		text, err := r.Transcribe(chunkSamples)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk: %w", err)
		}
		if text != "" {
			results = append(results, text)
		}
	}

	return MergeTranscriptions(results), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// MergeTranscriptions stitches transcribed text chunks together while removing overlap.
func MergeTranscriptions(results []string) string {
	if len(results) == 0 {
		return ""
	}
	if len(results) == 1 {
		return results[0]
	}

	cleaned := []string{results[0]}
	for i := 1; i < len(results); i++ {
		cleanedChunk := detectAndRemoveOverlap(results[i-1], results[i])
		if cleanedChunk != "" {
			cleaned = append(cleaned, cleanedChunk)
		}
	}

	return strings.Join(cleaned, " ")
}

func detectAndRemoveOverlap(prevText, currText string) string {
	prevWords := strings.Fields(prevText)
	currWords := strings.Fields(currText)
	if len(prevWords) == 0 || len(currWords) == 0 {
		return currText
	}

	maxOverlap := len(prevWords)
	if len(currWords) < maxOverlap {
		maxOverlap = len(currWords)
	}
	if maxOverlap > 10 {
		maxOverlap = 10
	}

	overlapFound := 0
	for i := 1; i <= maxOverlap; i++ {
		match := true
		for j := 0; j < i; j++ {
			if prevWords[len(prevWords)-i+j] != currWords[j] {
				match = false
				break
			}
		}
		if match {
			overlapFound = i
		}
	}

	if overlapFound > 0 {
		return strings.Join(currWords[overlapFound:], " ")
	}
	return currText
}
