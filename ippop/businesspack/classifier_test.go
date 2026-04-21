package businesspack

import "testing"

func TestClassifierClassify(t *testing.T) {
	classifier := NewClassifier(nil, "")

	tests := []struct {
		name          string
		host          string
		requestedPack string
		want          string
	}{
		{name: "explicit pack wins", host: "github.com", requestedPack: "youtube", want: PackYouTubeStreaming},
		{name: "youtube subdomain", host: "i.ytimg.com", want: PackYouTubeStreaming},
		{name: "github raw", host: "raw.githubusercontent.com", want: PackCodeDownload},
		{name: "research site", host: "pubs.acs.org", want: PackResearchAndNews},
		{name: "host with port", host: "github.com:443", want: PackCodeDownload},
		{name: "unknown host", host: "example.org", want: PackGeneralWeb},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifier.Classify(tt.host, tt.requestedPack); got != tt.want {
				t.Fatalf("Classify(%q, %q) = %q, want %q", tt.host, tt.requestedPack, got, tt.want)
			}
		})
	}
}

func TestNormalizePack(t *testing.T) {
	tests := map[string]string{
		"youtube":        PackYouTubeStreaming,
		"code-download":  PackCodeDownload,
		"social_media":   PackSocialMedia,
		"general-web":    PackGeneralWeb,
		"custom-package": "custom_package",
	}

	for input, want := range tests {
		if got := NormalizePack(input); got != want {
			t.Fatalf("NormalizePack(%q) = %q, want %q", input, got, want)
		}
	}
}
