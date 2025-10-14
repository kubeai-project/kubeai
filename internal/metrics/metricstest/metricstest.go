package metricstest

import (
	"context"
	"testing"

	"github.com/kubeai-project/kubeai/internal/metrics"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
)

var (
	testReader metric.Reader
)

// Init should be called at the beginning of a test to wire up all global metrics
// to a test reader. Test case should not be running in parallel with any other
// part of the program that interacts with metrics.
func Init(t *testing.T) {
	testReader = metric.NewManualReader()
	mp := metric.NewMeterProvider(
		metric.WithReader(testReader),
	)
	require.NoError(t, metrics.Init(mp.Meter(metrics.MeterName)))
}

// Collect metrics from test reader.
func Collect(t *testing.T) metricdata.ResourceMetrics {
	mets := metricdata.ResourceMetrics{}
	require.NoError(t, testReader.Collect(context.Background(), &mets))
	return mets
}

func RequireActiveRequestsMetric(t *testing.T, mets metricdata.ResourceMetrics, model string, val int64) {
	met := requireMetricExists(t, mets, metrics.MeterName, metrics.InferenceRequestsActiveMetricName)
	metricdatatest.AssertAggregationsEqual(t,
		metricdata.Sum[int64]{
			Temporality: metricdata.CumulativeTemporality,
			IsMonotonic: false,
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: attribute.NewSet(
						metrics.AttrRequestModel.String(model),
						metrics.AttrRequestType.String(metrics.AttrRequestTypeHTTP),
					),
					Value: val,
				},
			},
		},
		met.Data,
		metricdatatest.IgnoreExemplars(),
		metricdatatest.IgnoreTimestamp(),
	)
}

func requireMetricExists(t *testing.T, mets metricdata.ResourceMetrics, scope, name string) metricdata.Metrics {
	for _, sm := range mets.ScopeMetrics {
		if sm.Scope.Name == scope {
			for _, m := range sm.Metrics {
				if m.Name == name {
					return m
				}
			}
		}
	}
	t.Fatalf("metric %q not found in scope %q", name, scope)
	return metricdata.Metrics{}
}
