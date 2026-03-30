package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	Owner     = "bob-anderson-ok"
	Repo      = "GoPyOTE"
	UserAgent = "GoPyOTE-updater/1.0"
)

var CurrentVersion = Version // better: inject with -ldflags

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func ShowUpdateDialogTwoPane(parent fyne.Window) {
	var selectable []Release
	selectedIndex := -1

	// Left pane: release list
	listTitle := widget.NewLabel("Available versions")

	releaseList := widget.NewList(
		func() int {
			return len(selectable)
		},
		func() fyne.CanvasObject {
			title := widget.NewLabel("version")
			title.Wrapping = fyne.TextWrapBreak
			sub := widget.NewLabel("date")
			sub.Wrapping = fyne.TextWrapBreak
			return container.NewVBox(title, sub)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(selectable) {
				return
			}
			r := selectable[id]
			box := obj.(*fyne.Container)
			title := box.Objects[0].(*widget.Label)
			sub := box.Objects[1].(*widget.Label)

			label := r.TagName
			if r.Prerelease {
				label += "  [pre]"
			}
			if normalizeVersion(r.TagName) == normalizeVersion(CurrentVersion) {
				label += "  [current]"
			}
			title.SetText(label)
			sub.SetText(r.PublishedAt.Format("2006-01-02 15:04 MST"))
		},
	)

	// Right pane: details
	header := widget.NewLabel("Release details")
	header.TextStyle = fyne.TextStyle{Bold: true}

	meta := widget.NewLabel("")
	meta.Wrapping = fyne.TextWrapWord

	assetsBox := widget.NewLabel("")
	assetsBox.Wrapping = fyne.TextWrapWord

	notes := widget.NewLabel("")
	notes.Wrapping = fyne.TextWrapWord

	status := widget.NewLabel("Loading releases...")
	status.Wrapping = fyne.TextWrapWord

	progress := widget.NewProgressBarInfinite()
	progress.Hide()

	refreshBtn := widget.NewButton("Refresh", nil)

	updateRightPane := func() {
		if selectedIndex < 0 || selectedIndex >= len(selectable) {
			meta.SetText("")
			assetsBox.SetText("")
			notes.SetText("")
			return
		}

		r := selectable[selectedIndex]

		var mb strings.Builder
		_, _ = fmt.Fprintf(&mb, "Version: %s\n", r.TagName)
		if r.Name != "" {
			_, _ = fmt.Fprintf(&mb, "Title: %s\n", r.Name)
		}
		_, _ = fmt.Fprintf(&mb, "Published: %s\n", r.PublishedAt.Format(time.RFC3339))
		_, _ = fmt.Fprintf(&mb, "Pre-release: %v\n", r.Prerelease)
		_, _ = fmt.Fprintf(&mb, "Current installed: %s\n", CurrentVersion)
		meta.SetText(mb.String())

		var ab strings.Builder
		for _, a := range r.Assets {
			_, _ = fmt.Fprintf(&ab, "%s  (%d bytes)\n", a.Name, a.Size)
		}
		assetsBox.SetText(strings.TrimSpace(ab.String()))

		body := strings.TrimSpace(r.Body)
		if body == "" {
			body = "(no release notes)"
		}
		notes.SetText(body)
	}

	releaseList.OnSelected = func(id widget.ListItemID) {
		selectedIndex = id
		updateRightPane()
	}

	leftPane := container.NewBorder(
		listTitle,
		nil,
		nil,
		nil,
		releaseList,
	)

	rightPane := container.NewBorder(
		container.NewVBox(
			header,
			container.NewHBox(refreshBtn),
			status,
			progress,
		),
		nil,
		nil,
		nil,
		container.NewVScroll(
			container.NewVBox(
				widget.NewLabel("Release notes"),
				notes,
				widget.NewSeparator(),
				widget.NewLabel("Metadata"),
				meta,
				widget.NewSeparator(),
				widget.NewLabel("Assets"),
				assetsBox,
			),
		),
	)

	split := container.NewHSplit(leftPane, rightPane)
	split.Offset = 0.32

	loadReleases := func() {
		progress.Show()
		status.SetText("Loading releases from GitHub...")
		selectable = nil
		selectedIndex = -1
		releaseList.Refresh()
		updateRightPane()

		go func() {
			rs, err := getAllReleases(Owner, Repo, "")
			if err != nil {
				fyne.Do(func() {
					progress.Hide()
					status.SetText("Failed to load releases.")
					dialog.ShowError(err, parent)
				})
				return
			}

			wantAsset := expectedAssetName()
			filtered := make([]Release, 0, len(rs))
			for _, r := range rs {
				if r.Draft {
					continue
				}
				if hasAsset(r, wantAsset) {
					filtered = append(filtered, r)
				}
			}

			sort.Slice(filtered, func(i, j int) bool {
				ci, ei := compareSemver(normalizeVersion(filtered[i].TagName), normalizeVersion(filtered[j].TagName))
				if ei == nil && ci != 0 {
					return ci > 0
				}
				return filtered[i].PublishedAt.After(filtered[j].PublishedAt)
			})

			fyne.Do(func() {
				progress.Hide()
				selectable = filtered
				releaseList.Refresh()

				if len(selectable) == 0 {
					status.SetText("No installable releases found.")
					return
				}

				status.SetText(fmt.Sprintf("Found %d installable release(s).", len(selectable)))
				selectedIndex = 0
				releaseList.Select(0)
				updateRightPane()
			})
		}()
	}

	refreshBtn.OnTapped = loadReleases

	d := dialog.NewCustomConfirm(
		fmt.Sprintf("Install Version  (Currently installed version: %s)", CurrentVersion),
		"Install Selected",
		"Cancel",
		split,
		func(ok bool) {
			if !ok {
				return
			}
			if selectedIndex < 0 || selectedIndex >= len(selectable) {
				dialog.ShowInformation("No selection", "Select a version first.", parent)
				return
			}

			selectedTag := selectable[selectedIndex].TagName
			progress.Show()
			status.SetText("Downloading and verifying selected version...")

			go func() {
				err := downloadAndApplyVersion(selectedTag)
				fyne.Do(func() {
					progress.Hide()
					if err != nil {
						status.SetText("Install failed.")
						dialog.ShowError(err, parent)
						return
					}
					status.SetText("Updater launched. Application will exit if replacement proceeds.")
				})
			}()
		},
		parent,
	)
	d.Resize(fyne.NewSize(980, 620))
	d.Show()

	loadReleases()
}

