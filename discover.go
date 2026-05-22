package main

import (
	"sort"
	"sync"
)

type discoverFunc func() ([]Session, []error)

// DiscoverAll runs every per-tool discovery in parallel and merges the results.
// Returned sessions are sorted by LastActive descending (newest first).
// Errors are accumulated and returned alongside the partial results.
func DiscoverAll() ([]Session, []error) {
	return discoverWith([]discoverFunc{
		discoverClaude,
		discoverCodex,
		discoverAmp,
		discoverPi,
	})
}

// DiscoverFileBased runs only the tools whose sessions live on the local
// filesystem (Claude, Codex, Pi) — i.e. cheap enough to call from a periodic
// background tick. Amp is excluded because it requires a network call.
//
// FileBasedTools should match the set of tools dispatched here; it's used by
// callers that need to scope DB sync to "just what this function covered."
func DiscoverFileBased() ([]Session, []error) {
	return discoverWith([]discoverFunc{
		discoverClaude,
		discoverCodex,
		discoverPi,
	})
}

// FileBasedTools is the tool-name set covered by DiscoverFileBased. Pass to
// SyncSessionsScoped so background ticks don't mark Amp rows missing.
var FileBasedTools = []string{"claude", "codex", "pi"}

func discoverWith(funcs []discoverFunc) ([]Session, []error) {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		sessions []Session
		errs     []error
	)
	for _, fn := range funcs {
		wg.Add(1)
		go func(f discoverFunc) {
			defer wg.Done()
			ss, es := f()
			mu.Lock()
			sessions = append(sessions, ss...)
			errs = append(errs, es...)
			mu.Unlock()
		}(fn)
	}
	wg.Wait()
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActive.After(sessions[j].LastActive)
	})
	return sessions, errs
}
