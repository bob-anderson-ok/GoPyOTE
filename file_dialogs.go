package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

// ForcedVariantTheme delegates everything to Base, but forces Color() to use Variant.
// This replaces deprecated theme.DarkTheme()/theme.LightTheme() calls.
type ForcedVariantTheme struct {
	Base    fyne.Theme
	Variant fyne.ThemeVariant
}

func (t *ForcedVariantTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Base.Color(name, t.Variant)
}

func (t *ForcedVariantTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.Base.Font(style)
}

func (t *ForcedVariantTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.Base.Icon(name)
}

func (t *ForcedVariantTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.Base.Size(name)
}

// Maximum number of recent folders to keep
const maxRecentFolders = 6

// getRecentFolders retrieves the list of recent folders from preferences
func getRecentFolders(prefs fyne.Preferences) []string {
	var folders []string
	for i := 0; i < maxRecentFolders; i++ {
		folder := prefs.String(fmt.Sprintf("recentFolder%d", i))
		if folder == "" {
			continue
		}
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			continue
		}
		folders = append(folders, folder)
	}
	return folders
}

// saveRecentFolders saves the list of recent folders to preferences
func saveRecentFolders(prefs fyne.Preferences, folders []string) {
	for i := 0; i < maxRecentFolders; i++ {
		if i < len(folders) {
			prefs.SetString(fmt.Sprintf("recentFolder%d", i), folders[i])
		} else {
			prefs.SetString(fmt.Sprintf("recentFolder%d", i), "")
		}
	}
}

// addRecentFolder adds a folder to the recent list (pushdown stack behavior)
func addRecentFolder(prefs fyne.Preferences, folderPath string) {
	// Don't add internal application directories to the recent list
	base := filepath.Base(folderPath)
	if base == "OCCULTATION-PARAMETERS" || base == "SITE-FILES" {
		return
	}

	folders := getRecentFolders(prefs)

	// Remove if already exists (to move it to the top)
	newFolders := []string{folderPath}
	for _, f := range folders {
		if f != folderPath {
			newFolders = append(newFolders, f)
		}
	}

	// Limit to max size
	if len(newFolders) > maxRecentFolders {
		newFolders = newFolders[:maxRecentFolders]
	}

	saveRecentFolders(prefs, newFolders)
}

// showFileOpenWithRecents shows a dialog with recent folders, then opens the file dialog.
// homeDir is the observation home directory (from settings); if non-empty a blue Home button is shown.
func showFileOpenWithRecents(w fyne.Window, prefs fyne.Preferences, title string, filter storage.FileFilter, homeDir string, callback func(fyne.URIReadCloser, error)) {
	folders := getRecentFolders(prefs)

	// If no recent folders and no home dir, show the file dialog directly
	if len(folders) == 0 && homeDir == "" {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if reader != nil && err == nil {
				// Add the parent folder to recents
				folderPath := filepath.Dir(reader.URI().Path())
				addRecentFolder(prefs, folderPath)
			}
			callback(reader, err)
		}, w)
		if filter != nil {
			fileDialog.SetFilter(filter)
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
		return
	}

	// Create buttons for each recent folder
	var buttons []fyne.CanvasObject
	var customDialog *dialog.CustomDialog

	// Helper to open the file dialog at a specific location
	openAtLocation := func(folderPath string) {
		customDialog.Hide()
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if reader != nil && err == nil {
				// Add the parent folder to recents
				newFolderPath := filepath.Dir(reader.URI().Path())
				addRecentFolder(prefs, newFolderPath)
			}
			callback(reader, err)
		}, w)
		if filter != nil {
			fileDialog.SetFilter(filter)
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))

		// Set the starting location if the folder exists
		if folderPath != "" {
			folderURI := storage.NewFileURI(folderPath)
			listableURI, err := storage.ListerForURI(folderURI)
			if err == nil {
				fileDialog.SetLocation(listableURI)
			}
		}
		fileDialog.Show()
	}

	// Add a Home button with a directory label
	homeBtn := widget.NewButton("Home", func() {
		if info, err := os.Stat(homeDir); err != nil || !info.IsDir() {
			dialog.ShowError(fmt.Errorf("Home directory not found:\n%s", homeDir), w)
			return
		}
		openAtLocation(homeDir)
	})
	homeBtn.Importance = widget.HighImportance
	var homeDirLabel *widget.Label
	if homeDir != "" {
		homeDirLabel = widget.NewLabel(homeDir)
	} else {
		homeDirLabel = widget.NewLabel("use the Settings tab to set the Home directory")
		homeDirLabel.TextStyle = fyne.TextStyle{Italic: true}
		homeBtn.Disable()
	}
	buttons = append(buttons, container.NewHBox(homeBtn, homeDirLabel))

	// Add separator
	buttons = append(buttons, widget.NewSeparator())

	// Add label for recent folders
	buttons = append(buttons, widget.NewLabel("Recent folders (click to select):"))

	// Add a button for each recent folder
	for _, folder := range folders {
		folderCopy := folder // Capture for closure
		// Show an abbreviated path for display
		displayName := folder
		if len(displayName) > 135 {
			displayName = "..." + displayName[len(displayName)-132:]
		}
		btn := widget.NewButton(displayName, func() {
			openAtLocation(folderCopy)
		})
		btn.Importance = widget.LowImportance
		buttons = append(buttons, btn)
	}

	// Add a Clear history button
	buttons = append(buttons, widget.NewSeparator())
	clearHistoryBtn := widget.NewButton("Clear history", func() {
		saveRecentFolders(prefs, nil)
		customDialog.Hide()
	})
	clearHistoryBtn.Importance = widget.HighImportance
	buttons = append(buttons, container.NewHBox(clearHistoryBtn))

	content := container.NewVBox(buttons...)
	customDialog = dialog.NewCustom(title, "Cancel", content, w)
	customDialog.Resize(fyne.NewSize(900, 0))
	customDialog.Show()
}
