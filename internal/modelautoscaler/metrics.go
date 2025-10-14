package modelautoscaler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kubeai-project/kubeai/internal/metrics"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func aggregateAllMetrics(agg *metricsAggregation, addrs []string, path string) (err error) {
	// TODO: Consider concurrently scraping metrics from all endpoints.
	for _, addr := range addrs {
		if e := scrapeAndAggregateMetrics(agg, fmt.Sprintf("http://%s%s", addr, path)); e != nil {
			err = errors.Join(err, e)
		}
	}

	return err
}

type metricsAggregation struct {
	activeRequestsByModel map[string][]int64
}

func newMetricsAggregation() *metricsAggregation {
	return &metricsAggregation{
		activeRequestsByModel: make(map[string][]int64),
	}
}

func scrapeAndAggregateMetrics(agg *metricsAggregation, url string) error {
	// Perform the HTTP GET request
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to scrape metrics: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Use the expfmt library to parse the Prometheus metrics
	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to parse metrics: %w", err)
	}

	if fam, ok := metricFamilies[metrics.OtelNameToPromName(metrics.InferenceRequestsActiveMetricName)]; ok {
		for _, m := range fam.Metric {
			for _, label := range m.Label {
				if label.GetName() == metrics.OtelAttrToPromLabel(metrics.AttrRequestModel) {
					agg.activeRequestsByModel[label.GetValue()] = append(
						agg.activeRequestsByModel[label.GetValue()],
						getMetricsValue(fam, m),
					)
				}
			}
		}
	}

	return nil
}

func getMetricsValue(mf *io_prometheus_client.MetricFamily, m *io_prometheus_client.Metric) int64 {
	if mf.GetType() == io_prometheus_client.MetricType_GAUGE && m.Gauge != nil {
		return int64(m.GetGauge().GetValue())
	} else if mf.GetType() == io_prometheus_client.MetricType_COUNTER && m.Counter != nil {
		return int64(m.GetCounter().GetValue())
	}
	return 0
}
