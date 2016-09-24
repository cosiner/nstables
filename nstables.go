package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"sync"
	"unicode"

	"github.com/cosiner/ygo/log"
	"github.com/miekg/dns"
)

var (
	resolvFile string
	hostsFile  string
)

func init() {
	flag.StringVar(&resolvFile, "resolv", "/etc/resolv.conf", "resolv file")
	flag.StringVar(&hostsFile, "hosts", "/etc/hosts", "hosts file")
	flag.Parse()
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

func parseNameservers(file, exclude string) ([]string, error) {
	cfg, err := dns.ClientConfigFromFile(file)
	if err != nil {
		return nil, err
	}

	var prev int
	for i, server := range cfg.Servers {
		if !strings.Contains(server, ":") {
			server += ":53"
			cfg.Servers[i] = server
		}
		log.Info(server)
		if server != exclude {
			if prev != i {
				cfg.Servers[prev] = cfg.Servers[i]
			}
			prev++
		}
	}

	return cfg.Servers[:prev], nil
}

func parseHosts(file string) (map[string][]string, error) {
	hosts := make(map[string][]string)
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128))
	lines := bytes.Split(content, []byte("\n"))
	for i := range lines {
		line := strings.TrimSpace(string(lines[i]))
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

		hosts[secs[0]] = secs[1:]
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

func main() {
	addr := "127.0.0.1:53"
	nameservers, err := parseNameservers(resolvFile, addr)
	if err != nil {
		log.Fatal(err)
	}
	if len(nameservers) == 0 {
		log.Fatal("no nameservers available")
	}

	log.Info("Nameservers:", nameservers)
	hosts, err := parseHosts(hostsFile)
	if err != nil {
		log.Fatal(err)
	}
	a, aaaa, err := separateRecords(hosts)
	if err != nil {
		log.Fatal(err)
	}
	s := Server{
		A:           a,
		AAAA:        aaaa,
		Nameservers: nameservers,
	}

	var wg sync.WaitGroup
	for _, net := range []string{"tcp4", "udp4"} {
		server := dns.Server{
			Addr:    addr,
			Net:     net,
			Handler: &s,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := server.ListenAndServe()
			if err != nil {
				log.Error(err)
			}
		}()
	}
	wg.Wait()
}
