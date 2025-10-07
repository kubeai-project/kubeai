package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kubeaiv1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/leader"
	"github.com/substratusai/kubeai/internal/loadbalancer"
	"github.com/substratusai/kubeai/internal/messenger"
	"github.com/substratusai/kubeai/internal/modelautoscaler"
	"github.com/substratusai/kubeai/internal/modelclient"
	"github.com/substratusai/kubeai/internal/modelcontroller"
	"github.com/substratusai/kubeai/internal/modelproxy"
	"github.com/substratusai/kubeai/internal/openaiserver"
	"github.com/substratusai/kubeai/internal/vllmclient"

	// Pulling in these packages will register the gocloud implementations.
	_ "gocloud.dev/pubsub/awssnssqs"
	_ "gocloud.dev/pubsub/azuresb"
	_ "gocloud.dev/pubsub/gcppubsub"
	_ "gocloud.dev/pubsub/kafkapubsub"
	_ "gocloud.dev/pubsub/natspubsub"
	_ "gocloud.dev/pubsub/rabbitpubsub"

	// +kubebuilder:scaffold:imports

	"github.com/substratusai/kubeai/internal/config"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var (
	Log    = ctrl.Log.WithName("manager")
	Scheme = runtime.NewScheme()
)

func init() {
	// AddToScheme in init() to allow tests to use the same Scheme before calling Run().
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(kubeaiv1.AddToScheme(Scheme))

}

