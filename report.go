package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// displayReport outputs a report of proxy performance & health.
func displayReport(results []*result) {
	success := 0
	fail := 0
	averageTTFB := int64(0)

	// Calculate stats
	for _, v := range results {
		if v == nil {
			continue
		}

		if v.StatusCode == -1 {
			fail++

			continue
		}

		averageTTFB += v.Latency.TTFB
		success++
	}

	if success > 0 {
		averageTTFB /= int64(success)
	}

	switch output {
	case "json":
		// JSON formatted report
		report := &report{Success: success, Fail: fail, AverageTTFB: averageTTFB, Results: results}

		b, err := json.Marshal(report)
		if err != nil {
			panic(err)
		}

		// Write to stdout
		os.Stdout.Write(b)

		return
	case "yaml":
		// YAML formatted report
		report := &report{Success: success, Fail: fail, AverageTTFB: averageTTFB, Results: results}

		b, err := yaml.Marshal(report)
		if err != nil {
			panic(err)
		}

		// Write to stdout
		os.Stdout.Write(b)

		return
	default:
		break
	}

	// Plaintext formatted report
	fmt.Printf("Success rate:      %d/%d\n", success, len(results))
	fmt.Printf("Average TTFB:      %dms\n", averageTTFB)
}
