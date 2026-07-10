package jsonutil

import "fmt"

const DefaultMaxObjectDepth = 16

func IsUnsafeObjectKey(key string) bool {
	switch key {
	case "__proto__", "constructor", "prototype":
		return true
	default:
		return false
	}
}

// ValidateSafeObjectKeys recursively rejects keys that are interpreted as
// prototype accessors by JavaScript and bounds nested traversal.
func ValidateSafeObjectKeys(value any, maxDepth int) error {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxObjectDepth
	}
	return validateSafeObjectKeys(value, 0, maxDepth)
}

func validateSafeObjectKeys(value any, depth, maxDepth int) error {
	if depth > maxDepth {
		return fmt.Errorf("JSON object nesting exceeds limit")
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if IsUnsafeObjectKey(key) {
				return fmt.Errorf("unsafe JSON object key")
			}
			if err := validateSafeObjectKeys(child, depth+1, maxDepth); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateSafeObjectKeys(child, depth+1, maxDepth); err != nil {
				return err
			}
		}
	}
	return nil
}
