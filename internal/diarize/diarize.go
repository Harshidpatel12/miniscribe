package diarize

import (
	"fmt"
	"os"
	"path/filepath"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"miniscribe/internal/asr"
)

// SegmentResult represents the transcribed speech turn for a specific speaker.
type SegmentResult struct {
	Speaker string  `json:"speaker"`
	Start   float32 `json:"start"`
	End     float32 `json:"end"`
	Text    string  `json:"text"`
}

// Diarize processes the input audio, identifies speakers and transcribes their turns.
func Diarize(samples []float32, modelDir string, r *asr.Recognizer, threads int, numSpeakers int) ([]SegmentResult, error) {
	segDir := filepath.Join(modelDir, "sherpa-onnx-pyannote-segmentation-3-0")
	segModel := filepath.Join(segDir, "model.onnx")
	embModel := filepath.Join(modelDir, "3dspeaker_speech_eres2net_base_sv_zh-cn_3dspeaker_16k.onnx")

	if !fileExists(segModel) {
		return nil, fmt.Errorf("missing Pyannote model at %s. Please run: speech models pull diarization", segModel)
	}
	if !fileExists(embModel) {
		return nil, fmt.Errorf("missing speaker embedding model at %s. Please run: speech models pull diarization", embModel)
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

	// Process diarization to get turns
	segments := sd.Process(samples)

	var results []SegmentResult
	for _, seg := range segments {
		// Slice audio samples for the speaker turn
		startSample := int(seg.Start * 16000)
		endSample := int(seg.End * 16000)
		if startSample < 0 {
			startSample = 0
		}
		if endSample > len(samples) {
			endSample = len(samples)
		}
		if endSample <= startSample {
			continue
		}

		turnSamples := samples[startSample:endSample]
		text, err := r.Transcribe(turnSamples)
		if err != nil {
			return nil, fmt.Errorf("failed ASR for speaker segment (%.2f-%.2f): %w", seg.Start, seg.End, err)
		}

		if text != "" {
			speakerLabel := fmt.Sprintf("SPEAKER_%02d", seg.Speaker)
			results = append(results, SegmentResult{
				Speaker: speakerLabel,
				Start:   seg.Start,
				End:     seg.End,
				Text:    text,
			})
		}
	}

	return results, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
