package main

import (
	"testing"
)

func TestIsValidVideoFile(t *testing.T) {
	tests := []struct {
		filename string
		expected bool
	}{
		{"video.mp4", true},
		{"video.avi", true},
		{"video.mov", true},
		{"video.mkv", true},
		{"video.wmv", true},
		{"video.flv", true},
		{"video.webm", true},
		{"document.pdf", false},
		{"image.jpg", false},
		{"archive.zip", false},
		{"file.txt", false},
		{"VIDEO.MP4", true},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := isValidVideoFile(tt.filename)
			if result != tt.expected {
				t.Errorf("isValidVideoFile(%q) = %v, want %v", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestOutputKey(t *testing.T) {
	jobID := "12345"
	result := outputKey(jobID)
	expected := "frames_12345.zip"
	if result != expected {
		t.Errorf("outputKey(%q) = %q, want %q", jobID, result, expected)
	}
}

func TestEnvOr(t *testing.T) {
	result := envOr("NON_EXISTENT_VAR_XYZ", "default")
	if result != "default" {
		t.Errorf("envOr() = %q, want %q", result, "default")
	}
}
