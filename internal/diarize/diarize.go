package diarize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Harshidpatel12/miniscribe/internal/asr"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const sampleRate = 16000

// SegmentResult is the transcribed speech turn for a single speaker.
type SegmentResult struct {
	Speaker string  `json:"speaker"`
	Start   float32 `json:"start"`
	End     float32 `json:"end"`
	Text    string  `json:"text"`
}

// Diarize runs speaker diarization on the audio buffer and transcribes each turn.
func Diarize(samples []float32, modelDir string, r *asr.Recognizer, threads, numSpeakers int) ([]SegmentResult, error) {
	segModel := filepath.Join(modelDir, "sherpa-onnx-pyannote-segmentation-3-0", "model.onnx")
	embModel := filepath.Join(modelDir, "3dspeaker_speech_eres2net_base_sv_zh-cn_3dspeaker_16k.onnx")

	if !isFile(segModel) {
		return nil, fmt.Errorf("missing Pyannote model at %s\nRun: miniscribe models pull diarization", segModel)
	}
	if !isFile(embModel) {
		return nil, fmt.Errorf("missing speaker embedding model at %s\nRun: miniscribe models pull diarization", embModel)
	}

	config := sherpa.OfflineSpeakerDiarizationConfig{
		Segmentation: sherpa.OfflineSpeakerSegmentationModelConfig{
			Pyannote: sherpa.OfflineSpeakerSegmentationPyannoteModelConfig{
				Model: segModel,
			},
			NumThreads: threads,
			Debug:      0,
			Provider:   "cpu",
		},
		Embedding: sherpa.SpeakerEmbeddingExtractorConfig{
			Model:      embModel,
			NumThreads: threads,
			Debug:      0,
			Provider:   "cpu",
		},
		MinDurationOn:  0.3,
		MinDurationOff: 0.5,
	}

	if numSpeakers > 0 {
		config.Clustering.NumClusters = numSpeakers
		config.Clustering.Threshold = -1.0
	} else {
		config.Clustering.NumClusters = -1
		config.Clustering.Threshold = 0.55
	}

	sd := sherpa.NewOfflineSpeakerDiarization(&config)
	if sd == nil {
		return nil, fmt.Errorf("failed to create OfflineSpeakerDiarization engine")
	}
	defer sherpa.DeleteOfflineSpeakerDiarization(sd)

	segments := sd.Process(samples)

	results := make([]SegmentResult, 0, len(segments))
	for _, seg := range segments {
		startSample := clamp(int(seg.Start*sampleRate), 0, len(samples))
		endSample := clamp(int(seg.End*sampleRate), 0, len(samples))
		if endSample <= startSample {
			continue
		}

		text, err := r.Transcribe(samples[startSample:endSample])
		if err != nil {
			return nil, fmt.Errorf("ASR failed for segment %.2f-%.2f: %w", seg.Start, seg.End, err)
		}
		if text != "" {
			results = append(results, SegmentResult{
				Speaker: fmt.Sprintf("SPEAKER_%02d", seg.Speaker),
				Start:   seg.Start,
				End:     seg.End,
				Text:    text,
			})
		}
	}

	return results, nil
}

// clamp restricts v to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist) && err == nil && !info.IsDir()
}
