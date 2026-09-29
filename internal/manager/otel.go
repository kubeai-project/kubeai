package manager

import (
	"context"
	"errors"

	"github.com/kubeai-project/kubeai/internal/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
)

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func setupOTelSDK(ctx context.Context) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	//tracerProvider, err := newTraceProvider()
	//if err != nil {
	//	handleErr(err)
	//	return
	//}
	//shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	//otel.SetTracerProvider(tracerProvider)

	// Set up meter provider.
	meterProvider, err := newMeterProvider()
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	if initErr := metrics.Init(otel.Meter(metrics.MeterName)); initErr != nil {
		handleErr(initErr)
		return
	}

	// Set up logger provider.
	//loggerProvider, err := newLoggerProvider()
	//if err != nil {
	//	handleErr(err)
	//	return
	//}
	//shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	//global.SetLoggerProvider(loggerProvider)

	return
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

//func newTraceProvider() (*trace.TracerProvider, error) {
//	traceExporter, err := stdouttrace.New(
//		stdouttrace.WithPrettyPrint())
//	if err != nil {
//		return nil, err
//	}
//
//	traceProvider := trace.NewTracerProvider(
//		trace.WithBatcher(traceExporter,
//			// Default is 5s. Set to 1s for demonstrative purposes.
//			trace.WithBatchTimeout(time.Second)),
//	)
//	return traceProvider, nil
//}

func newMeterProvider() (*metric.MeterProvider, error) {
	//stdoutExporter, err := stdoutmetric.New()
	//if err != nil {
	//	return nil, err
	//}

	promExporter, err := prometheus.New()
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(promExporter),
		// Autoscaling requires per-model request counts. Overflow aggregation
		// drops the model attribute, so preserve the pre-1.44 unlimited cardinality.
		metric.WithCardinalityLimit(0),
		//metric.WithReader(metric.NewPeriodicReader(stdoutExporter,
		//	// Default is 1m. Set to 3s for demonstrative purposes.
		//	metric.WithInterval(3*time.Second))),
	)
	return meterProvider, nil
}

//func newLoggerProvider() (*log.LoggerProvider, error) {
//	logExporter, err := stdoutlog.New()
//	if err != nil {
//		return nil, err
//	}
//
//	loggerProvider := log.NewLoggerProvider(
//		log.WithProcessor(log.NewBatchProcessor(logExporter)),
//	)
//	return loggerProvider, nil
//}
