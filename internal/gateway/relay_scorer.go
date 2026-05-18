package gateway

import (
	"math"
	"sort"
	"sync"

	"tunneledge/internal/domain"
)

const defaultEWMAAlpha = 0.3

// RelayScorer maintains EWMA-smoothed composite health scores for known relay
// gateways. Scores range from 0.0 (worst) to 1.0 (best). Weights:
//
//   - RTT              40 %  (lower RTT → higher score)
//   - Packet loss      40 %  (lower loss → higher score)
//   - Utilisation      20 %  (lower util → higher score)
type RelayScorer struct {
	mu      sync.RWMutex
	scores  map[string]float64 // relayID → composite score
	regions map[string]string  // relayID → region tag
	addrs   map[string]string  // relayID → advertise addr
	alpha   float64            // EWMA smoothing factor
}

// NewRelayScorer returns a scorer with the default EWMA alpha of 0.3.
func NewRelayScorer() *RelayScorer {
	return &RelayScorer{
		scores:  make(map[string]float64),
		regions: make(map[string]string),
		addrs:   make(map[string]string),
		alpha:   defaultEWMAAlpha,
	}
}

// Update ingests a RelayHealth report and recomputes the EWMA score for that
// relay. It also records the relay's region and advertise address so they can
// be queried later without locking the distributed router.
func (rs *RelayScorer) Update(relayID string, h domain.RelayHealth) {
	newScore := computeScore(h)

	rs.mu.Lock()
	defer rs.mu.Unlock()

	if prev, ok := rs.scores[relayID]; ok {
		rs.scores[relayID] = rs.alpha*newScore + (1-rs.alpha)*prev
	} else {
		rs.scores[relayID] = newScore
	}

	if h.Region != "" {
		rs.regions[relayID] = h.Region
	}
	if h.AdvertiseAddr != "" {
		rs.addrs[relayID] = h.AdvertiseAddr
	}
}

// Score returns the current composite score for relayID, or 0 if unknown.
func (rs *RelayScorer) Score(relayID string) float64 {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.scores[relayID]
}

// BestRelay returns the advertise address of the relay with the highest score.
// Returns empty string when no relays are known.
func (rs *RelayScorer) BestRelay() string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	best := ""
	bestScore := -1.0
	for relayID, score := range rs.scores {
		if score > bestScore {
			best = relayID
			bestScore = score
		}
	}
	if addr := rs.addrs[best]; addr != "" {
		return addr
	}
	return ""
}

// BestRelaysForRegion returns up to n advertise addresses of the top-scoring
// relays in the given region. When region is empty, all relays are considered.
func (rs *RelayScorer) BestRelaysForRegion(region string, n int) []string {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	type candidate struct {
		addr  string
		score float64
	}
	var candidates []candidate
	for relayID, score := range rs.scores {
		if region != "" && rs.regions[relayID] != region {
			continue
		}
		if addr := rs.addrs[relayID]; addr != "" {
			candidates = append(candidates, candidate{addr: addr, score: score})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	result := make([]string, 0, n)
	for i := 0; i < len(candidates) && i < n; i++ {
		result = append(result, candidates[i].addr)
	}
	return result
}

// computeScore converts a RelayHealth snapshot into a [0,1] composite score.
func computeScore(h domain.RelayHealth) float64 {
	// RTT score: asymptotic, 0 ms → 1.0, 500 ms → ~0.5, ∞ → 0.
	rttScore := 1.0 / (1.0 + float64(h.RTTMillis)/500.0)

	// Packet loss score: 0% → 1.0, 100% → 0.0.
	lossScore := math.Max(0, 1.0-h.PacketLossPct/100.0)

	// Utilisation score: average of CPU and memory, 0% → 1.0, 100% → 0.0.
	utilScore := math.Max(0, 1.0-(h.CPUUtilizationPct+h.MemoryUtilizationPct)/200.0)

	return 0.4*rttScore + 0.4*lossScore + 0.2*utilScore
}
