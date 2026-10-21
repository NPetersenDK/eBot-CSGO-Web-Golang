package auth

import "strings"

const roleDeny = "deny"

type roleMapping struct {
	Value string
	Role  string
}

// roleMap maps OIDC claim values to roles.
type roleMap struct {
	Claim   string
	Map     []roleMapping
	Default string
}

func newRoleMap(claim, raw, def string) roleMap {
	return roleMap{Claim: claim, Map: parseRoleMap(raw), Default: strings.TrimSpace(def)}
}

// resolve returns admin if any admin mapping matches, else the first match in
// order, else the default. Anything that is not admin or viewer means deny.
func (m roleMap) resolve(values []string) string {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	for _, e := range m.Map {
		if e.Role != RoleAdmin {
			continue
		}
		if _, ok := set[e.Value]; ok {
			return RoleAdmin
		}
	}
	for _, e := range m.Map {
		if _, ok := set[e.Value]; ok {
			return e.Role
		}
	}
	return validRole(m.Default)
}

func validRole(r string) string {
	if r == RoleAdmin || r == RoleViewer {
		return r
	}
	return roleDeny
}

// parseRoleMap parses "value=role,value=role". Malformed pairs and unknown
// roles are skipped.
func parseRoleMap(s string) []roleMapping {
	var out []roleMapping
	for _, pair := range strings.Split(s, ",") {
		value, role, ok := strings.Cut(pair, "=")
		value, role = strings.TrimSpace(value), strings.TrimSpace(role)
		if !ok || value == "" || (role != RoleAdmin && role != RoleViewer) {
			continue
		}
		out = append(out, roleMapping{Value: value, Role: role})
	}
	return out
}

func stringClaim(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func stringSliceClaim(m map[string]any, key string) []string {
	switch raw := m[key].(type) {
	case []any:
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{raw}
	default:
		return nil
	}
}
