package main

import (
	"embed"
	"image"
	"regexp"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	// Import image decoders
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// Example embedded filesystem - in your actual code, use your own embed directive
// //go:embed images/*
// var embeddedImages embed.FS

// MarkdownSegment represents a piece of the Markdown content
type MarkdownSegment struct {
	IsImage bool
	Content string  // Text content or image path
	AltText string  // Alt text for images
	Width   float32 // Image width (0 = use default)
	Height  float32 // Image height (0 = use default)
}

// imageRegex matches Markdown image syntax: ![alt text](image_path) or ![alt text](image_path =WxH)
var imageRegex = regexp.MustCompile(`!\[([^]]*)]\(([^)]+)\)`)

// imageSizeRegex extracts size from an image path: "image.png =600x400" or "image.png =600"
var imageSizeRegex = regexp.MustCompile(`^(.+?)\s*=(\d+)(?:x(\d+))?$`)

// parseMarkdownWithImages parses Markdown text and separates text from image references
func parseMarkdownWithImages(markdown string) []MarkdownSegment {
	var segments []MarkdownSegment

	// Find all image matches
	matches := imageRegex.FindAllStringSubmatchIndex(markdown, -1)

	if len(matches) == 0 {
		// No images, return the whole text as one segment
		return []MarkdownSegment{{IsImage: false, Content: markdown}}
	}

	lastEnd := 0
	for _, match := range matches {
		// match[0]:match[1] is the full match
		// match[2]:match[3] is the alt text
		// match[4]:match[5] is the image path

		// Add text before this image
		if match[0] > lastEnd {
			textBefore := markdown[lastEnd:match[0]]
			if strings.TrimSpace(textBefore) != "" {
				segments = append(segments, MarkdownSegment{
					IsImage: false,
					Content: textBefore,
				})
			}
		}

		// Add the image segment
		altText := markdown[match[2]:match[3]]
		imagePath := markdown[match[4]:match[5]]

		// Parse optional size from an image path: "image.png =600x400" or "image.png =600"
		var width, height float32
		sizeMatch := imageSizeRegex.FindStringSubmatch(imagePath)
		if sizeMatch != nil {
			imagePath = strings.TrimSpace(sizeMatch[1])
			if w, err := strconv.ParseFloat(sizeMatch[2], 32); err == nil {
				width = float32(w)
			}
			if len(sizeMatch) > 3 && sizeMatch[3] != "" {
				if h, err := strconv.ParseFloat(sizeMatch[3], 32); err == nil {
					height = float32(h)
				}
			}
		}

		segments = append(segments, MarkdownSegment{
			IsImage: true,
			Content: imagePath,
			AltText: altText,
			Width:   width,
			Height:  height,
		})

		lastEnd = match[1]
	}

	// Add any remaining text after the last image
	if lastEnd < len(markdown) {
		textAfter := markdown[lastEnd:]
		if strings.TrimSpace(textAfter) != "" {
			segments = append(segments, MarkdownSegment{
				IsImage: false,
				Content: textAfter,
			})
		}
	}

	return segments
}

// loadImageFromEmbedFS loads an image from an embedded filesystem
func loadImageFromEmbedFS(fs embed.FS, path string) (image.Image, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}

	return img, nil
}

// createMarkdownContentWithImages creates a Fyne container from Markdown with images
// If embedFS is nil, images are loaded from the local filesystem
func createMarkdownContentWithImages(markdown string, embedFS *embed.FS) fyne.CanvasObject {
	segments := parseMarkdownWithImages(markdown)

	var objects []fyne.CanvasObject

	for _, seg := range segments {
		if seg.IsImage {
			var img *canvas.Image

			if embedFS != nil {
				// Load from embedded filesystem
				loadedImg, err := loadImageFromEmbedFS(*embedFS, seg.Content)
				if err != nil {
					// Show error placeholder
					errorLabel := widget.NewLabel("[Image not found: " + seg.Content + "]")
					objects = append(objects, errorLabel)
					continue
				}
				img = canvas.NewImageFromImage(loadedImg)
			} else {
				// Load from local filesystem
				img = canvas.NewImageFromFile(seg.Content)
			}

			img.FillMode = canvas.ImageFillContain
			// Use specified size or default to 600x400
			imgWidth := float32(600)
			imgHeight := float32(400)
			if seg.Width > 0 {
				imgWidth = seg.Width
			}
			if seg.Height > 0 {
				imgHeight = seg.Height
			} else if seg.Width > 0 {
				// If only width specified, maintain the aspect ratio (use 2:3 ratio as default)
				imgHeight = seg.Width * 2 / 3
			}
			img.SetMinSize(fyne.NewSize(imgWidth, imgHeight))

			// Wrap the image in a container with an optional caption
			if seg.AltText != "" {
				caption := widget.NewLabelWithStyle(seg.AltText, fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
				imgWithCaption := container.NewVBox(img, caption)
				objects = append(objects, imgWithCaption)
			} else {
				objects = append(objects, img)
			}
		} else {
			// Render text as Markdown using Fyne's RichText
			richText := widget.NewRichTextFromMarkdown(seg.Content)
			richText.Wrapping = fyne.TextWrapWord
			objects = append(objects, richText)
		}
	}

	// Arrange all objects vertically
	content := container.NewVBox(objects...)

	return content
}

// ShowMarkdownDialogWithImages displays a resizable window with Markdown content including images
func ShowMarkdownDialogWithImages(title, markdown string, embedFS *embed.FS, parent fyne.Window) {
	content := createMarkdownContentWithImages(markdown, embedFS)

	// Wrap in a scroll container for long content
	scroll := container.NewVScroll(content)

	// Create a new resizable window instead of a dialog
	helpWindow := fyne.CurrentApp().NewWindow(title)
	helpWindow.SetContent(scroll)
	helpWindow.Resize(fyne.NewSize(900, 500))

	// Resize relative to the parent if possible
	if parent != nil {
		parentPos := parent.Canvas().Size()
		helpWindow.Resize(fyne.NewSize(
			min(900, parentPos.Width*0.8),
			min(500, parentPos.Height*0.8),
		))
	}

	helpWindow.CenterOnScreen()
	helpWindow.Show()
}

// Example usage - uncomment and modify for your use case:
/*
//go:embed help_images/*
var helpImages embed.FS

func showHelpDialog(w fyne.Window) {
	helpText := `# Getting Started

Welcome to the application! Here's how to use it:

## Loading Data

Click the **File** menu and select **Open** to load your data file.

![Loading data](help_images/load_data.png)

## Viewing Results

After loading, your data will be displayed in the main plot area.

![Plot view](help_images/plot_view.png)

## Tips

- Use the mouse wheel to zoom
- Click and drag to pan
- Double-click to reset the view
`
	ShowMarkdownDialogWithImages("Help", helpText, &helpImages, w)
}
*/
