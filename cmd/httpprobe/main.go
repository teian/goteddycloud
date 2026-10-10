// SPDX-License-Identifier: GPL-2.0-only
// httpprobe measures bounded HTTP time requests against an isolated local server.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"time"
)

type result struct {
	Requests           int     `json:"requests"`
	RequestsPerSecond  float64 `json:"requests_per_second"`
	MedianMicroseconds float64 `json:"median_us"`
	P95Microseconds    float64 `json:"p95_us"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	target := flag.String("url", "http://127.0.0.1:18080/v1/time", "isolated loopback time endpoint")
	requests := flag.Int("requests", 20000, "requests per round")
	concurrency := flag.Int("concurrency", 4, "concurrent clients")
	rounds := flag.Int("rounds", 5, "measurement rounds")
	flag.Parse()
	parsed, err := url.Parse(*target)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") {
		return fmt.Errorf("requires a loopback HTTP URL")
	}
	if *requests < 1 || *requests > 1000000 || *concurrency < 1 || *concurrency > 64 || *requests < *concurrency || *rounds < 1 || *rounds > 20 {
		return fmt.Errorf("invalid benchmark bounds")
	}
	transport := &http.Transport{MaxIdleConnsPerHost: *concurrency, MaxConnsPerHost: *concurrency}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request := func() error {
		response, err := client.Get(*target)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 128))
		if err != nil {
			return err
		}
		if response.StatusCode != 200 || len(body) == 0 {
			return fmt.Errorf("invalid time response: status %d", response.StatusCode)
		}
		for _, char := range body {
			if char < '0' || char > '9' {
				return fmt.Errorf("invalid time payload")
			}
		}
		return nil
	}
	results := make([]result, 0, *rounds)
	for range *rounds {
		start := make(chan struct{})
		samples := make(chan []float64, *concurrency)
		failures := make(chan error, *concurrency)
		var ready, workers sync.WaitGroup
		ready.Add(*concurrency)
		workers.Add(*concurrency)
		for worker := range *concurrency {
			count := *requests / *concurrency
			if worker == 0 {
				count += *requests % *concurrency
			}
			go func() {
				defer workers.Done()
				var warmError error
				for range 10 {
					if err := request(); err != nil {
						warmError = err
						break
					}
				}
				timings := make([]float64, 0, count)
				ready.Done()
				<-start
				if warmError != nil {
					failures <- warmError
					return
				}
				for range count {
					began := time.Now()
					if err := request(); err != nil {
						failures <- err
						return
					}
					timings = append(timings, float64(time.Since(began).Nanoseconds())/1000)
				}
				samples <- timings
			}()
		}
		ready.Wait()
		began := time.Now()
		close(start)
		workers.Wait()
		elapsed := time.Since(began)
		close(failures)
		for err := range failures {
			return err
		}
		close(samples)
		timings := make([]float64, 0, *requests)
		for batch := range samples {
			timings = append(timings, batch...)
		}
		sort.Float64s(timings)
		results = append(results, result{Requests: len(timings), RequestsPerSecond: float64(len(timings)) / elapsed.Seconds(), MedianMicroseconds: timings[len(timings)/2], P95Microseconds: timings[(len(timings)-1)*95/100]})
	}
	return json.NewEncoder(os.Stdout).Encode(results)
}
