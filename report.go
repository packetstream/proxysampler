package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v1"
)


// displayReport outputs a report of proxy performance & health
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
	fmt.Println(fmt.Sprintf("Success rate:      %d/%d", success, success+fail))
	fmt.Println(fmt.Sprintf("Average TTFB:      %dms", averageTTFB))
}
