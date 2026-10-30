package communication

import (
	"sync"
	"time"
)

const ProxyLogInterval = 10 * time.Minute

type ProxyLogLimiter struct {
	Mu   sync.Mutex
	Last map[string]time.Time
}

func (l *ProxyLogLimiter) Allow(purpose string, reason string, now time.Time) bool {
	l.Mu.Lock()
	defer l.Mu.Unlock()
	key := purpose + ":" + reason
	if last, ok := l.Last[key]; ok && now.Sub(last) < ProxyLogInterval {
		return false
	}
	l.Last[key] = now
	return true
}
