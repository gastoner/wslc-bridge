package wslutil

import (
	"strings"
	"unicode/utf16"
)

func DecodeWindowsOutput(data []byte) string {
	if len(data) >= 2 {
		if data[0] == 0xff && data[1] == 0xfe {
			data = data[2:]
		} else if data[0] == 0xfe && data[1] == 0xff {
			units := make([]uint16, 0, len(data)/2)
			for i := 2; i+1 < len(data); i += 2 {
				units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
			}
			return strings.TrimSpace(string(utf16.Decode(units)))
		}
	}
	if len(data) > 1 {
		// Windows tools emit UTF-16LE without a BOM. ASCII-dominated output is
		// roughly half NUL bytes, so require a meaningful share before assuming
		// UTF-16: a single NUL can appear in otherwise valid UTF-8 output.
		zeros := 0
		for _, value := range data {
			if value == 0 {
				zeros++
			}
		}
		if zeros*4 >= len(data) {
			units := make([]uint16, 0, len(data)/2)
			for i := 0; i+1 < len(data); i += 2 {
				units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
			}
			return strings.TrimSpace(string(utf16.Decode(units)))
		}
	}
	return strings.TrimSpace(string(data))
}
