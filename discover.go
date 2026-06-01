package main

import (
	"sort"
	"sync"
)

type discoverFunc func() ([]Session, []error)

type discoverSpec struct {
	tool      string
	fn        discoverFunc
	available func() bool
}

type DiscoveryResult struct {
	Sessions      []Session
	Errors        []error
	CompleteTools []string
}

// DiscoverAll runs every per-tool discovery in parallel and merges the results.
// Returned sessions are sorted by LastActive descending (newest first).
// Errors are accumulated and returned alongside the partial results.
func DiscoverAll() ([]Session, []error) {
	r := DiscoverAllDetailed()
	return r.Sessions, r.Errors
}

func DiscoverAllDetailed() DiscoveryResult {
	return discoverWith([]discoverSpec{
		{tool: "claude", fn: discoverClaude},
		{tool: "codex", fn: discoverCodex},
		{tool: "amp", fn: discoverAmp, available: ampDiscoverable},
		{tool: "pi", fn: discoverPi},
	})
}

// DiscoverFileBased runs only the tools whose sessions live on the local
// filesystem (Claude, Codex, Pi) — i.e. cheap enough to call from a periodic
// background tick. Amp is excluded because it requires a network call.
func DiscoverFileBased() ([]Session, []error) {
	r := DiscoverFileBasedDetailed()
	return r.Sessions, r.Errors
}

func DiscoverFileBasedDetailed() DiscoveryResult {
	return discoverWith([]discoverSpec{
		{tool: "claude", fn: discoverClaude},
		{tool: "codex", fn: discoverCodex},
		{tool: "pi", fn: discoverPi},
	})
}

func discoverWith(specs []discoverSpec) DiscoveryResult {
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		sessions []Session
		errs     []error
		complete = make([]string, 0, len(specs))
	)
	for _, spec := range specs {
		wg.Add(1)
		go func(s discoverSpec) {
			defer wg.Done()
			if s.available != nil && !s.available() {
				return
			}
			ss, es := s.fn()
			mu.Lock()
			sessions = append(sessions, ss...)
			errs = append(errs, es...)
			if len(es) == 0 {
				complete = append(complete, s.tool)
			}
			mu.Unlock()
		}(spec)
	}
	wg.Wait()
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActive.After(sessions[j].LastActive)
	})
	sort.Strings(complete)
	return DiscoveryResult{
		Sessions:      sessions,
		Errors:        errs,
		CompleteTools: complete,
	}
}
