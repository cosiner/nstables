package main

import (
	"log"
	"net"
	"time"

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
		c  dns.Client
		ch = make(chan *dns.Msg)
	)
	defer close(ch)

	for _, nameserver := range s.Nameservers {
		go func() {
			m, _, err := c.Exchange(r, nameserver)
			if err != nil {
				log.Println(nameserver, err)
			}
			ch <- m
		}()

		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case m := <-ch:
			timer.Stop()
			if m != nil {
				s.writeMsg(w, r, m)
				return
			}
		}
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
			return
		}
	}

	s.serveExtern(w, r)
}
