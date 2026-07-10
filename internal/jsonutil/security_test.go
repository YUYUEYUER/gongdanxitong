package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestValidateSafeObjectKeysRejectsNestedPrototypeKeys(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"__proto__":{"polluted":true}}`,
		`{"safe":{"constructor":{"prototype":{"polluted":true}}}}`,
		`{"safe":[{"prototype":true}]}`,
	} {
		var value map[string]any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if err := ValidateSafeObjectKeys(value, DefaultMaxObjectDepth); err == nil {
			t.Fatalf("unsafe payload accepted: %s", raw)
		}
	}
}

func TestValidateSafeObjectKeysAcceptsNormalNestedValues(t *testing.T) {
	t.Parallel()
	value := map[string]any{"profile": map[string]any{"tier": "gold"}, "tags": []any{"one", "two"}}
	if err := ValidateSafeObjectKeys(value, DefaultMaxObjectDepth); err != nil {
		t.Fatal(err)
	}
}
