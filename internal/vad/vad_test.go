package vad

import (
	"testing"
)

func TestGroupSegments(t *testing.T) {
	segments := []Segment{
		{StartSample: 0, EndSample: 16000 * 2, StartSec: 0, EndSec: 2},
		{StartSample: 16000 * 3, EndSample: 16000 * 5, StartSec: 3, EndSec: 5},
		{StartSample: 16000 * 10, EndSample: 16000 * 20, StartSec: 10, EndSec: 20},
		{StartSample: 16000 * 22, EndSample: 16000 * 25, StartSec: 22, EndSec: 25},
	}

	// Max chunk duration = 10s, overlap = 2s. Budget is 8s.
	// Chunk 1 should contain segment 0 (0-2s) and 1 (3-5s). Span is 5s <= 8s.
	// Segment 2 (10-20s) starts a new group.
	// Segment 3 (22-25s) starts a new group.
	chunks := GroupSegments(segments, 16000*30, 10.0, 2.0)

	if len(chunks) != 3 {
		t.Errorf("Expected 3 chunks, got %d", len(chunks))
	}

	// Let's check Chunk 1:
	// start = group[0].StartSample - padSamples (0 - 16000 = 0)
	// end = group[len(group)-1].EndSample + padSamples (16000*5 + 16000 = 16000*6)
	if chunks[0].StartSample != 0 || chunks[0].EndSample != 16000*6 {
		t.Errorf("Chunk 0 sample bounds mismatch: expected [0, 96000], got [%d, %d]", chunks[0].StartSample, chunks[0].EndSample)
	}

	// Let's check Chunk 2:
	// start = 16000*10 - 16000 = 16000*9
	// end = 16000*20 + 16000 = 16000*21
	if chunks[1].StartSample != 16000*9 || chunks[1].EndSample != 16000*21 {
		t.Errorf("Chunk 1 sample bounds mismatch: expected [144000, 336000], got [%d, %d]", chunks[1].StartSample, chunks[1].EndSample)
	}
}
