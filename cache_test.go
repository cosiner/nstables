package main

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestName(t *testing.T) {
	c := NewMemCache(3, 2*time.Millisecond)
	keys := []string{"a", "b", "c", "d", "e"}
	for _, key := range keys {
		t.Log(key, c.Set(key, &dns.Msg{
			Compress: true,
		}))
	}

	for i := 0; i < 2; i++ {
		for _, key := range keys {
			got := c.Get(key)
			if got == nil {
				t.Log(key, "got nil")
			} else {
				t.Log(key, got.Compress)
			}
		}

		if i == 0 {
			time.Sleep(2 * time.Millisecond)
			c.Remove("")
		}
	}

}
