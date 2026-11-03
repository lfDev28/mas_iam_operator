package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/config"
	"github.com/lfDev28/mas-iam/services/scim-bridge/internal/mas"
)

// ConfigFingerprint hashes the bridge settings that can turn a failing MAS
// call into a succeeding one (target, profiles, auth, TLS). It is folded into
// every desired-state hash, so fixing the config and restarting re-attempts
// parked entries. Secrets are deliberately left out: a minted JWT changes
// on every start and would unpark everything on every restart.
func ConfigFingerprint(cfg config.Settings) string {
	keys := make([]string, 0, len(cfg.MAS.ProfileMap))
	for k := range cfg.MAS.ProfileMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "mas.base=%s\n", strings.TrimRight(cfg.MAS.BaseURL, "/"))
	fmt.Fprintf(&b, "mas.profile=%s\n", cfg.MAS.ProfileID)
	for _, k := range keys {
		fmt.Fprintf(&b, "mas.profile_map.%s=%s\n", k, cfg.MAS.ProfileMap[k])
	}
	fmt.Fprintf(&b, "mas.require_label=%t\n", cfg.MAS.ProfileRequireLabel)
	fmt.Fprintf(&b, "mas.auth_type=%s\n", strings.ToLower(cfg.MAS.AuthType))
	fmt.Fprintf(&b, "mas.api_token_name=%s\n", cfg.MAS.APITokenName)
	fmt.Fprintf(&b, "mas.ca_file=%s\n", cfg.MAS.CAFile)
	fmt.Fprintf(&b, "mas.insecure=%t\n", cfg.MAS.InsecureSkipVerify)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

// desiredHash fingerprints what an action is trying to make true in MAS. A
// parked entry is retried as soon as this changes.
func desiredHash(configFingerprint string, action Action, payload mas.UserResource) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1\n%s\n%s\n", configFingerprint, action.ProfileID)
	if action.Type == ActionDeactivate {
		fmt.Fprintf(h, "deactivate\n%s\n", action.ExistingMAS)
	} else {
		_ = json.NewEncoder(h).Encode(payload)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}
