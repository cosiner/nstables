package main

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type Server struct {
	cache Cache

	mu      sync.RWMutex
	Timeout time.Duration
	A       map[string][]net.IP
	AAAA    map[string][]net.IP
	NS      []string
}

func NewServer(cacheSize int, cacheExpire time.Duration) *Server {
	if cacheExpire <= 0 {
		cacheExpire = 300 * time.Second
	}
	return &Server{
		cache: NewMemCache(cacheSize, cacheExpire),
	}
}

func (s *Server) Reload(sr *Server) {
	s.mu.Lock()
	s.A = sr.A
	s.AAAA = sr.AAAA
	s.NS = sr.NS
	s.Timeout = sr.Timeout
	s.mu.Unlock()
}

func (s *Server) ips(name string, a bool) []net.IP {
	var ips []net.IP
	s.mu.RLock()
	if a {
		ips = s.A[name]
	} else {
		ips = s.AAAA[name]
	}
	s.mu.RUnlock()
	return ips
}

func (s *Server) nsAndTimeout() ([]string, time.Duration) {
	s.mu.RLock()
	servers := s.NS
	timeout := s.Timeout
	s.mu.RUnlock()
	return servers, timeout
}

func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	if len(r.Question) < 1 {
		dns.HandleFailed(w, r)
		return
	}

	var (
		msg *dns.Msg
		q   = &r.Question[0]
	)

	q.Name = dns.Fqdn(q.Name)
	if q.Qclass == dns.ClassINET && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA) {
		isA := q.Qtype == dns.TypeA
		msg = serveHosts(s.ips(q.Name, isA), isA, r)
	}

	if msg == nil {
		cacheKey := questionKey(q)
		msg = s.cache.Get(cacheKey)
		if msg == nil {
			ns, timeout := s.nsAndTimeout()
			msg = serveExtern(ns, timeout, r)
			if msg != nil {
				s.cache.Set(cacheKey, msg)
			}
		}
	}

	writeMsg(w, r, msg)
}

func serveHosts(ips []net.IP, isA bool, r *dns.Msg) *dns.Msg {
	if len(ips) == 0 {
		return nil
	}
	var m dns.Msg
	m.SetReply(r)
	m.Answer = answers(r.Question[0].Name, ips, isA)

	return &m
}

func serveExtern(nameservers []string, timeout time.Duration, r *dns.Msg) *dns.Msg {
	var (
		c   dns.Client
		msg *dns.Msg

		chmu   sync.Mutex
		ch     = make(chan *dns.Msg, len(nameservers))
		active int32
	)

	for _, nameserver := range nameservers {
		atomic.AddInt32(&active, 1)
		go func() {
			reply, _, err := c.Exchange(r, nameserver)
			if err != nil {
				log.Println(nameserver, err)
			}
			if ch != nil {
				chmu.Lock()
				if ch != nil {
					ch <- reply
				} else {
				}
				chmu.Unlock()
			}
		}()

		timer := time.NewTimer(timeout)
		for loop := true; loop; {
			select {
			case <-timer.C:
				loop = false
			case msg = <-ch:
				curr := atomic.AddInt32(&active, -1)
				if curr <= 0 || msg != nil {
					timer.Stop()
					loop = false
				}
			}
		}

		if msg != nil {
			break
		}
	}

	for msg == nil {
		msg = <-ch
		if atomic.AddInt32(&active, -1) <= 0 {
			break
		}
	}

	chmu.Lock()
	for loop := true; loop; {
		select {
		case <-ch:
		default:
			loop = false
		}
	}
	close(ch)
	ch = nil
	chmu.Unlock()

	return msg
}

func questionKey(q *dns.Question) string {
	return q.String()
}

func answers(name string, ips []net.IP, a bool) []dns.RR {
	hdr := dns.RR_Header{
		Name:   name,
		Rrtype: dns.TypeA,
		Class:  dns.ClassINET,
	}
	if !a {
		hdr.Rrtype = dns.TypeAAAA
	}

	rr := make([]dns.RR, len(ips))
	for i, ip := range ips {
		if a {
			rr[i] = &dns.A{
				Hdr: hdr,
				A:   ip,
			}
		} else {
			rr[i] = &dns.AAAA{
				Hdr:  hdr,
				AAAA: ip,
			}
		}
	}
	return rr
}

func writeMsg(w dns.ResponseWriter, r, m *dns.Msg) {
	if m == nil || w.WriteMsg(m) != nil {
		dns.HandleFailed(w, r)
	}
}
