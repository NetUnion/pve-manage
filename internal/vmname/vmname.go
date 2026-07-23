package vmname

import (
	"fmt"
	"strings"
	"unicode"
)

// Prefixed returns the name stored in PVE. Managed VM names are prefixed with
// their owner unless the caller already supplied that prefix.
func Prefixed(owner, name string) string {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return name
	}
	prefix := owner + "-"
	if strings.HasPrefix(name, prefix) {
		return name
	}
	return prefix + name
}

// ValidatePVE checks the DNS-name format accepted by PVE for VM names.
func ValidatePVE(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("VM name is required")
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || !asciiAlphaNumeric(rune(label[0])) || !asciiAlphaNumeric(rune(label[len(label)-1])) {
			return invalidNameError(name)
		}
		for _, char := range label {
			if !asciiAlphaNumeric(char) && char != '-' {
				return invalidNameError(name)
			}
		}
	}
	return nil
}

// ValidateManaged checks the final owner-prefixed name sent to PVE.
func ValidateManaged(owner, name string) error {
	return ValidatePVE(Prefixed(owner, name))
}

func asciiAlphaNumeric(char rune) bool {
	return char <= unicode.MaxASCII &&
		(char >= 'a' && char <= 'z' ||
			char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9')
}

func invalidNameError(name string) error {
	return fmt.Errorf("invalid VM name %q: the final PVE name must contain only letters, digits, hyphens, and dots; each dot-separated part must start and end with a letter or digit", name)
}
