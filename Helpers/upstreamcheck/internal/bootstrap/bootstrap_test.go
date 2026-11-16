package bootstrap

import "testing"

func TestFeatureForSubject(t *testing.T) {
	tests := []struct {
		subject string
		feature string
		title   string
	}{
		{":unicorn: Remove WebGL API", "webgl", "WebGL API"},
		{":unicorn: Remove Media Capture and Media Streams API", "media-capture", "Media Capture and Media Streams API"},
		{":rocket: Removed most Quirks", "quirks", "most Quirks"},
		{":bug: Fix for IndexedDB Origin Leak", "maintenance", "Fix for IndexedDB Origin Leak"},
		{":unicorn: Remove Web RTC API", "webrtc", "Web RTC API"},
	}
	for _, test := range tests {
		feature, title := featureForSubject(test.subject)
		if feature != test.feature || title != test.title {
			t.Errorf("featureForSubject(%q) = (%q, %q), want (%q, %q)", test.subject, feature, title, test.feature, test.title)
		}
	}
}
