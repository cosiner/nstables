package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"unicode"

	"github.com/miekg/dns"
)

func completeDNSPort(addr string) string {
	if !strings.Contains(addr, ":") {
		addr += ":53"
	}
	return addr
}

func trimStrings(ss []string) {
	for i := range ss {
		ss[i] = strings.TrimSpace(ss[i])
	}
}

func mergeSpace(buf *bytes.Buffer, s string) string {
	buf.Reset()
	var hasSpace bool
	for _, r := range s {
		if unicode.IsSpace(r) {
			hasSpace = true
		} else {
			if hasSpace {
				hasSpace = false
				buf.WriteRune(' ')
			}
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func removeElement(ss []string, match func(string) bool) []string {
	var prev int
	for i := range ss {
		if !match(ss[i]) {
			if prev != i {
				ss[prev] = ss[i]
			}
			prev += 1
		}
	}
	return ss[:prev]
}

func parseNameservers(cfg *Config) ([]string, error) {
	listenNs := make(map[string]bool)
	for _, lis := range cfg.Listens {
		_, addr := splitNetAddr(lis)
		listenNs[completeDNSPort(addr)] = true
	}

	servers := cfg.Nameservers

	for _, file := range cfg.ResolvFiles {
		c, err := dns.ClientConfigFromFile(file)
		if err != nil {
			return nil, err
		}
		servers = append(servers, c.Servers...)
	}

	var (
		prev  int
		added = make(map[string]bool)
	)
	for i, server := range servers {
		server = completeDNSPort(server)
		servers[i] = server

		if !listenNs[server] && !added[server] {
			added[server] = true
			if prev != i {
				servers[prev] = servers[i]
			}
			prev++
		}
	}

	return servers[:prev], nil
}

func parseHosts(cfg *Config) (map[string][]string, error) {
	hosts := make(map[string][]string)

	var lines []string
	for _, file := range cfg.HostsFiles {
		c, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, err
		}
		content := string(c)
		if content != "" {
			lines = append(lines, strings.Split(content, "\n")...)
		}
	}
	lines = append(lines, cfg.Hosts...)

	buf := bytes.NewBuffer(make([]byte, 0, 128))
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = mergeSpace(buf, line)
		secs := strings.Split(line, " ")
		trimStrings(secs)
		secs = removeElement(secs, func(s string) bool { return s == "" })
		if len(secs) < 2 {
			return nil, fmt.Errorf("illegal hostt line: %d, %s", i+1, line)
		}
		hosts[secs[0]] = append(hosts[secs[0]], secs[1:]...)
	}
	return hosts, nil
}

func separateRecords(hosts map[string][]string) (a, aaaa map[string][]net.IP, err error) {
	a = make(map[string][]net.IP)
	aaaa = make(map[string][]net.IP)

	for ip, h := range hosts {
		addr, err := net.ResolveIPAddr("", ip)
		if err != nil {
			return nil, nil, err
		}

		var m map[string][]net.IP
		if addr.IP.To4() != nil {
			m = a
		} else {
			m = aaaa
		}
		for _, host := range h {
			host = dns.Fqdn(host)
			m[host] = append(m[host], addr.IP)
		}
	}
	return a, aaaa, nil
}

func splitNetAddr(s string) (net, addr string) {
	secs := strings.SplitN(s, "://", 2)
	if len(secs) == 1 {
		return "udp", secs[0]
	}
	return secs[0], secs[1]
}
