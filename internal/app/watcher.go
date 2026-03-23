package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
)

type FileWatcher struct {
	ingest  *IngestService
	logger  *slog.Logger
	watcher *fsnotify.Watcher

	mu       sync.Mutex
	projects map[string]*uuid.UUID // rootPath -> projectID
	pending  map[string]time.Time  // debounce: path -> last event
}

func NewFileWatcher(ingest *IngestService, logger *slog.Logger) *FileWatcher {
	return &FileWatcher{
		ingest:   ingest,
		logger:   logger,
		projects: make(map[string]*uuid.UUID),
		pending:  make(map[string]time.Time),
	}
}

func (fw *FileWatcher) WatchProject(rootPath string, projectID *uuid.UUID) error {
	fw.mu.Lock()
	fw.projects[rootPath] = projectID
	fw.mu.Unlock()

	if fw.watcher == nil {
		return nil
	}

	return fw.addDirs(rootPath)
}

func (fw *FileWatcher) Start(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	fw.watcher = w

	fw.mu.Lock()
	for root := range fw.projects {
		fw.addDirs(root)
	}
	fw.mu.Unlock()

	go fw.loop(ctx)
	go fw.debounceLoop(ctx)

	fw.logger.Info("file watcher started", "projects", len(fw.projects))
	return nil
}

func (fw *FileWatcher) addDirs(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == "node_modules" || base == ".git" || base == "web_dist" || base == "bin" {
				return filepath.SkipDir
			}
			return fw.watcher.Add(path)
		}
		return nil
	})
}

func (fw *FileWatcher) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			fw.watcher.Close()
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			if !strings.HasSuffix(event.Name, ".go") {
				continue
			}
			if strings.HasSuffix(event.Name, "_test.go") {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			fw.mu.Lock()
			fw.pending[event.Name] = time.Now()
			fw.mu.Unlock()

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fw.logger.Warn("watcher error", "error", err)
		}
	}
}

func (fw *FileWatcher) debounceLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.processPending(ctx)
		}
	}
}

func (fw *FileWatcher) processPending(ctx context.Context) {
	fw.mu.Lock()
	if len(fw.pending) == 0 {
		fw.mu.Unlock()
		return
	}

	threshold := time.Now().Add(-5 * time.Second)
	rootsToIngest := make(map[string]*uuid.UUID)

	for path, eventTime := range fw.pending {
		if eventTime.Before(threshold) {
			root := fw.findProjectRoot(path)
			if root != "" {
				rootsToIngest[root] = fw.projects[root]
			}
			delete(fw.pending, path)
		}
	}
	fw.mu.Unlock()

	for root, projectID := range rootsToIngest {
		fw.logger.Info("auto-ingesting changed project", "root", root)
		result, err := fw.ingest.IngestGoProject(ctx, root, projectID)
		if err != nil {
			fw.logger.Warn("auto-ingest failed", "root", root, "error", err)
		} else {
			fw.logger.Info("auto-ingest completed",
				"root", root,
				"files", result.FilesScanned,
				"created", result.MemoriesCreated,
			)
		}
	}
}

func (fw *FileWatcher) findProjectRoot(path string) string {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	absPath, _ := filepath.Abs(path)
	for root := range fw.projects {
		absRoot, _ := filepath.Abs(root)
		if strings.HasPrefix(absPath, absRoot) {
			return root
		}
	}
	return ""
}

func (fw *FileWatcher) Stop() {
	if fw.watcher != nil {
		fw.watcher.Close()
	}
}
