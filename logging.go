package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Global variables for action logging
var (
	actionLogFile   *os.File
	actionLogWriter *bufio.Writer
)

// createActionLog creates a new log file based on the CSV file path
func createActionLog(csvFilePath string) error {
	// Close any existing log file
	closeActionLog()

	// Build log file path: same name as CSV but with .log extension
	dir := filepath.Dir(csvFilePath)
	base := filepath.Base(csvFilePath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	logPath := filepath.Join(dir, nameWithoutExt+".log")

	// Create/open the log file (append mode)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	actionLogFile = file
	actionLogWriter = bufio.NewWriter(file)

	// Write session start marker
	logAction("=== New Session Started ===")
	logAction("CSV file: " + csvFilePath)

	return nil
}

// logAction writes a timestamped action to the log file
func logAction(action string) {
	if actionLogWriter == nil {
		return
	}

	timestamp := time.Now().Format("2006-Jan-02 15:04")
	line := fmt.Sprintf("[%s] %s\n", timestamp, action)

	if _, err := actionLogWriter.WriteString(line); err != nil {
		fmt.Printf("Warning: failed to write to log: %v\n", err)
		return
	}
	if err := actionLogWriter.Flush(); err != nil {
		fmt.Printf("Warning: failed to flush log: %v\n", err)
	}
}

// closeActionLog closes the current log file
func closeActionLog() {
	if actionLogWriter != nil {
		logAction("=== Session Ended ===")
		if err := actionLogWriter.Flush(); err != nil {
			fmt.Printf("Warning: failed to flush log on close: %v\n", err)
		}
		actionLogWriter = nil
	}
	if actionLogFile != nil {
		if err := actionLogFile.Close(); err != nil {
			fmt.Printf("Warning: failed to close log file: %v\n", err)
		}
		actionLogFile = nil
	}
}
