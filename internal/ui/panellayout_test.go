package ui

import "testing"

func TestPanelLayoutThreadFitUsesCombinedMinimumWidths(t *testing.T) {
	tests := []struct {
		name           string
		width          int
		wantAutoHidden bool
		wantMsgWidth   int
		wantThreadW    int
	}{
		{
			name:           "one column below combined minimum auto-hides",
			width:          111,
			wantAutoHidden: true,
			wantMsgWidth:   71,
		},
		{
			name:         "combined minimum boundary clamps thread width",
			width:        112,
			wantMsgWidth: 40,
			wantThreadW:  30,
		},
		{
			name:         "just below natural minimum still clamps thread width",
			width:        121,
			wantMsgWidth: 49,
			wantThreadW:  30,
		},
		{
			name:         "default terminal width keeps thread visible",
			width:        120,
			wantMsgWidth: 48,
			wantThreadW:  30,
		},
		{
			name:         "wide layout keeps proportional split",
			width:        200,
			wantMsgWidth: 102,
			wantThreadW:  56,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := newPanelLayout()
			frame := layout.Compute(tt.width, 30, 6, 30, true, true)

			if frame.ThreadAutoHidden != tt.wantAutoHidden {
				t.Errorf("ThreadAutoHidden = %v, want %v", frame.ThreadAutoHidden, tt.wantAutoHidden)
			}
			if frame.MsgWidth != tt.wantMsgWidth {
				t.Errorf("MsgWidth = %d, want %d", frame.MsgWidth, tt.wantMsgWidth)
			}
			if frame.ThreadWidth != tt.wantThreadW {
				t.Errorf("ThreadWidth = %d, want %d", frame.ThreadWidth, tt.wantThreadW)
			}
		})
	}
}
