package asr

import (
	"testing"
)

func TestDetectAndRemoveOverlap(t *testing.T) {
	tests := []struct {
		prev     string
		curr     string
		expected string
	}{
		{
			prev:     "hello world this is",
			curr:     "this is a test",
			expected: "a test",
		},
		{
			prev:     "hello world this is",
			curr:     "hello world this is a test",
			expected: "a test",
		},
		{
			prev:     "no overlap here",
			curr:     "different sentence completely",
			expected: "different sentence completely",
		},
		{
			prev:     "",
			curr:     "empty previous chunk",
			expected: "empty previous chunk",
		},
	}

	for _, test := range tests {
		res := detectAndRemoveOverlap(test.prev, test.curr)
		if res != test.expected {
			t.Errorf("For prev=%q, curr=%q: expected %q, got %q", test.prev, test.curr, test.expected, res)
		}
	}
}

func TestMergeTranscriptions(t *testing.T) {
	chunks := []string{
		"hello world this is",
		"this is a simple",
		"a simple test for",
		"test for the merge function",
	}

	expected := "hello world this is a simple test for the merge function"
	res := MergeTranscriptions(chunks)
	if res != expected {
		t.Errorf("Expected %q, got %q", expected, res)
	}
}
