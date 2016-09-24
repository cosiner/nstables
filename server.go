package main

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cosiner/ygo/log"
	"github.com/miekg/dns"
)

type Server struct {
	A           map[string][]net.IP
	AAAA        map[string][]net.IP
	Nameservers []string
}

func (s *Server) answers(name string, ips []net.IP, a bool) []dns.RR {
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

func (s *Server) writeMsg(w dns.ResponseWriter, r, m *dns.Msg) {
	if w.WriteMsg(m) != nil {
		dns.HandleFailed(w, r)
	}
}

func (s *Server) serveHosts(w dns.ResponseWriter, r *dns.Msg) bool {
	name := dns.Fqdn(r.Question[0].Name)

	isA := r.Question[0].Qtype == dns.TypeA
	var ips []net.IP
	if isA {
		ips = s.A[name]
	} else {
		ips = s.AAAA[name]
	}
	if len(ips) == 0 {
		return false
	}
	var m dns.Msg
	m.SetReply(r)
	m.Answer = s.answers(name, ips, isA)

	s.writeMsg(w, r, &m)
	return true
}

func (s *Server) serveExtern(w dns.ResponseWriter, r *dns.Msg) {
	var (
		c   dns.Client
		msg *dns.Msg

		chmu   sync.Mutex
		ch     = make(chan *dns.Msg, len(s.Nameservers))
		active int32
	)

	for _, nameserver := range s.Nameservers {
		atomic.AddInt32(&active, 1)
		go func() {
			reply, _, err := c.Exchange(r, nameserver)
			if err != nil {
				log.Error(nameserver, err)
			}
			if ch != nil {
				chmu.Lock()
				if ch != nil {
					ch <- reply
				}
				chmu.Unlock()
			}
		}()

		timer := time.NewTimer(time.Second)
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

	if msg != nil && w.WriteMsg(msg) == nil {
		return
	}
	dns.HandleFailed(w, r)
}

func (s *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	if len(r.Question) < 1 {
		dns.HandleFailed(w, r)
		return
	}

	q := &r.Question[0]
	if q.Qclass == dns.ClassINET && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA) {
		if s.serveHosts(w, r) {
			log.Info("Response From Hosts")
			return
		}
	}

	log.Info("Response From External Nameserver")
	s.serveExtern(w, r)
}
