package mas

import (
	"encoding/json"
	"strings"
)

const redactedValue = "[REDACTED]"

// RedactPayload redacts common secret-like fields in JSON before support logging.
func RedactPayload(raw []byte) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return redactedValue
	}
	redactValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return redactedValue
	}
	return string(encoded)
}

func redactValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if isSensitivePayloadKey(key) {
				v[key] = redactedValue
				continue
			}
			redactValue(nested)
		}
	case []any:
		for _, nested := range v {
			redactValue(nested)
		}
	}
}

func isSensitivePayloadKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "_", ""))
	for _, marker := range []string{"password", "secret", "token", "apikey", "authorization", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
