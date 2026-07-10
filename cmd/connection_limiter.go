package main

import "sync"

type activeConnectionLimiter struct {
	mu          sync.Mutex
	globalLimit int
	groupLimit  int
	ipLimit     int
	global      int
	groups      map[string]int
	ips         map[string]int
}

func newActiveConnectionLimiter(globalLimit, groupLimit, ipLimit int) *activeConnectionLimiter {
	return &activeConnectionLimiter{
		globalLimit: globalLimit,
		groupLimit:  groupLimit,
		ipLimit:     ipLimit,
		groups:      make(map[string]int),
		ips:         make(map[string]int),
	}
}

func (l *activeConnectionLimiter) acquire(group, ip string) (func(), bool) {
	if l == nil || group == "" || ip == "" {
		return func() {}, false
	}
	l.mu.Lock()
	if l.global >= l.globalLimit || l.groups[group] >= l.groupLimit || l.ips[ip] >= l.ipLimit {
		l.mu.Unlock()
		return func() {}, false
	}
	l.global++
	l.groups[group]++
	l.ips[ip]++
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.global--
			l.groups[group]--
			l.ips[ip]--
			if l.groups[group] == 0 {
				delete(l.groups, group)
			}
			if l.ips[ip] == 0 {
				delete(l.ips, ip)
			}
		})
	}, true
}

var (
	widgetActiveConnections = newActiveConnectionLimiter(2000, 500, 50)
	agentActiveConnections  = newActiveConnectionLimiter(500, 10, 100)
)
