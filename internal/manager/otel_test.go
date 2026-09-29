package manager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kubeai-project/kubeai/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func TestMeterProviderPreservesActiveRequestsAtHighCardinality(t *testing.T) {
	// Exercise the SDK default independently of the environment running the test.
	t.Setenv("OTEL_GO_X_CARDINALITY_LIMIT", "")
	registry := prometheus.NewRegistry()
	previousRegisterer := prometheus.DefaultRegisterer
	prometheus.DefaultRegisterer = registry
	t.Cleanup(func() { prometheus.DefaultRegisterer = previousRegisterer })

	provider, err := newMeterProvider()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	counter, err := provider.Meter(metrics.MeterName).Int64UpDownCounter(metrics.InferenceRequestsActiveMetricName)
	require.NoError(t, err)

	// Exceed the 2,000-series default introduced in OpenTelemetry SDK 1.44.
	const modelCount = 2005
	options := make([]metric.MeasurementOption, modelCount)
	for i := range options {
		options[i] = metric.WithAttributes(
			metrics.AttrRequestModel.String(fmt.Sprintf("model-%d", i)),
			metrics.AttrRequestType.String(metrics.AttrRequestTypeHTTP),
		)
		counter.Add(context.Background(), 2, options[i])
	}

	assertCounts := func(want float64) {
		t.Helper()
		response := httptest.NewRecorder()
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, response.Code)
		parser := expfmt.NewTextParser(model.UTF8Validation)
		families, err := parser.TextToMetricFamilies(response.Body)
		require.NoError(t, err)
		counts := make(map[string]float64)
		for _, family := range families {
			if family.GetName() != metrics.OtelNameToPromName(metrics.InferenceRequestsActiveMetricName) {
				continue
			}
			for _, sample := range family.Metric {
				var model string
				for _, label := range sample.Label {
					require.NotEqual(t, metrics.OtelAttrToPromLabel(attribute.Key("otel.metric.overflow")), label.GetName(), "overflow hides the model from the autoscaler")
					if label.GetName() == metrics.OtelAttrToPromLabel(metrics.AttrRequestModel) {
						model = label.GetValue()
					}
				}
				require.NotEmpty(t, model)
				require.NotNil(t, sample.Gauge)
				counts[model] = sample.GetGauge().GetValue()
			}
		}
		require.Len(t, counts, modelCount)
		for i := range options {
			require.Equal(t, want, counts[fmt.Sprintf("model-%d", i)])
		}
	}

	assertCounts(2)
	for _, option := range options {
		counter.Add(context.Background(), -1, option)
	}
	assertCounts(1)
}
