package user

import (
	"testing"

	"github.com/abhinavxd/libredesk/internal/user/models"
)

func TestAgentCacheVersionRejectsInFlightStaleLoads(t *testing.T) {
	manager := &Manager{
		agentCache:           make(map[int]cachedAgent),
		agentCacheGeneration: make(map[int]uint64),
	}
	agent := models.User{ID: 42}

	beforeInvalidation := manager.currentAgentCacheVersion(agent.ID)
	manager.agentCacheMu.Lock()
	manager.agentCacheGeneration[agent.ID]++
	manager.agentCacheMu.Unlock()
	if manager.cacheAgentIfCurrent(agent, beforeInvalidation) {
		t.Fatal("an individual invalidation must reject an older in-flight load")
	}

	current := manager.currentAgentCacheVersion(agent.ID)
	if !manager.cacheAgentIfCurrent(agent, current) {
		t.Fatal("the current cache generation should be stored")
	}

	beforeGlobalInvalidation := manager.currentAgentCacheVersion(agent.ID)
	manager.agentCacheMu.Lock()
	manager.agentCacheEpoch++
	manager.agentCacheMu.Unlock()
	if manager.cacheAgentIfCurrent(agent, beforeGlobalInvalidation) {
		t.Fatal("a global invalidation must reject an older in-flight load")
	}
}
