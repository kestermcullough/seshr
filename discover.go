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
	funcs := []discoverFunc{
		discoverClaude,
		discoverCodex,
		discoverAmp,
		discoverPi,
	}

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
