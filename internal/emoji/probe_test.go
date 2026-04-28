package emoji

import (
	"testing"
)

func TestParseDSRResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantCol int
		wantErr bool
	}{
		{"valid response", []byte("\x1b[1;3R"), 3, false},
		{"valid with extra row digits", []byte("\x1b[42;5R"), 5, false},
		{"valid col=1", []byte("\x1b[1;1R"), 1, false},
		{"valid wide", []byte("\x1b[1;200R"), 200, false},
		{"empty", []byte(""), 0, true},
		{"missing terminator", []byte("\x1b[1;3"), 0, true},
		{"missing semicolon", []byte("\x1b[13R"), 0, true},
		{"missing CSI", []byte("1;3R"), 0, true},
		{"col not a number", []byte("\x1b[1;xR"), 0, true},
		{"col zero", []byte("\x1b[1;0R"), 0, true},
		{"col negative", []byte("\x1b[1;-1R"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := parseDSRResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDSRResponse(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if col != tt.wantCol {
				t.Errorf("parseDSRResponse(%q) col = %d, want %d", tt.input, col, tt.wantCol)
			}
		})
	}
}
