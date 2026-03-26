package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const VideoRepo = "GoPyOTE-Videos"

type VideoAsset struct {
	Name        string
	DownloadURL string
	Size        int64
}

func showVideoLibraryDialog(parent fyne.Window) {
	var videos []VideoAsset
	selectedIndex := -1

	status := widget.NewLabel("Loading video list from GitHub...")
	status.Wrapping = fyne.TextWrapWord
	progress := widget.NewProgressBarInfinite()

	videoList := widget.NewList(
		func() int { return len(videos) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("video name placeholder")
			name.Wrapping = fyne.TextWrapBreak
			size := widget.NewLabel("size")
			return container.NewVBox(name, size)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(videos) {
				return
			}
			v := videos[id]
			box := obj.(*fyne.Container)
			box.Objects[0].(*widget.Label).SetText(v.Name)
			box.Objects[1].(*widget.Label).SetText(fmt.Sprintf("%.1f MB", float64(v.Size)/(1024*1024)))
		},
	)

	videoList.OnSelected = func(id widget.ListItemID) {
		selectedIndex = id
	}

	content := container.NewBorder(
		container.NewVBox(status, progress),
		nil, nil, nil,
		videoList,
	)

	d := dialog.NewCustomConfirm(
		"Video Library",
		"Download & Play",
		"Cancel",
		content,
		func(ok bool) {
			if !ok {
				return
			}
			if selectedIndex < 0 || selectedIndex >= len(videos) {
				dialog.ShowInformation("No selection", "Please select a video first.", parent)
				return
			}
			downloadAndPlayVideo(videos[selectedIndex], parent)
		},
		parent,
	)
	d.Resize(fyne.NewSize(600, 400))
	d.Show()

	// Fetch all releases from the Videos repo in the background.
	go func() {
		releases, err := getAllReleases(Owner, VideoRepo, "")
		if err != nil {
			fyne.Do(func() {
				progress.Hide()
				status.SetText("Failed to load video list.")
				dialog.ShowError(err, parent)
			})
			return
		}

		var allVideos []VideoAsset
		for _, r := range releases {
			if r.Draft {
				continue
			}
			for _, a := range r.Assets {
				if strings.HasSuffix(strings.ToLower(a.Name), ".mp4") {
					allVideos = append(allVideos, VideoAsset{
						Name:        a.Name,
						DownloadURL: a.BrowserDownloadURL,
						Size:        a.Size,
					})
				}
			}
		}

		sort.Slice(allVideos, func(i, j int) bool {
			return allVideos[i].Name < allVideos[j].Name
		})

		fyne.Do(func() {
			progress.Hide()
			videos = allVideos
			videoList.Refresh()
			if len(videos) == 0 {
				status.SetText("No videos found.")
			} else {
				status.SetText(fmt.Sprintf("Found %d video(s). Select one and click 'Download & Play'.", len(videos)))
			}
		})
	}()
}

func downloadAndPlayVideo(v VideoAsset, parent fyne.Window) {
	statusLabel := widget.NewLabel(fmt.Sprintf("Downloading %s (%.1f MB)...", v.Name, float64(v.Size)/(1024*1024)))
	statusLabel.Wrapping = fyne.TextWrapWord
	progressBar := widget.NewProgressBarInfinite()

	dlg := dialog.NewCustomWithoutButtons("Downloading Video",
		container.NewVBox(statusLabel, progressBar),
		parent,
	)
	dlg.Resize(fyne.NewSize(450, 100))
	dlg.Show()

	go func() {
		tmpDir, err := os.MkdirTemp("", "gopyote-video-*")
		if err != nil {
			fyne.Do(func() {
				dlg.Hide()
				dialog.ShowError(fmt.Errorf("failed to create temp directory: %w", err), parent)
			})
			return
		}

		outPath := filepath.Join(tmpDir, v.Name)
		if err := downloadFile(v.DownloadURL, outPath, ""); err != nil {
			fyne.Do(func() {
				dlg.Hide()
				dialog.ShowError(fmt.Errorf("failed to download video: %w", err), parent)
			})
			return
		}

		cmd := exec.Command("cmd", "/C", "start", "", outPath)
		if err := cmd.Start(); err != nil {
			fyne.Do(func() {
				dlg.Hide()
				dialog.ShowError(fmt.Errorf("failed to open video player: %w", err), parent)
			})
			return
		}

		fyne.Do(func() {
			dlg.Hide()
		})
	}()
}
