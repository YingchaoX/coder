package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewSessionID 生成新的会话 ID / Generates a new session ID
func NewSessionID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("sess_%d_%s", time.Now().UTC().Unix(), hex.EncodeToString(buf))
}
