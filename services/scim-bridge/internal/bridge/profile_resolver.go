package bridge

import "strings"

// ProfileResolver maps Keycloak masProfile labels to MAS SCIM profile IDs.
type ProfileResolver struct {
	defaultProfileID string
	requireLabel     bool
	mapping          map[string]string
}

// ProfileResolution captures the result of resolving a masProfile label.
type ProfileResolution struct {
	Label     string
	ProfileID string
	FromLabel bool
}

// NewProfileResolver constructs a resolver with normalized keys.
func NewProfileResolver(defaultProfileID string, mapping map[string]string, requireLabel bool) ProfileResolver {
	normalized := make(map[string]string, len(mapping))
	for k, v := range mapping {
		key := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		if key == "" || val == "" {
			continue
		}
		normalized[key] = val
	}
	return ProfileResolver{
		defaultProfileID: strings.TrimSpace(defaultProfileID),
		requireLabel:     requireLabel,
		mapping:          normalized,
	}
}

// Resolve returns the MAS profile for a given masProfile label.
func (r ProfileResolver) Resolve(label string) (ProfileResolution, bool) {
	normalized := strings.ToLower(strings.TrimSpace(label))
	if normalized != "" {
		if id, ok := r.mapping[normalized]; ok {
			return ProfileResolution{Label: normalized, ProfileID: id, FromLabel: true}, true
		}
		if r.requireLabel {
			return ProfileResolution{Label: normalized}, false
		}
	} else if r.requireLabel {
		return ProfileResolution{}, false
	}
	return ProfileResolution{Label: normalized, ProfileID: r.defaultProfileID, FromLabel: false}, true
}

func (r ProfileResolver) DefaultProfileID() string {
	return r.defaultProfileID
}
