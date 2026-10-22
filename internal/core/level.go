package core

import (
	"fmt"
	"strings"
)

// Level is the severity of a message.
type Level string

const (
	LevelInfo     Level = "info"
	LevelSuccess  Level = "success"
	LevelWarning  Level = "warning"
	LevelError    Level = "error"
	LevelCritical Level = "critical"
)

// Levels lists every level, in the order of the documentation.
var Levels = []Level{LevelInfo, LevelSuccess, LevelWarning, LevelError, LevelCritical}

// ParseLevel accepts a level name, case-insensitively.
func ParseLevel(s string) (Level, error) {
	l := Level(strings.ToLower(strings.TrimSpace(s)))
	for _, known := range Levels {
		if l == known {
			return l, nil
		}
	}
	return "", fmt.Errorf("unknown level %q (use info, success, warning, error or critical)", s)
}
