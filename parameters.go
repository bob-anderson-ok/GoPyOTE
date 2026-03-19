package main

import (
	"fmt"
	"io"
	"os"

	"github.com/KevinWang15/go-json5"
)

// parseOccultationParameters parses a JSON5 parameters file
func parseOccultationParameters(reader io.Reader) (*OccultationParameters, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var params OccultationParameters
	if err := json5.Unmarshal(content, &params); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	return &params, nil
}

// logOccparamsRead logs an .occparams file read with key parameter values.
func logOccparamsRead(context, path string) {
	info, serr := os.Stat(path)
	if serr != nil {
		logAction(fmt.Sprintf("OCCPARAMS READ [%s]: %s — FILE NOT FOUND", context, path))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		logAction(fmt.Sprintf("OCCPARAMS READ [%s]: %s — OPEN FAILED: %v", context, path, err))
		return
	}
	p, perr := parseOccultationParameters(f)
	_ = f.Close()
	if perr != nil {
		logAction(fmt.Sprintf("OCCPARAMS READ [%s]: %s — PARSE FAILED: %v", context, path, perr))
		return
	}
	logAction(fmt.Sprintf("OCCPARAMS READ [%s]: %s (modified %s, %d bytes) major=%.3f minor=%.3f",
		context, path, info.ModTime().Format("15:04:05"), info.Size(),
		p.MainBody.MajorAxisKm, p.MainBody.MinorAxisKm))
}

// logOccparamsWrite logs an .occparams file with key parameter values.
func logOccparamsWrite(context, path string) {
	info, serr := os.Stat(path)
	if serr != nil {
		logAction(fmt.Sprintf("OCCPARAMS WRITE [%s]: %s — STAT FAILED: %v", context, path, serr))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		logAction(fmt.Sprintf("OCCPARAMS WRITE [%s]: %s — OPEN FAILED: %v", context, path, err))
		return
	}
	p, perr := parseOccultationParameters(f)
	_ = f.Close()
	if perr != nil {
		logAction(fmt.Sprintf("OCCPARAMS WRITE [%s]: %s — PARSE FAILED: %v", context, path, perr))
		return
	}
	logAction(fmt.Sprintf("OCCPARAMS WRITE [%s]: %s (modified %s, %d bytes) major=%.3f minor=%.3f",
		context, path, info.ModTime().Format("15:04:05"), info.Size(),
		p.MainBody.MajorAxisKm, p.MainBody.MinorAxisKm))
}
