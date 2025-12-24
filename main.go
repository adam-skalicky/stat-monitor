package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"gopkg.in/yaml.v3"
)

// --- Configuration ---

type MetricConfig struct {
	Type           string        `yaml:"type"`    // disk, disk_auto, service, net_rate, cpu, mem, swap
	Path           string        `yaml:"path"`    // for disk
	Measure        string        `yaml:"measure"` // percent_used, free_gb, rx_mbps, etc.
	Service        string        `yaml:"service"` // for systemd
	Diff           float64       `yaml:"diff"`
	Interval       time.Duration `yaml:"interval"`
	ResendInterval time.Duration `yaml:"resend_interval"`
}

type Config struct {
	Global struct {
		CheckFrequency time.Duration `yaml:"check_frequency"`
	} `yaml:"global"`
	Metrics map[string]MetricConfig `yaml:"metrics"`
}

// --- State Management ---

type MetricState struct {
	Name          string
	Config        MetricConfig
	LastValue     float64
	LastTime      time.Time
	LastBroadcast time.Time
	FirstRun      bool

	LastRawCounter uint64 // For calculating network rates
}

// CheckAndBroadcast decides if a broadcast is needed.
func (s *MetricState) CheckAndBroadcast(currentValue float64) {
	now := time.Now()

	// 1. First Run: Always broadcast immediately on startup
	if s.FirstRun {
		s.FirstRun = false
		s.updateState(currentValue, now)
		broadcast(s.Name, currentValue)
		return
	}

	timeSinceLast := now.Sub(s.LastBroadcast)

	// 2. Heartbeat (Resend Interval)
	if timeSinceLast >= s.Config.ResendInterval {
		s.updateState(currentValue, now)
		broadcast(s.Name, currentValue)
		return
	}

	// 3. Throttle (Interval) & Diff
	if timeSinceLast >= s.Config.Interval {
		diff := math.Abs(currentValue - s.LastValue)
		if diff >= s.Config.Diff {
			s.updateState(currentValue, now)
			broadcast(s.Name, currentValue)
			return
		}
	}
}

func (s *MetricState) updateState(val float64, t time.Time) {
	s.LastValue = val
	s.LastBroadcast = t
}

// --- Main Loop ---

func main() {
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Initialize States
	states := initializeStates(cfg)

	// Set up Ticker
	ticker := time.NewTicker(cfg.Global.CheckFrequency)
	defer ticker.Stop()

	// Set up Signal Handling
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Service started. Watching metrics...")

	// --- CHANGE: Immediate First Run ---
	// We run this ONCE before the ticker starts to ensure logs appear
	// instantly on system boot, rather than waiting 1 second.
	log.Println("Broadcasting initial baseline stats...")
	collectAndProcess(states)

	for {
		select {
		case <-sigs:
			log.Println("Shutting down...")
			return
		case <-ticker.C:
			collectAndProcess(states)
		}
	}
}

// --- Initialization Logic ---

func initializeStates(cfg *Config) map[string]*MetricState {
	states := make(map[string]*MetricState)

	for key, config := range cfg.Metrics {
		// DYNAMIC DISK
		if config.Type == "disk_auto" {
			partitions, err := disk.Partitions(false)
			if err != nil {
				log.Printf("Error detecting partitions: %v", err)
				continue
			}
			for _, p := range partitions {
				if strings.HasPrefix(p.Device, "/dev/") || p.Fstype == "ext4" || p.Fstype == "xfs" || p.Fstype == "apfs" || p.Fstype == "zfs" {
					cleanMount := strings.ReplaceAll(p.Mountpoint, "/", "_")
					if cleanMount == "_" {
						cleanMount = "_root"
					}
					name := fmt.Sprintf("%s%s", key, cleanMount)
					c := config
					c.Path = p.Mountpoint
					states[name] = &MetricState{Name: name, Config: c, FirstRun: true}
					log.Printf("Discovered disk: %s -> %s", p.Mountpoint, name)
				}
			}
			continue
		}

		// CPU PER CORE
		if config.Type == "cpu" && config.Measure == "per_core" {
			count, _ := cpu.Counts(true)
			for i := 0; i < count; i++ {
				name := fmt.Sprintf("cpu_core_%d", i)
				states[name] = &MetricState{Name: name, Config: config, FirstRun: true}
			}
			continue
		}

		// STANDARD METRICS
		states[key] = &MetricState{
			Name:     key,
			Config:   config,
			FirstRun: true,
		}
	}
	return states
}

