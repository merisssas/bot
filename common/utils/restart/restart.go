package restart

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

const sentinelPrefix = "saveanybot-restart:"

var sentinelToken = newToken()

// Error returns an error that signals the bot to restart.
func Error() error {
	return errors.New(sentinelPrefix + sentinelToken)
}

// Matches reports whether the given message matches the restart sentinel.
func Matches(message string) bool {
	return message == sentinelPrefix+sentinelToken
}

func newToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("fallback-%d", len(buf))
	}
	return hex.EncodeToString(buf)
}
