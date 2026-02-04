package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
)

func main() {
	builder := dashboard.NewDashboardBuilder("Lectures Notifier").
		Uid("lectures-notifier").
		Tags([]string{"lectures", "notifier", "prometheus"}).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone(common.TimeZoneBrowser)

	statReduce := common.NewReduceDataOptionsBuilder().Calcs([]string{"lastNotNull"})

	builder = builder.WithRow(dashboard.NewRowBuilder("Last 5 minutes"))

	builder = builder.WithPanel(
		stat.NewPanelBuilder().
			Title("Executions (5m)").
			Span(6).
			Interval("5m").
			ReduceOptions(statReduce).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(increase(lectures_notifier_execution_success_total[5m])) + sum(increase(lectures_notifier_execution_failure_total[5m]))`).
					LegendFormat("executions"),
			),
		)

	builder = builder.WithPanel(
		stat.NewPanelBuilder().
			Title("Failures (5m)").
			Span(6).
			Interval("5m").
			ReduceOptions(statReduce).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(increase(lectures_notifier_execution_failure_total[5m]))`).
					LegendFormat("failures"),
			),
		)

	builder = builder.WithPanel(
		stat.NewPanelBuilder().
			Title("Notifications sent (5m)").
			Span(6).
			Interval("5m").
			ReduceOptions(statReduce).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(increase(lectures_notifier_events_notified_total[5m]))`).
					LegendFormat("notified"),
			),
		)

	builder = builder.WithPanel(
		stat.NewPanelBuilder().
			Title("Execution duration p95 (5m)").
			Span(6).
			Interval("5m").
			Unit("s").
			ReduceOptions(statReduce).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`histogram_quantile(0.95, sum(rate(lectures_notifier_execution_duration_seconds_bucket[5m])) by (le))`).
					LegendFormat("p95"),
			),
		)

	dashboardJSON, err := builder.Build()
	if err != nil {
		panic(err)
	}

	outputPath := os.Getenv("DASHBOARD_OUT")
	if outputPath == "" {
		outputPath = "dashboard.json"
	}

	payload, err := json.MarshalIndent(dashboardJSON, "", "  ")
	if err != nil {
		panic(err)
	}

	if err := os.WriteFile(outputPath, payload, 0o600); err != nil {
		panic(err)
	}

	fmt.Printf("dashboard written to %s\n", outputPath)

}
