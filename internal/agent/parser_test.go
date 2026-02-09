package agent

import "testing"

func TestParseProgress(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected float64
		ok       bool
	}{
		{
			name:     "valid ffmpeg output",
			line:     "frame=  171 fps=0.0 q=-1.0 size=    1024kB time=00:00:06.84 bitrate=1225.6kbits/s speed=13.6x out_time_us=1234567",
			expected: 1.234567,
			ok:       true,
		},
		{
			name:     "line without out_time_us",
			line:     "frame=  171 fps=0.0 q=-1.0 size=    1024kB time=00:00:06.84",
			expected: 0,
			ok:       false,
		},
		{
			name:     "invalid out_time_us value",
			line:     "out_time_us=invalid",
			expected: 0,
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseProgress(tt.line)
			if ok != tt.ok {
				t.Errorf("parseProgress() ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.expected {
				t.Errorf("parseProgress() got = %v, want %v", got, tt.expected)
			}
		})
	}
}
