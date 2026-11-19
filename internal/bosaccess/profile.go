package bosaccess

// Profile is the part of a peer profile the access contract reads: the
// contracts the peer lists with their features, the grant shapes it
// represents and the staleness limit it enforces.
type Profile struct {
	Tool        string   `json:"tool"`
	ToolVersion string   `json:"tool_version"`
	Contracts   []Listed `json:"contracts"`
	Represents  []string `json:"represents"`
	Limits      Limits   `json:"limits"`
	Auth        []string `json:"auth,omitempty"`
}

// Listed is one entry of a profile's contracts list.
type Listed struct {
	ID       string   `json:"id"`
	Level    string   `json:"level"`
	Features []string `json:"features,omitempty"`
}

// Limits are the numbers a peer declares.
type Limits struct {
	MaxStalenessSeconds int64 `json:"max_staleness_seconds"`
}

// Expresses reports whether the peer lists a grant shape.
func (p Profile) Expresses(shape string) bool {
	for _, s := range p.Represents {
		if s == shape {
			return true
		}
	}
	return false
}

// HasFeature reports whether the peer lists the feature for the contract at
// a level other than unsupported.
func (p Profile) HasFeature(contract, feature string) bool {
	for _, c := range p.Contracts {
		if c.ID != contract || c.Level == "unsupported" {
			continue
		}
		for _, f := range c.Features {
			if f == feature {
				return true
			}
		}
	}
	return false
}

// Supports reports whether the peer lists what a scenario requires: a grant
// shape, or the feature record_access.
func (p Profile) Supports(req string) bool {
	if req == FeatureRecordAccess {
		return p.HasFeature(Contract, FeatureRecordAccess)
	}
	return p.Expresses(req)
}

// DefaultMaxStalenessSeconds is the staleness limit ctxt declares: a set it
// has not seen confirmed for a day stops allowing.
const DefaultMaxStalenessSeconds = 86400

// DeclaredProfile is what ctxt declares to the authority: bos.access/v1 with
// the feature record_access, and the shapes the record entries need
// (row_relation for an assignment, record_grant for a single record). It
// lists no type grant shape: ctxt reads through its own share policies, so a
// type grant the authority holds is one ctxt does not enforce and the
// authority lists as not representable. Entry dates (valid_from,
// valid_until) are part of the feature.
func DeclaredProfile(version string) Profile {
	return Profile{
		Tool:        "ctxt",
		ToolVersion: version,
		Contracts:   []Listed{{ID: Contract, Level: "partial", Features: []string{FeatureRecordAccess}}},
		Represents:  []string{ShapeRowRelation, ShapeRecordGrant},
		Limits:      Limits{MaxStalenessSeconds: DefaultMaxStalenessSeconds},
	}
}
