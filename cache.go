package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

// CacheStats summarizes the disk footprint of seshr's own storage —
// the SQLite database (plus its WAL/SHM sidecars) and the Amp content cache.
type CacheStats struct {
	DBBytes       int64
	AmpCacheBytes int64
	AmpCacheFiles int
	TotalBytes    int64
}

func seshrCacheStats() CacheStats {
	var s CacheStats
	dbBase := filepath.Join(dbDir(), "sessions.db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(dbBase + suffix); err == nil {
			s.DBBytes += info.Size()
		}
	}
	if entries, err := os.ReadDir(ampCacheDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if info, err := e.Info(); err == nil {
				s.AmpCacheBytes += info.Size()
				s.AmpCacheFiles++
			}
		}
	}
	s.TotalBytes = s.DBBytes + s.AmpCacheBytes
	return s
}

// AgentStorageStat is per-tool on-disk usage of the tool's *own* session
// store (not our cache). Surfaces in the info modal so the user can see
// where space is actually going.
type AgentStorageStat struct {
	Tool      string
	Path      string
	Bytes     int64
	FileCount int
}

func agentStorageStats() []AgentStorageStat {
	return []AgentStorageStat{
		statForDir("claude", claudeProjectsDir()),
		statForDir("codex", codexSessionsDir()),
		statForDir("amp", ampThreadsDir()),
		statForDir("pi", piSessionsDir()),
	}
}

func statForDir(tool, root string) AgentStorageStat {
	s := AgentStorageStat{Tool: tool, Path: root}
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			s.Bytes += info.Size()
			s.FileCount++
		}
		return nil
	})
	return s
}
