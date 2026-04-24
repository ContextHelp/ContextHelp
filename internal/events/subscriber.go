package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// deptPools maps department to knowledge pool tags.
var deptPools = map[string]deptMapping{
	"ops":       {pools: []string{"company", "revenue-ops", "incidents", "compliance"}, rerank: map[string]float64{"pool:incidents": 0.15, "pool:revenue-ops": 0.10, "dept:ops": 0.10}},
	"eng":       {pools: []string{"company", "product-eng", "incidents", "compliance"}, rerank: map[string]float64{"pool:product-eng": 0.15, "pool:incidents": 0.10, "dept:eng": 0.10}},
	"product":   {pools: []string{"company", "product-eng", "go-to-market"}, rerank: map[string]float64{"pool:product-eng": 0.15, "pool:go-to-market": 0.10, "dept:product": 0.10}},
	"marketing": {pools: []string{"company", "go-to-market"}, rerank: map[string]float64{"pool:go-to-market": 0.15, "dept:marketing": 0.10}},
	"sales":     {pools: []string{"company", "go-to-market", "revenue-ops"}, rerank: map[string]float64{"pool:go-to-market": 0.10, "pool:revenue-ops": 0.15, "dept:sales": 0.10}},
	"finance":   {pools: []string{"company", "revenue-ops"}, rerank: map[string]float64{"pool:revenue-ops": 0.15, "dept:finance": 0.10}},
	"legal":     {pools: []string{"company", "compliance"}, rerank: map[string]float64{"pool:compliance": 0.15, "dept:legal": 0.10}},
	"devrel":    {pools: []string{"company", "product-eng", "go-to-market"}, rerank: map[string]float64{"pool:product-eng": 0.15, "pool:go-to-market": 0.10, "dept:devrel": 0.10}},
	"executive": {pools: []string{"company", "product-eng", "go-to-market", "revenue-ops", "incidents", "compliance"}, rerank: map[string]float64{"pool:company": 0.10}},
}

type deptMapping struct {
	pools  []string
	rerank map[string]float64
}

// ProfilePayload is the expected payload for aps.profile.* events.
type ProfilePayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Department  string `json:"department"`
	Description string `json:"description"`
}

// Subscriber reacts to inbound bus events and provisions config.
type Subscriber struct {
	cfg     *config.Config
	cfgPath string
	mu      sync.Mutex
}

// NewSubscriber creates a subscriber that mutates cfg and persists to cfgPath.
func NewSubscriber(cfg *config.Config, cfgPath string) *Subscriber {
	return &Subscriber{cfg: cfg, cfgPath: cfgPath}
}

// Register wires all aps.profile.* handlers onto the bus.
func (s *Subscriber) Register(b Bus) {
	b.Subscribe("aps.profile.created", s.onProfileCreated)
	b.Subscribe("aps.profile.updated", s.onProfileUpdated)
	b.Subscribe("aps.profile.deleted", s.onProfileDeleted)
}

func (s *Subscriber) onProfileCreated(_ context.Context, e Event) error {
	p, err := decodeProfilePayload(e)
	if err != nil {
		log.Printf("[events] profile.created: bad payload: %v", err)
		return nil
	}

	mapping, ok := deptPools[p.Department]
	if !ok {
		log.Printf("[events] profile.created: unknown dept %q for %s; skipping", p.Department, p.ID)
		return nil
	}

	fp := buildFocusProfile(p, mapping)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cfg.Profile.Profiles == nil {
		s.cfg.Profile.Profiles = make(map[string]config.FocusProfile)
	}
	if _, exists := s.cfg.Profile.Profiles[p.ID]; exists {
		log.Printf("[events] profile.created: %s already exists; skipping", p.ID)
		return nil
	}

	s.cfg.Profile.Profiles[p.ID] = fp
	if err := config.WriteBack(s.cfg, s.cfgPath); err != nil {
		log.Printf("[events] profile.created: write-back failed: %v", err)
		return nil
	}
	log.Printf("[events] profile.created: provisioned %s (dept=%s, pools=%d)",
		p.ID, p.Department, len(mapping.pools))
	return nil
}

func (s *Subscriber) onProfileUpdated(_ context.Context, e Event) error {
	p, err := decodeProfilePayload(e)
	if err != nil {
		log.Printf("[events] profile.updated: bad payload: %v", err)
		return nil
	}

	mapping, ok := deptPools[p.Department]
	if !ok {
		log.Printf("[events] profile.updated: unknown dept %q for %s; skipping", p.Department, p.ID)
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cfg.Profile.Profiles[p.ID]; !exists {
		log.Printf("[events] profile.updated: %s not found; skipping", p.ID)
		return nil
	}

	fp := buildFocusProfile(p, mapping)
	s.cfg.Profile.Profiles[p.ID] = fp
	if err := config.WriteBack(s.cfg, s.cfgPath); err != nil {
		log.Printf("[events] profile.updated: write-back failed: %v", err)
		return nil
	}
	log.Printf("[events] profile.updated: refreshed %s (dept=%s)", p.ID, p.Department)
	return nil
}

func (s *Subscriber) onProfileDeleted(_ context.Context, e Event) error {
	p, err := decodeProfilePayload(e)
	if err != nil {
		log.Printf("[events] profile.deleted: bad payload: %v", err)
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cfg.Profile.Profiles[p.ID]; !exists {
		return nil
	}

	delete(s.cfg.Profile.Profiles, p.ID)
	if err := config.WriteBack(s.cfg, s.cfgPath); err != nil {
		log.Printf("[events] profile.deleted: write-back failed: %v", err)
		return nil
	}
	log.Printf("[events] profile.deleted: removed %s", p.ID)
	return nil
}

func buildFocusProfile(p ProfilePayload, m deptMapping) config.FocusProfile {
	tags := make([]string, 0, 2+len(m.pools))
	tags = append(tags, "agent:"+p.ID)
	tags = append(tags, "dept:"+p.Department)
	for _, pool := range m.pools {
		tags = append(tags, "pool:"+pool)
	}

	desc := p.Description
	if desc == "" {
		desc = fmt.Sprintf("%s — %s", p.Name, p.Department)
	}

	boosts := make(map[string]float64, len(m.rerank))
	for k, v := range m.rerank {
		boosts[k] = v
	}

	return config.FocusProfile{
		Description:  desc,
		Tags:         tags,
		RerankBoosts: boosts,
	}
}

func decodeProfilePayload(e Event) (ProfilePayload, error) {
	var p ProfilePayload
	if err := json.Unmarshal(e.Data, &p); err != nil {
		return p, fmt.Errorf("unmarshal: %w", err)
	}
	if p.ID == "" {
		return p, fmt.Errorf("missing id")
	}
	return p, nil
}
