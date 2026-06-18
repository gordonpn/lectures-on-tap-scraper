package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
)

func main() {
	builder := dashboard.NewDashboardBuilder("Lectures Notifier").
		Uid("lectures-notifier").
		Tags([]string{"lectures", "notifier", "prometheus"}).
		Refresh("30s").
		Time("now-6h", "now").
		Timezone(common.TimeZoneBrowser)

	statReduce := common.NewReduceDataOptionsBuilder().Calcs([]string{"lastNotNull"})

	// Row 1: Scraper Status & Liveness
	builder = builder.WithRow(dashboard.NewRowBuilder("Scraper Liveness & Status"))

	// Panel 1: Dead-Man Switch / Liveness Lag (time() - scraper_last_success_timestamp_seconds)
	builder = builder.WithPanel(
		stat.NewPanelBuilder().
			Title("Liveness Lag (Time Since Last Success)").
			Span(6).
			Unit("s").
			ReduceOptions(statReduce).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`time() - scraper_last_success_timestamp_seconds`).
					LegendFormat("Lag (Seconds)"),
			),
	)

	// Panel 2: Last Run Status (scraper_last_run_success)
	builder = builder.WithPanel(
		stat.NewPanelBuilder().
			Title("Last Run Status").
			Span(6).
			ReduceOptions(statReduce).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`scraper_last_run_success`).
					LegendFormat("Status (1=Success, 0=Failure)"),
			),
	)

	// Row 2: Throughput per Run
	builder = builder.WithRow(dashboard.NewRowBuilder("Scraper Throughput"))

	// Panel 3: Throughput per Run (scraper_last_run_items_processed_total)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Processed vs. Available vs. Notified Events per Run").
			Span(12).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`scraper_last_run_items_processed_total`).
					LegendFormat("Processed"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`scraper_last_run_items_available_total`).
					LegendFormat("Available"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`scraper_last_run_items_notified_total`).
					LegendFormat("Notified"),
			),
	)

	// Row 3: Performance Metrics
	builder = builder.WithRow(dashboard.NewRowBuilder("Execution & API Performance"))

	// Panel 4: Execution Durations (scraper_last_run_duration_seconds & scraper_execution_duration_seconds)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("Execution Duration (Last Run & Historical p95)").
			Span(6).
			Unit("s").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`scraper_last_run_duration_seconds`).
					LegendFormat("Last Run Duration"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`histogram_quantile(0.95, sum(rate(scraper_execution_duration_seconds_bucket[5m])) by (le))`).
					LegendFormat("p95 Active Runtime"),
			),
	)

	// Panel 5: External API performance (EventBrite and Ntfy publish durations)
	builder = builder.WithPanel(
		timeseries.NewPanelBuilder().
			Title("External API Duration p95 (EventBrite & Ntfy)").
			Span(6).
			Unit("s").
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`histogram_quantile(0.95, sum(rate(scraper_last_run_eventbrite_fetch_duration_seconds_bucket[5m])) by (le))`).
					LegendFormat("EventBrite Fetch p95"),
			).
			WithTarget(
				prometheus.NewDataqueryBuilder().
					Expr(`histogram_quantile(0.95, sum(rate(scraper_last_run_ntfy_publish_duration_seconds_bucket[5m])) by (le))`).
					LegendFormat("Ntfy Publish p95"),
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
