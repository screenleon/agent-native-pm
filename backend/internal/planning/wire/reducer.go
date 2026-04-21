package wire

import (
	"encoding/json"
)

// sourceKey identifies a named source inside PlanningContextSources for the
// byte-cap reducer.
type sourceKey int

const (
	sourceOpenTasks sourceKey = iota
	sourceRecentDocuments
	sourceOpenDriftSignals
	sourceLatestSyncRun
	sourceRecentAgentRuns
)

var sourceDroppedCountKeys = map[sourceKey]string{
	sourceOpenTasks:        "open_tasks",
	sourceRecentDocuments:  "recent_documents",
	sourceOpenDriftSignals: "open_drift_signals",
	sourceLatestSyncRun:    "latest_sync_run",
	sourceRecentAgentRuns:  "recent_agent_runs",
}

// sourceByteLen returns the JSON-marshaled byte length of a single named
// source field within sources. Errors are treated as zero length.
func sourceByteLen(sources PlanningContextSources, key sourceKey) int {
	var value interface{}
	switch key {
	case sourceOpenTasks:
		value = sources.OpenTasks
	case sourceRecentDocuments:
		value = sources.RecentDocuments
	case sourceOpenDriftSignals:
		value = sources.OpenDriftSignals
	case sourceLatestSyncRun:
		value = sources.LatestSyncRun
	case sourceRecentAgentRuns:
		value = sources.RecentAgentRuns
	default:
		return 0
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(payload)
}

// sourcesByteLen returns the JSON-marshaled byte length of the sources block.
func sourcesByteLen(sources PlanningContextSources) int {
	payload, err := json.Marshal(sources)
	if err != nil {
		return 0
	}
	return len(payload)
}

// dropLastFromSource removes the lowest-ranked (last) item from the largest
// source. Returns true if a drop occurred. Ranking order is assumed to be
// caller-established — reducer operates on slice tail because callers have
// pre-sorted by rank descending.
func dropLastFromSource(sources *PlanningContextSources, key sourceKey) bool {
	switch key {
	case sourceOpenTasks:
		if n := len(sources.OpenTasks); n > 0 {
			sources.OpenTasks = sources.OpenTasks[:n-1]
			return true
		}
	case sourceRecentDocuments:
		if n := len(sources.RecentDocuments); n > 0 {
			sources.RecentDocuments = sources.RecentDocuments[:n-1]
			return true
		}
	case sourceOpenDriftSignals:
		if n := len(sources.OpenDriftSignals); n > 0 {
			sources.OpenDriftSignals = sources.OpenDriftSignals[:n-1]
			return true
		}
	case sourceLatestSyncRun:
		if sources.LatestSyncRun != nil {
			sources.LatestSyncRun = nil
			return true
		}
	case sourceRecentAgentRuns:
		if n := len(sources.RecentAgentRuns); n > 0 {
			sources.RecentAgentRuns = sources.RecentAgentRuns[:n-1]
			return true
		}
	}
	return false
}

// largestSource identifies the source with the largest marshaled JSON byte
// length. Returns the source key and the observed byte length.
func largestSource(sources PlanningContextSources) (sourceKey, int) {
	var bestKey sourceKey = sourceOpenTasks
	bestLen := -1
	for _, key := range []sourceKey{
		sourceOpenTasks,
		sourceRecentDocuments,
		sourceOpenDriftSignals,
		sourceLatestSyncRun,
		sourceRecentAgentRuns,
	} {
		if n := sourceByteLen(sources, key); n > bestLen {
			bestLen = n
			bestKey = key
		}
	}
	if bestLen < 0 {
		bestLen = 0
	}
	return bestKey, bestLen
}

// ReduceSources shrinks sources until its JSON-marshaled byte length is at
// most maxBytes. Items are dropped one at a time from whichever source
// currently has the largest marshaled byte length (re-measured each round).
//
// The cap applies only to sources — scaffolding overhead in the surrounding
// PlanningContextV1 struct is not counted.
//
// Returns the reduced sources, a map of per-source drop counts (every named
// source is represented with a zero value when nothing was dropped), and the
// final sources byte length.
func ReduceSources(sources PlanningContextSources, maxBytes int) (PlanningContextSources, map[string]int, int) {
	dropped := map[string]int{
		"open_tasks":         0,
		"recent_documents":   0,
		"open_drift_signals": 0,
		"latest_sync_run":    0,
		"recent_agent_runs":  0,
	}

	if maxBytes <= 0 {
		return sources, dropped, sourcesByteLen(sources)
	}

	current := sourcesByteLen(sources)
	if current <= maxBytes {
		return sources, dropped, current
	}

	// Iteratively drop from the largest-in-bytes source until under cap or
	// all sources are empty.
	for current > maxBytes {
		key, _ := largestSource(sources)
		if !dropLastFromSource(&sources, key) {
			// Nothing left to drop.
			break
		}
		dropped[sourceDroppedCountKeys[key]]++
		current = sourcesByteLen(sources)
	}

	return sources, dropped, current
}
