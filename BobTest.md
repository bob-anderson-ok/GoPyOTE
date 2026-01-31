# Bob's Test Document

This is a **test** of the embedded Markdown dialog system.

## Features Demonstrated

- Markdown formatting with **bold** and *italic* text
- Bullet point lists
- Headers at different levels

## How It Works

1. The Markdown file is embedded using Go's `embed` package
2. Content is parsed to separate text from image references
3. Text is rendered using Fyne's `RichTextFromMarkdown`
4. Images (if any) are loaded and displayed inline

### Code Example

You can include code references like `functionName()` inline.

## Next Steps

To add an image, use the syntax:

```
![Alt text](path/to/image.png)
```
![Camera Response](help_images/diffractionImage8bit.png =800x800)
---

*This test file confirms the embed system is working correctly.*
