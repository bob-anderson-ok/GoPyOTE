package main

import "testing"

func TestIsNewerInstallableRelease(t *testing.T) {
	exe := []Asset{{Name: expectedAssetName()}}
	tests := []struct {
		name    string
		rel     *Release
		current string
		want    bool
	}{
		{"newer patch", &Release{TagName: "v1.3.8", Assets: exe}, "1.3.7", true},
		{"newer minor", &Release{TagName: "v1.4.0", Assets: exe}, "1.3.7", true},
		{"same version", &Release{TagName: "v1.3.7", Assets: exe}, "1.3.7", false},
		{"older (running a dev build)", &Release{TagName: "v1.3.7", Assets: exe}, "1.3.8", false},
		{"newer but pre-release", &Release{TagName: "v1.3.8", Prerelease: true, Assets: exe}, "1.3.7", false},
		{"newer but draft", &Release{TagName: "v1.3.8", Draft: true, Assets: exe}, "1.3.7", false},
		{"newer but no executable", &Release{TagName: "v1.3.8", Assets: []Asset{{Name: "notes.txt"}}}, "1.3.7", false},
		{"nil release", nil, "1.3.7", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := isNewerInstallableRelease(tc.rel, tc.current)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
