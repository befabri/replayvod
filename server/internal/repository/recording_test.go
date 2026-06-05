package repository

import "testing"

func TestNormalizeRecordingType(t *testing.T) {
	cases := map[string]string{
		"video":   RecordingTypeVideo,
		"audio":   RecordingTypeAudio,
		"":        RecordingTypeVideo,
		"AUDIO":   RecordingTypeVideo, // case-sensitive: unknown defaults to video
		"unknown": RecordingTypeVideo,
	}
	for in, want := range cases {
		if got := NormalizeRecordingType(in); got != want {
			t.Errorf("NormalizeRecordingType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScheduleForceH264 pins the single source of truth for the audio rule: the
// H.264 override only applies to video, so audio always reports false regardless
// of the stored flag.
func TestScheduleForceH264(t *testing.T) {
	cases := []struct {
		recordingType string
		forceH264     bool
		want          bool
	}{
		{RecordingTypeVideo, true, true},
		{RecordingTypeVideo, false, false},
		{RecordingTypeAudio, true, false},
		{RecordingTypeAudio, false, false},
		{"", true, true},      // unknown normalizes to video
		{"bogus", true, true}, // unknown normalizes to video
	}
	for _, tc := range cases {
		if got := ScheduleForceH264(tc.recordingType, tc.forceH264); got != tc.want {
			t.Errorf("ScheduleForceH264(%q, %v) = %v, want %v", tc.recordingType, tc.forceH264, got, tc.want)
		}
	}
}

func TestNormalizeRecordingSettings(t *testing.T) {
	cases := []struct {
		name string
		in   RecordingSettingsInput
		want RecordingSettings
	}{
		{
			name: "video preserves quality and h264",
			in:   RecordingSettingsInput{RecordingType: RecordingTypeVideo, Quality: QualityMedium, ForceH264: true},
			want: RecordingSettings{RecordingType: RecordingTypeVideo, Quality: QualityMedium, ForceH264: true},
		},
		{
			name: "audio clears h264",
			in:   RecordingSettingsInput{RecordingType: RecordingTypeAudio, Quality: QualityLow, ForceH264: true},
			want: RecordingSettings{RecordingType: RecordingTypeAudio, Quality: QualityLow, ForceH264: false},
		},
		{
			name: "empty values default to video high",
			in:   RecordingSettingsInput{ForceH264: true},
			want: RecordingSettings{RecordingType: RecordingTypeVideo, Quality: QualityHigh, ForceH264: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeRecordingSettings(tc.in); got != tc.want {
				t.Fatalf("NormalizeRecordingSettings(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
