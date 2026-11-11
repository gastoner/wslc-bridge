package wslutil

import (
	"strings"
	"testing"
)

func TestDecodeWindowsOutputKeepsUTF8WithAnEmbeddedNUL(t *testing.T) {
	value := "guest\x00tail"
	input := []byte(value)
	if got := DecodeWindowsOutput(input); got != value {
		t.Fatalf("DecodeWindowsOutput() = %q, want %q", got, value)
	}
}

func TestDecodeWindowsOutputDecodesUTF16LEWithoutABOM(t *testing.T) {
	value := "WSL version: 2.9.3.0"
	encoded := make([]byte, 0, len(value)*2)
	for _, character := range value {
		encoded = append(encoded, byte(character), 0)
	}
	if got := DecodeWindowsOutput(encoded); !strings.EqualFold(got, value) {
		t.Fatalf("DecodeWindowsOutput() = %q, want %q", got, value)
	}
}
