package vad

import (
	"fmt"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// Segment represents a speech segment in samples.
type Segment struct {
	StartSample int
	EndSample   int
	StartSec    float32
	EndSec      float32
}

// Chunk represents a slice of the waveform to transcribe.
type Chunk struct {
	StartSample int
	EndSample   int
	StartSec    float32
	EndSec      float32
}

// DetectSpeechSegments runs Silero VAD on the mono 16kHz float32 waveform.
func DetectSpeechSegments(modelPath string, samples []float32, numThreads int) ([]Segment, error) {
	config := sherpa.VadModelConfig{
		SileroVad: sherpa.SileroVadModelConfig{
			Model:              modelPath,
			Threshold:          0.4,
			MinSilenceDuration: 0.5,
			MinSpeechDuration:  0.25,
			WindowSize:         512,
		},
		SampleRate: 16000,
		NumThreads: numThreads,
	}

	// Initialize VAD.
	// A reasonable buffer size for VAD: audio duration + 10 seconds.
	durationSeconds := float32(len(samples)) / 16000.0
	bufferSize := durationSeconds + 10.0
	if bufferSize < 30.0 {
		bufferSize = 30.0
	}

	detector := sherpa.NewVoiceActivityDetector(&config, bufferSize)
	if detector == nil {
		return nil, fmt.Errorf("failed to create VoiceActivityDetector: check if model file exists at %s", modelPath)
	}
	defer sherpa.DeleteVoiceActivityDetector(detector)

	// Feed samples in chunks of 512.
	const feedSamples = 512
	for i := 0; i < len(samples); i += feedSamples {
		end := i + feedSamples
		if end > len(samples) {
			break // Silero VAD requires exact window sizes of 512
		}
		detector.AcceptWaveform(samples[i:end])
	}
	detector.Flush()

	var segments []Segment
	for !detector.IsEmpty() {
		seg := detector.Front()
		segments = append(segments, Segment{
			StartSample: seg.Start,
			EndSample:   seg.Start + len(seg.Samples),
			StartSec:    float32(seg.Start) / 16000.0,
			EndSec:      float32(seg.Start+len(seg.Samples)) / 16000.0,
		})
		detector.Pop()
	}

	return segments, nil
}

// GroupSegments into chunks of maximum maxChunkDuration seconds (with overlap/padding).
func GroupSegments(segments []Segment, totalSamples int, maxChunkDuration float32, overlap float32) []Chunk {
	if len(segments) == 0 {
		return nil
	}

	var chunks []Chunk
	padSamples := int((overlap / 2.0) * 16000)
	budget := maxChunkDuration - overlap
	if budget < 1.0 {
		budget = 1.0
	}

	var currentGroup []Segment
	for _, seg := range segments {
		if len(currentGroup) == 0 {
			currentGroup = append(currentGroup, seg)
			continue
		}

		// Check if adding this segment would exceed the budget duration
		startSec := currentGroup[0].StartSec
		endSec := seg.EndSec
		if (endSec - startSec) > budget {
			// Finalize current group.
			chunks = append(chunks, makeChunk(currentGroup, totalSamples, padSamples))
			currentGroup = []Segment{seg}
		} else {
			currentGroup = append(currentGroup, seg)
		}
	}

	if len(currentGroup) > 0 {
		chunks = append(chunks, makeChunk(currentGroup, totalSamples, padSamples))
	}

	return chunks
}

func makeChunk(group []Segment, totalSamples int, padSamples int) Chunk {
	start := group[0].StartSample - padSamples
	if start < 0 {
		start = 0
	}
	end := group[len(group)-1].EndSample + padSamples
	if end > totalSamples {
		end = totalSamples
	}

	return Chunk{
		StartSample: start,
		EndSample:   end,
		StartSec:    float32(start) / 16000.0,
		EndSec:      float32(end) / 16000.0,
	}
}
