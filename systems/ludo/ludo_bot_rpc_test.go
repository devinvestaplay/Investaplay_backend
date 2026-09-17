package ludo

import (
	"strings"
	"testing"
)

func TestLudoBotRequestStorageKeysStayWithinNakamaLimit(t *testing.T) {
	longRequestID := strings.Repeat("request-id-", 40)
	keys := []string{
		ludoBotRequestStorageKey("3c8a5c12-e6a5-4cce-9aad-418b423161ed", longRequestID),
		ludoOnlineBotRequestStorageKey("3c8a5c12-e6a5-4cce-9aad-418b423161ed", "2P_ONLINE_ARENA", 2, longRequestID),
	}

	for _, key := range keys {
		if len(key) > 128 {
			t.Fatalf("storage key exceeds Nakama's 128-character limit: length=%d key=%q", len(key), key)
		}
	}
}

func TestLudoBotRequestStorageKeysAreDeterministicAndDistinct(t *testing.T) {
	first := ludoOnlineBotRequestStorageKey("user", "arena", 2, "request-a")
	if again := ludoOnlineBotRequestStorageKey("user", "arena", 2, "request-a"); first != again {
		t.Fatalf("same request produced different storage keys: %q != %q", first, again)
	}
	if other := ludoOnlineBotRequestStorageKey("user", "arena", 2, "request-b"); first == other {
		t.Fatalf("different requests produced the same storage key: %q", first)
	}
}