// --- Collection Logic ---

func collectAndProcess(states map[string]*MetricState) {
	for _, state := range states {
		// Run checks in parallel
		go func(s *MetricState) {
			val, err := getValue(s)
			// We only broadcast if there was NO error.
			if err == nil {
				s.CheckAndBroadcast(val)
			}
		}(state)
	}
}

func getValue(s *MetricState) (float64, error) {
	switch s.Config.Type {

	case "disk", "disk_auto":
		u, err := disk.Usage(s.Config.Path)
		if err != nil {
			return 0, err
		}
		switch s.Config.Measure {
		case "percent_free":
			return 100.0 - u.UsedPercent, nil
		case "used_gb":
			return float64(u.Used) / 1024 / 1024 / 1024, nil
		case "free_gb":
			return float64(u.Free) / 1024 / 1024 / 1024, nil
		case "used_mb":
			return float64(u.Used) / 1024 / 1024, nil
		case "free_mb":
			return float64(u.Free) / 1024 / 1024, nil
		default:
			return u.UsedPercent, nil
		}

	case "service":
		cmd := exec.Command("systemctl", "is-active", "--quiet", s.Config.Service)
		err := cmd.Run()
		if err != nil {
			return 0.0, nil
		}
		return 1.0, nil

	case "net_rate":
		cts, err := net.IOCounters(false)
		if err != nil || len(cts) == 0 {
			return 0, fmt.Errorf("no net")
		}

		var currentRaw uint64
		if s.Config.Measure == "tx_mbps" {
			currentRaw = cts[0].BytesSent
		} else {
			currentRaw = cts[0].BytesRecv
		}

		now := time.Now()

		// Note on Restart: We CANNOT broadcast a rate on the very first instant
		// because we need a delta (Current - Previous).
		// This block initializes the baseline so the SECOND tick (e.g. 1s later) works.
		if s.LastTime.IsZero() {
			s.LastRawCounter = currentRaw
			s.LastTime = now
			return 0, fmt.Errorf("initializing net rate")
		}

		deltaBytes := float64(currentRaw - s.LastRawCounter)
		deltaTime := now.Sub(s.LastTime).Seconds()

		s.LastRawCounter = currentRaw
		s.LastTime = now

		if deltaTime <= 0 {
			return 0, fmt.Errorf("time skew")
		}

		mbps := (deltaBytes * 8) / (1024 * 1024) / deltaTime
		if mbps < 0 {
			mbps = 0
		}
		return mbps, nil

	case "cpu":
		if s.Config.Measure == "total" {
			c, _ := cpu.Percent(0, false)
			if len(c) > 0 {
				return c[0], nil
			}
		} else if s.Config.Measure == "per_core" {
			c, _ := cpu.Percent(0, true)
			var idx int
			fmt.Sscanf(s.Name, "cpu_core_%d", &idx)
			if idx < len(c) {
				return c[idx], nil
			}
		}
		return 0, fmt.Errorf("cpu err")

	case "mem":
		v, _ := mem.VirtualMemory()
		if s.Config.Measure == "free_gb" {
			return float64(v.Free) / 1024 / 1024 / 1024, nil
		}
		return v.UsedPercent, nil

	case "swap":
		v, _ := mem.SwapMemory()
		if s.Config.Measure == "free_gb" {
			return float64(v.Free) / 1024 / 1024 / 1024, nil
		}
		return v.UsedPercent, nil

	case "load":
		l, _ := load.Avg()
		return l.Load5, nil

	case "uptime":
		u, _ := host.Uptime()
		return float64(u) / 3600, nil
	}

	return 0, fmt.Errorf("unknown type")
}

func broadcast(name string, value float64) {
	log.Printf("[BROADCAST] %s: %.2f\n", name, value)
}

func loadConfig(path string) (*Config, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	cfg.Global.CheckFrequency = 1 * time.Second
	if err := yaml.Unmarshal(f, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
