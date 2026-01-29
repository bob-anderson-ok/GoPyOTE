package main

import (
	"fmt"
	"io"

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