// ---------------- GitHub API ----------------

func getAllReleases(owner, repo, token string) ([]Release, error) {
	var all []Release
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=100&page=%d", owner, repo, page)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/vnd.github+json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("github api status %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}

		var batch []Release
		err = json.NewDecoder(resp.Body).Decode(&batch)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if len(batch) == 0 {
			break
		}

		all = append(all, batch...)
		page++
	}

	return all, nil
}

func getReleaseByTag(owner, repo, tag, token string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s", owner, repo, tag)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github api status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// ---------------- Install flow ----------------

func downloadAndApplyVersion(tag string) error {
	rel, err := getReleaseByTag(Owner, Repo, tag, "")
	if err != nil {
		return err
	}

	exeAsset, err := findAssetByName(rel, expectedAssetName())
	if err != nil {
		return err
	}
	sumAsset, err := findAssetByName(rel, "SHA256SUMS.txt")
	if err != nil {
		return fmt.Errorf("checksum asset missing: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "mytool-update-*")
	if err != nil {
		return err
	}

	newExePath := filepath.Join(tmpDir, exeAsset.Name)
	sumsPath := filepath.Join(tmpDir, sumAsset.Name)

	if err := downloadFile(exeAsset.BrowserDownloadURL, newExePath, ""); err != nil {
		return fmt.Errorf("download executable: %w", err)
	}
	if err := downloadFile(sumAsset.BrowserDownloadURL, sumsPath, ""); err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}

	expectedHash, err := hashFromSHA256SUMS(sumsPath, exeAsset.Name)
	if err != nil {
		return err
	}
	actualHash, err := fileSHA256(newExePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(expectedHash, actualHash) {
		return fmt.Errorf("checksum mismatch: expected %s got %s", expectedHash, actualHash)
	}

	if runtime.GOOS == "windows" {
		return applyWindowsSelfUpdate(newExePath)
	}
	return fmt.Errorf("self-update currently implemented here for Windows only")
}

// ---------------- Helpers ----------------

func expectedAssetName() string {
	base := "GoPyOTE"
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return base + ".exe"
		case "arm64":
			return base + "-windows-arm64.exe"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return base + "-linux-amd64"
		case "arm64":
			return base + "-linux-arm64"
		}
	}
	return fmt.Sprintf("%s-%s-%s", base, runtime.GOOS, runtime.GOARCH)
}

func hasAsset(r Release, want string) bool {
	for _, a := range r.Assets {
		if a.Name == want {
			return true
		}
	}
	return false
}

func findAssetByName(rel *Release, name string) (*Asset, error) {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("asset %q not found in release %s", name, rel.TagName)
}

func downloadFile(url, outPath, token string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFromSHA256SUMS(sumsPath, targetFile string) (string, error) {
	f, err := os.Open(sumsPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		hash := parts[0]
		name := strings.TrimPrefix(parts[len(parts)-1], "*")
		if name == targetFile {
			return hash, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no checksum entry for %q", targetFile)
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "refs/tags/")
	v = strings.TrimPrefix(v, "v")
	return v
}

func compareSemver(a, b string) (int, error) {
	pa, err := parseVersionTriplet(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseVersionTriplet(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1, nil
		}
		if pa[i] > pb[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersionTriplet(v string) ([3]int, error) {
	var out [3]int
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return out, fmt.Errorf("unsupported version format %q", v)
	}
	for i := 0; i < len(parts); i++ {
		_, err := fmt.Sscanf(parts[i], "%d", &out[i])
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// ---------------- Windows self-replacement ----------------

func applyWindowsSelfUpdate(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	currentExe, err = filepath.Abs(currentExe)
	if err != nil {
		return err
	}

	workDir := filepath.Dir(currentExe)
	restartName := filepath.Base(currentExe)
	batPath := filepath.Join(filepath.Dir(newExePath), "replace_and_restart.bat")

	script := fmt.Sprintf(`@echo off
setlocal
timeout /t 2 /nobreak >nul
copy /Y "%s" "%s"
if errorlevel 1 exit /b 1
cd /d "%s"
start "" "%s"
exit /b 0
`, newExePath, currentExe, workDir, restartName)

	if err := os.WriteFile(batPath, []byte(script), 0644); err != nil {
		return err
	}

	cmd := exec.Command("cmd", "/C", "start", "", batPath)
	if err := cmd.Start(); err != nil {
		return err
	}

	os.Exit(0)
	return nil
}
