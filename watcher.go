package httpwatch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type WatcherConfig struct {
	Recursive   bool
	Dir         string
	FilePattern string
}

func NewWatcherFn(ctx context.Context, cfg WatcherConfig, b *Broadcaster) (func(), error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create watcher", "error", err)
		os.Exit(1)
	}

	addDir := func(path string) error {
		if cfg.Recursive {
			return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.IsDir() {
					if err := watcher.Add(walkPath); err != nil {
						return err
					}
				}
				return nil
			})
		} else {
			if err := watcher.Add(path); err != nil {
				return err
			}
		}
		return nil
	}

	// Add the initial directory to the watcher
	if err := addDir(cfg.Dir); err != nil {
		return nil, fmt.Errorf("failed adding directory to watcher: %w", err)
	}

	// Create a map to track recently processed events to avoid duplicates
	// This is useful because some file operations can trigger multiple events
	recentEvents := make(map[string]time.Time)
	const eventTimeout = 100 * time.Millisecond

	// Compile the regex pattern
	regex := regexp.MustCompile(cfg.FilePattern)

	fullPath := cfg.Dir
	if fullPath != "" {
		var err error
		fullPath, err = filepath.Abs(cfg.Dir)
		if err != nil {
			return nil, fmt.Errorf("failed getting path: %w", err)
		}
	}

	return func() {
		defer watcher.Close()

		slog.InfoContext(ctx, "started watching for files", "pattern", cfg.FilePattern)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// Get the absolute file path for consistent handling
				filePath, err := filepath.Abs(event.Name)
				if err != nil {
					slog.ErrorContext(ctx, "Error getting absolute path", "error", err)
					continue
				}

				// Check if this is a recent duplicate event
				lastEvent, exists := recentEvents[filePath+string(rune(event.Op))]
				if exists && time.Since(lastEvent) < eventTimeout {
					continue
				}
				recentEvents[filePath+string(rune(event.Op))] = time.Now()

				// Get file info to check if it's a directory
				fileInfo, err := os.Stat(filePath)
				isDir := err == nil && fileInfo.IsDir()

				// If a new directory is created and we're in recursive mode, watch it
				if isDir && cfg.Recursive && (event.Op&fsnotify.Create == fsnotify.Create) {
					if err := addDir(filePath); err != nil {
						slog.ErrorContext(ctx, "Error adding new directory to watcher", "error", err)
					}
					continue
				}

				// Skip directory events for filtering
				if isDir {
					continue
				}

				// Check if the file matches our pattern
				filename := filepath.Base(filePath)
				if !regex.MatchString(filename) {
					continue
				}

				path := strings.ReplaceAll(filePath, fullPath, "")
				handleEvent(ctx, event, path, b)

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.ErrorContext(ctx, "watcher error", "error", err)
			}
		}
	}, nil
}

func handleEvent(ctx context.Context, event fsnotify.Event, filePath string, b *Broadcaster) {
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
	case event.Op&fsnotify.Write == fsnotify.Write:
	case event.Op&fsnotify.Remove == fsnotify.Remove:
	case event.Op&fsnotify.Rename == fsnotify.Rename:
	case event.Op&fsnotify.Chmod == fsnotify.Chmod:
		// eventType = "permission changed"
		return
	}

	slog.DebugContext(ctx, "file changed", "file", filePath)
	b.Broadcast(filePath)
}