// Run starts all components of the system and blocks until they complete.
// The context is used to signal the system to stop.
// Returns an error if setup fails.
// Exits the program if any of the components stop with an error.
func Run(ctx context.Context, k8sCfg *rest.Config, cfg config.System) error {
	defer func() {
		Log.Info("run finished")
	}()
	if err := cfg.DefaultAndValidate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Set up OpenTelemetry.
	otelShutdown, err := setupOTelSDK(ctx)
	if err != nil {
		return err
	}
	// Handle shutdown properly so nothing leaks.
	defer func() {
		if err = errors.Join(err, otelShutdown(context.Background())); err != nil {
			Log.Error(err, "error shutting down OpenTelemetry")
		}
	}()

	namespace, found := os.LookupEnv("POD_NAMESPACE")
	if !found {
		return errors.New("POD_NAMESPACE not set")
	}

	{
		cfgYaml, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("unable to marshal config: %w", err)
		}
		Log.Info("loaded config", "config", string(cfgYaml))
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	//disableHTTP2 := func(c *tls.Config) {
	//	Log.Info("disabling http/2")
	//	c.NextProtos = []string{"http/1.1"}
	//}

	//if !enableHTTP2 {
	//	tlsOpts = append(tlsOpts, disableHTTP2)
	//}

	//webhookServer := webhook.NewServer(webhook.Options{
	//	TLSOpts: tlsOpts,
	//})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		// Setting to "0" disables the metrics server.
		// It would be good to re-enable these controller-runtime metrics
		// once opentelemetry is integrated.
		BindAddress:   "0", // cfg.MetricsAddr,
		SecureServing: false,
	}

	SkipNameValidation := true
	mgr, err := ctrl.NewManager(k8sCfg, ctrl.Options{
		Scheme:  Scheme,
		Metrics: metricsServerOptions,
		//WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.HealthAddress,
		// TODO: Consolidate controller and autoscaler leader election.
		LeaderElection:          true,
		LeaderElectionID:        "cc6bca10.substratus.ai",
		LeaderElectionNamespace: namespace,
		LeaseDuration:           ptr.To(cfg.LeaderElection.LeaseDuration.Duration),
		RenewDeadline:           ptr.To(cfg.LeaderElection.RenewDeadline.Duration),
		RetryPeriod:             ptr.To(cfg.LeaderElection.RetryPeriod.Duration),
		Controller:              controllerconfig.Controller{SkipNameValidation: &SkipNameValidation},
		Cache: cache.Options{
			Scheme: Scheme, //mgr.GetScheme(),
			DefaultNamespaces: map[string]cache.Config{
				// Restrict operations to this Namespace.
				// (this should also be enforced by Namespaced RBAC rules)
				namespace: {},
			},
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("unable to create clientset: %w", err)
	}

	podRESTClient, err := apiutil.RESTClientForGVK(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}, false, false, mgr.GetConfig(), serializer.NewCodecFactory(mgr.GetScheme()), mgr.GetHTTPClient())
	if err != nil {
		return fmt.Errorf("unable to create pod REST client: %w", err)
	}

	// Create a new client for the autoscaler to use. This client will not use
	// a cache and will be ready to use immediately.
	k8sClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		return fmt.Errorf("unable to create client: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %w", err)
	}
	leaderElection := leader.NewElection(clientset, hostname, namespace,
		cfg.LeaderElection.LeaseDuration.Duration,
		cfg.LeaderElection.RenewDeadline.Duration,
		cfg.LeaderElection.RetryPeriod.Duration,
	)

	loadBalancer, err := loadbalancer.New(mgr)
	if err != nil {
		return fmt.Errorf("unable to setup model resolver: %w", err)
	}

	modelReconciler := &modelcontroller.ModelReconciler{
		Client:                  mgr.GetClient(),
		RESTConfig:              mgr.GetConfig(),
		PodRESTClient:           podRESTClient,
		Scheme:                  mgr.GetScheme(),
		Namespace:               namespace,
		AllowPodAddressOverride: cfg.AllowPodAddressOverride,
		SecretNames:             cfg.SecretNames,
		ResourceProfiles:        cfg.ResourceProfiles,
		CacheProfiles:           cfg.CacheProfiles,
		ModelServers:            cfg.ModelServers,
		ModelServerPods:         cfg.ModelServerPods,
		ModelLoaders:            cfg.ModelLoading,
		ModelRollouts:           cfg.ModelRollouts,
		VLLMClient: &vllmclient.Client{
			HTTPClient: &http.Client{Timeout: 10 * time.Second},
		},
	}
	if err = modelReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create Model controller: %w", err)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	modelClient := modelclient.NewModelClient(mgr.GetClient(), namespace)

	metricsPort, err := parsePortFromAddr(cfg.MetricsAddr)
	if err != nil {
		return fmt.Errorf("unable to parse metrics port: %w", err)
	}

	modelAutoscaler, err := modelautoscaler.New(
		ctx,
		k8sClient,
		leaderElection,
		modelClient,
		loadBalancer,
		cfg.ModelAutoscaling,
		metricsPort,
		types.NamespacedName{Name: cfg.ModelAutoscaling.StateConfigMapName, Namespace: namespace},
		cfg.FixedSelfMetricAddrs,
	)
	if err != nil {
		return fmt.Errorf("unable to create model autoscaler: %w", err)
	}

	modelProxy := modelproxy.NewHandler(modelClient, loadBalancer, 3, nil)
	openaiHandler := openaiserver.NewHandler(mgr.GetClient(), modelProxy)
	mux := http.NewServeMux()
	mux.Handle("/openai/", openaiHandler)
	apiServer := &http.Server{
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		Addr:        ":8000",
		Handler:     mux,
	}

	metricsMux := http.NewServeMux()
	metricsServer := &http.Server{
		Addr:    cfg.MetricsAddr,
		Handler: metricsMux,
	}
	metricsMux.Handle("/metrics", promhttp.Handler())

	httpClient := &http.Client{}

	var msgrs []*messenger.Messenger
	for i, stream := range cfg.Messaging.Streams {
		msgr, err := messenger.NewMessenger(
			ctx,
			stream.RequestsURL,
			stream.ResponsesURL,
			stream.MaxHandlers,
			cfg.Messaging.ErrorMaxBackoff.Duration,
			modelClient,
			loadBalancer,
			httpClient,
		)
		if err != nil {
			return fmt.Errorf("unable to create messenger[%v]: %w", i, err)
		}
		msgrs = append(msgrs, msgr)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer func() {
			Log.Info("autoscaler stopped")
			wg.Done()
		}()
		modelAutoscaler.Start(ctx)
	}()

	wg.Add(1)
	go func() {
		defer func() {
			Log.Info("api server stopped")
			wg.Done()
		}()
		Log.Info("starting api server", "addr", apiServer.Addr)
		if err := apiServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				Log.Info("api server closed")
			} else {
				Log.Error(err, "error serving api server")
				os.Exit(1)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer func() {
			Log.Info("metrics server stopped")
			wg.Done()
		}()
		Log.Info("starting metrics server", "addr", metricsServer.Addr)
		if err := metricsServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				Log.Info("metrics server closed")
			} else {
				Log.Error(err, "error serving metrics server")
				os.Exit(1)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer func() {
			Log.Info("leader election stopped")
			wg.Done()
		}()
		Log.Info("starting leader election")
		err := leaderElection.Start(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				Log.Info("context cancelled while running leader election")
			} else {
				Log.Error(err, "starting leader election")
				os.Exit(1)
			}
		}
	}()
	for i := range msgrs {
		wg.Add(1)
		go func() {
			defer func() {
				Log.Info("messenger stopped", "index", i)
				wg.Done()
			}()
			Log.Info("Starting messenger", "index", i)
			err := msgrs[i].Start(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					Log.Info("context cancelled while running manager")
				} else {
					Log.Error(err, "starting messenger")
					os.Exit(1)
				}
			}
		}()
	}

	Log.Info("starting controller-manager")
	wg.Add(1)
	go func() {
		defer func() {
			Log.Info("controller-manager stopped")
			wg.Done()
		}()
		if err := mgr.Start(ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				Log.Error(err, "error running controller-manager")
				os.Exit(1)
			}
		}
		apiServer.Shutdown(context.Background())
		metricsServer.Shutdown(context.Background())
	}()

	Log.Info("run launched all goroutines")
	wg.Wait()
	Log.Info("run goroutines finished")

	return nil
}

// parsePortFromAddr takes a string like ":8080" and returns 8080.
func parsePortFromAddr(addr string) (int, error) {
	if addr == "" {
		return 0, errors.New("empty address")
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("unable to parse port from address: %w", err)
	}
	return strconv.Atoi(port)
}
