package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"

	"github.com/insomniacslk/neato"
)

var (
	flagPath     = pflag.String("p", "/metrics", "HTTP path where to expose metrics to")
	flagListen   = pflag.StringP("listen-address", "l", ":9110", "Address to listen to")
	flagToken    = pflag.StringP("token", "t", "", "Authorization token")
	flagBots     = pflag.StringP("bots", "b", "", "Comma-separated list of bot numbers, e.g. \"1,3\". Use 0 or leave it empty to use all bots. Bot numbering start at 1")
	flagInterval = pflag.DurationP("interval", "i", 1*time.Minute, "Interval between sensor readings, expressed as a Go duration string")
)

var robotAttrs = []string{"name", "serial", "model", "firmware", "mac"}

func makeGauge(name, help string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "neato_" + name,
			Help: "Neato - " + help,
		},
		robotAttrs,
	)
}

var (
	batteryGauge = makeGauge("battery", "battery level (percentage)")
	areaGauge    = makeGauge("area", "cleaned area (square meters)")
	stateGauge   = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "neato_state",
			Help: "Neato - robot state",
		},
		append(robotAttrs, "error", "alert", "state", "action", "category", "navigation_mode", "is_charging", "is_docked", "is_schedule_enabled", "dock_has_been_seen", "charge"),
	)
)

func collector(robots []*neato.Robot) {
	for {
		for _, r := range robots {
			s, err := r.State()
			if err != nil {
				log.Printf("Failed to get state for robot '%s': %v", r.Name, err)
				time.Sleep(*flagInterval)
				continue
			}
			model := "unknown"
			if r.Model != nil {
				model = *r.Model
			}
			firmware := "unknown"
			if r.Firmware != nil {
				firmware = *r.Firmware
			}
			mac := "unknown"
			if r.MACAddress != nil {
				mac = *r.MACAddress
			}
			batteryGauge.WithLabelValues(r.Name, r.Serial, model, firmware, mac).Set(float64(s.Details.Charge))

			errStr := "unset"
			if s.Error != nil {
				errStr = *s.Error
			}
			alert := "unset"
			if s.Alert != nil {
				alert = *s.Alert
			}
			stateGauge.WithLabelValues(
				r.Name, r.Serial, model, firmware, mac,
				errStr, alert, s.State.String(), s.Action.String(),
				s.Cleaning.Category.String(), s.Cleaning.NavigationMode.String(),
				strconv.FormatBool(s.Details.IsCharging), strconv.FormatBool(s.Details.IsDocked),
				strconv.FormatBool(s.Details.IsScheduleEnabled), strconv.FormatBool(s.Details.DockHasBeenSeen),
				strconv.FormatInt(int64(s.Details.Charge), 10),
			).Set(1.)

			// get maps
			maps, err := r.Maps()
			if err != nil {
				log.Printf("Failed to get maps for robot '%s' (serial '%s'): %v", r.Name, r.Serial, err)
			} else {
				if len(maps) == 0 {
					log.Printf("No maps found for robot '%s': (serial: '%s')", r.Name, r.Serial)
				} else {
					if maps[0].CleanedArea != nil {
						areaGauge.WithLabelValues(r.Name, r.Serial, model, firmware, mac).Set(float64(*maps[0].CleanedArea))
					} else {
						log.Printf("No cleaned area is set for robo '%s' (serial: '%s')", r.Name, r.Serial)
					}
				}
			}
		}

		time.Sleep(*flagInterval)
	}
}

func getBots(s string) ([]int, error) {
	if s == "" {
		// `nil` means that we want all the bots
		return nil, nil
	}
	parts := strings.Split(s, ",")
	botMap := make(map[int]struct{})
	hasZero := false
	for _, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("string '%s' is not a valid digit: %w", p, err)
		}
		if n == 0 {
			hasZero = true
		}
		botMap[int(n)] = struct{}{}
	}
	if hasZero {
		if len(botMap) > 1 {
			return nil, fmt.Errorf("cannot have a list of indexes containing zero")
		}
		if len(botMap) == 0 {
			// `nil` means that we want all the bots
			return nil, nil
		}
	}

	bots := make([]int, 0)
	for n := range botMap {
		bots = append(bots, int(n))
	}
	sort.Slice(bots, func(i, j int) bool { return i < j })
	return bots, nil
}

func main() {
	pflag.Parse()

	if *flagToken == "" {
		log.Fatalf("Empty authorization token")
	}

	bots, err := getBots(*flagBots)
	if err != nil {
		log.Fatalf("Failed to parse bot indexes: %v", err)
	}

	endpoint := "https://beehive.neatocloud.com"
	header := url.Values{}
	header.Set("Authorization", fmt.Sprintf("Token token=%s", *flagToken))
	s := neato.NewPasswordSession(endpoint, &header)
	acc := neato.NewAccount(s)

	allRobots, err := acc.Robots()
	if err != nil {
		log.Fatalf("Failed to get robots: %v", err)
	}
	if len(allRobots) == 0 {
		log.Fatalf("No bots found")
	}

	robots := make([]*neato.Robot, 0)
	if bots == nil {
		// we want all the bots
		robots = allRobots
	} else {
		// we only want a subset of bots.
		for _, n := range bots {
			if n > len(allRobots) {
				log.Fatalf("Robot number %d out of bounds, there are %d robots in total", n, len(allRobots))
			}
			robots = append(robots, allRobots[n-1])
		}
	}

	// register all gauges
	if err := prometheus.Register(batteryGauge); err != nil {
		log.Fatalf("Failed to register Neato battery gauge: %v", err)
	}
	if err := prometheus.Register(areaGauge); err != nil {
		log.Fatalf("Failed to register Neato area gauge: %v", err)
	}
	if err := prometheus.Register(stateGauge); err != nil {
		log.Fatalf("Failed to register Neato state gauge: %v", err)
	}

	// start collector
	go collector(robots)

	server := http.Server{Addr: *flagListen}
	http.Handle(*flagPath, promhttp.Handler())
	log.Printf("Starting server on %s", *flagListen)
	log.Fatal(server.ListenAndServe())
}
