package main

import (
	"sync"
	"time"

	"github.com/miekg/dns"
)

type Cache interface {
	Get(string) *dns.Msg
	Set(string, *dns.Msg) bool
	Remove(string)
}

func NewMemCache(cap int, expire time.Duration) Cache {
	m := &memCache{
		expire: expire,
		cap:    cap,
	}
	if cap > 0 {
		m.keys = make(map[string]int)
		m.msgs = make([]memCacheItem, cap)
	}
	return m
}

type memCacheItem struct {
	key      string
	msg      *dns.Msg
	expireAt time.Time
}

type memCache struct {
	cap    int
	expire time.Duration

	mu   sync.RWMutex
	keys map[string]int
	// ring buffer
	begin int
	end   int
	msgs  []memCacheItem
}

func (m *memCache) Get(key string) (dmsg *dns.Msg) {
	if m.cap <= 0 {
		return nil
	}

	m.mu.RLock()
	index, has := m.keys[key]
	if has {
		msg := m.msgs[index]
		now := time.Now()
		if msg.expireAt.Before(now) {
			m.remove(index, now)
		} else {
			dmsg = msg.msg
		}
	}
	m.mu.RUnlock()
	return dmsg
}
func (m *memCache) realIndex(i int) int {
	if i < m.cap {
		return i
	}
	return i - m.cap
}

func (m *memCache) virtualEnd() int {
	if m.end > m.begin {
		return m.end
	}
	return m.end + m.cap
}

func (m *memCache) remove(index int, now time.Time) int {
	if len(m.keys) == 0 {
		return 0
	}

	var (
		expireIndex = -1
		end         = m.virtualEnd()
	)
	for i := index; i < end; i++ {
		j := m.realIndex(i)
		if m.msgs[j].expireAt.Before(now) {
			expireIndex = i
		} else {
			break
		}
	}
	if expireIndex < 0 {
		return 0
	}

	for i := m.begin; i <= expireIndex; i++ {
		delete(m.keys, m.msgs[m.realIndex(i)].key)
	}
	m.begin = m.realIndex(expireIndex + 1)
	return expireIndex - index + 1
}

func (m *memCache) Set(key string, msg *dns.Msg) bool {
	if m.cap <= 0 {
		return false
	}

	now := time.Now()

	var added bool
	m.mu.Lock()
	m.remove(m.begin, now)
	if m.begin != m.end || len(m.keys) == 0 {
		m.msgs[m.end] = memCacheItem{
			key:      key,
			msg:      msg,
			expireAt: now.Add(m.expire),
		}
		m.keys[key] = m.end

		m.end = m.realIndex(m.end + 1)
		added = true
	}
	m.mu.Unlock()
	return added
}

func (m *memCache) Remove(key string) {
	if m.cap <= 0 {
		return
	}

	now := time.Now()
	m.mu.Lock()
	if key == "" {
		m.remove(m.begin, now)
	} else {
		index, has := m.keys[key]
		if has {
			m.remove(index, now)
		}
	}
	m.mu.Unlock()
}
