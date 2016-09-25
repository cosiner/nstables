package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/cosiner/process"
	"github.com/miekg/dns"
	yaml "gopkg.in/yaml.v2"
)

type Config struct {
	Pid     string   `yaml:"pid"`
	Listens []string `yaml:"listens"`

	TimeoutMs   int      `yaml:"timeoutMs"`
	ResolvFiles []string `yaml:"resolvFiles"`
	Nameservers []string `yaml:"nameservers"`
	HostsFiles  []string `yaml:"hostsFiles"`
	Hosts       []string `yaml:"hosts"`
}

var (
	conf string
	sig  string
	pid  int
)

func init() {
	flag.StringVar(&conf, "c", "nstables.yaml", "configure file in yaml format")
	flag.StringVar(&sig, "s", "", "signal, stop/reload")
	flag.IntVar(&pid, "pid", 0, "process pid")
	flag.Parse()
}

func parseConfigFile(conf string) (Config, error) {
	var cfg Config
	data, err := ioutil.ReadFile(conf)
	if err != nil {
		return cfg, err
	}
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return cfg, err
	}
	if len(cfg.Listens) == 0 {
		return cfg, errors.New("no address to listening")
	}
	return cfg, err
}

func resolveConfig(cfg *Config) (*Server, error) {
	s := NewServer()
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 1000
	}
	s.Timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond

	var err error
	s.NS, err = parseNameservers(cfg)
	if err != nil {
		return s, err
	}
	if len(s.NS) == 0 {
		return s, errors.New("no nameservers available")
	}

	hosts, err := parseHosts(cfg)
	if err != nil {
		return s, err
	}

	s.A, s.AAAA, err = separateRecords(hosts)
	return s, nil
}

func handleSig(cfg *Config) {
	const (
		SIG_RELOAD = "reload"
		SIG_STOP   = "stop"
	)

	var s os.Signal
	switch sig {
	case SIG_RELOAD:
		s = syscall.SIGUSR1
	case SIG_STOP:
		s = syscall.SIGINT
	default:
		log.Fatalln("illegal signal:", sig)
	}

	if pid <= 0 && cfg.Pid != "" {
		p := process.NewPIDFile(cfg.Pid)

		var err error
		pid, err = p.Read()
		if err != nil {
			log.Fatalln("read pid file failed:", err)
		}
	}
	if pid <= 0 {
		log.Fatalln("process id is unknown")
	}

	err := process.Kill(pid, s)
	if err != nil {
		log.Fatalln("send signal failed:", pid, err)
	}
}

func main() {
	cfg, err := parseConfigFile(conf)
	if err != nil {
		log.Fatal(err)
	}
	if sig != "" {
		handleSig(&cfg)
		return
	}

	s, err := resolveConfig(&cfg)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.Pid != "" {
		pid := process.NewPIDFile(cfg.Pid)
		err = pid.Write()
		if err != nil {
			log.Fatal(err)
		}
		defer pid.Remove()
	}

	signal := process.NewSignal()
	for _, lis := range cfg.Listens {
		net, addr := splitNetAddr(lis)
		server := dns.Server{
			Net:     net,
			Addr:    completeDNSPort(addr),
			Handler: s,
		}
		go func() {
			defer signal.Close()

			err := server.ListenAndServe()
			if err != nil {
				log.Println(err)
			}
		}()
	}

	signal.
		Exit(syscall.SIGTERM, syscall.SIGINT, syscall.SIGABRT, syscall.SIGQUIT).
		Default(process.SigIgnore).
		Ignore(syscall.SIGHUP).
		Handle(func() bool {
			err := reload(s)
			if err != nil {
				log.Println("reload config failed:", err)
			} else {
				log.Println("reload config success.")
			}

			return true
		}, syscall.SIGUSR1, syscall.SIGUSR2).
		Loop()
}

func reload(s *Server) error {
	cfg, err := parseConfigFile(conf)
	if err != nil {
		return err
	}
	sr, err := resolveConfig(&cfg)
	if err != nil {
		return err
	}

	s.Reload(sr)
	return nil
}
