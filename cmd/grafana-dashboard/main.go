package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

func main() {
	builder := dashboard.NewDashboardBuilder("Lectures Notifier").
		Uid("lectures-notifier").
		Tags([]string{"lectures", "notifier", "prometheus"}).
		Refresh("1m").
		Time("now-6h", "now").
		Timezone(common.TimeZoneBrowser)

	builder = builder.WithRow(dashboard.NewRowBuilder("Executions"))
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Execution rate").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_execution_success_total[5m]))`).
					LegendFormat("success"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_execution_failure_total[5m]))`).
					LegendFormat("failure"),
			),
	)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Execution duration avg").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_execution_duration_seconds_sum[5m])) / sum(rate(lectures_notifier_execution_duration_seconds_count[5m]))`).
					LegendFormat("avg"),
			),
	)

	builder = builder.WithRow(dashboard.NewRowBuilder("Events"))
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Events processed").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_events_processed_total[5m]))`).
					LegendFormat("processed"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_events_available_total[5m]))`).
					LegendFormat("available"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_events_notified_total[5m]))`).
					LegendFormat("notified"),
			),
	)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Events deduplicated / sold out / missing start time").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_events_deduplicated_total[5m]))`).
					LegendFormat("deduplicated"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_events_sold_out_total[5m]))`).
					LegendFormat("sold_out"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_events_without_start_time_total[5m]))`).
					LegendFormat("missing_start_time"),
			),
	)

	builder = builder.WithRow(dashboard.NewRowBuilder("Integrations"))
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Eventbrite fetch duration avg").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_eventbrite_fetch_duration_seconds_sum[5m])) / sum(rate(lectures_notifier_eventbrite_fetch_duration_seconds_count[5m]))`).
					LegendFormat("avg"),
			),
	)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Ntfy publish duration avg").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_ntfy_publish_duration_seconds_sum[5m])) / sum(rate(lectures_notifier_ntfy_publish_duration_seconds_count[5m]))`).
					LegendFormat("avg"),
			),
	)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Errors").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`sum(rate(lectures_notifier_errors_total[5m]))`).
					LegendFormat("errors"),
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
