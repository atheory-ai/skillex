package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	brokertelemetry "github.com/atheory-ai/skillex/internal/telemetry"
	"github.com/atheory-ai/skillex/internal/trust"
	"github.com/spf13/cobra"
)

type telemetrySummary struct {
	Events       int            `json:"events"`
	ByOperation  map[string]int `json:"by_operation"`
	ByOutcome    map[string]int `json:"by_outcome"`
	ByCapability map[string]int `json:"by_capability"`
}

func newTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "telemetry", Short: "Inspect privacy-safe local MCP usage telemetry"}
	cmd.AddCommand(&cobra.Command{
		Use: "summary", Short: "Summarize local broker usage without arguments, results, or credentials",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			trusted, _, err := trust.LoadConfigured()
			if err != nil {
				return err
			}
			if trusted.Telemetry == nil || !trusted.Telemetry.Enabled {
				return fmt.Errorf("local MCP telemetry is disabled in trusted configuration")
			}
			summary, err := readTelemetrySummary(trusted.Telemetry.Path)
			if err != nil {
				return err
			}
			if flagJSON {
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(summary)
			}
			fmt.Printf("%d events\n", summary.Events)
			printCountMap("operation", summary.ByOperation)
			printCountMap("outcome", summary.ByOutcome)
			printCountMap("capability", summary.ByCapability)
			return nil
		},
	})
	return cmd
}

func readTelemetrySummary(path string) (telemetrySummary, error) {
	file, err := os.Open(path)
	if err != nil {
		return telemetrySummary{}, err
	}
	defer file.Close()
	result := telemetrySummary{ByOperation: map[string]int{}, ByOutcome: map[string]int{}, ByCapability: map[string]int{}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var event brokertelemetry.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return telemetrySummary{}, fmt.Errorf("invalid local telemetry event: %w", err)
		}
		result.Events++
		result.ByOperation[event.Operation]++
		result.ByOutcome[event.Outcome]++
		if event.Server != "" && event.Capability != "" {
			result.ByCapability[event.Server+"/"+event.Capability]++
		}
	}
	return result, scanner.Err()
}

func printCountMap(label string, values map[string]int) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("%s %s: %d\n", label, key, values[key])
	}
}
