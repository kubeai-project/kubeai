package manager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"sigs.k8s.io/yaml"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	kubeaiv1 "github.com/substratusai/kubeai/api/v1"
	"github.com/substratusai/kubeai/internal/leader"
	"github.com/substratusai/kubeai/internal/messenger"
	"github.com/substratusai/kubeai/internal/modelautoscaler"
	"github.com/substratusai/kubeai/internal/modelcontroller"
	"github.com/substratusai/kubeai/internal/modelproxy"
	"github.com/substratusai/kubeai/internal/modelresolver"
	"github.com/substratusai/kubeai/internal/modelscaler"
	"github.com/substratusai/kubeai/internal/openaiserver"

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
		BindAddress:   cfg.MetricsAddr,
		SecureServing: false,
	}

	mgr, err := ctrl.NewManager(k8sCfg, ctrl.Options{
		Scheme:  Scheme,
		Metrics: metricsServerOptions,
		//WebhookServer:          webhookServer,
		HealthProbeBindAddress: cfg.HealthAddress,
		// TODO: Consolidate controller and autoscaler leader election.
		LeaderElection:          true,
		LeaderElectionID:        "cc6bca10.substratus.ai",
		LeaderElectionNamespace: namespace,
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

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("unable to get hostname: %w", err)
	}
	leaderElection := leader.NewElection(clientset, hostname, namespace)

	modelResolver, err := modelresolver.NewManager(mgr)
	if err != nil {
		return fmt.Errorf("unable to setup model resolver: %w", err)
	}

	modelReconciler := &modelcontroller.ModelReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		Namespace:               namespace,
		AllowPodAddressOverride: cfg.AllowPodAddressOverride,
		HuggingfaceSecretName:   cfg.SecretNames.Huggingface,
		ResourceProfiles:        cfg.ResourceProfiles,
		ModelServers:            cfg.ModelServers,
		ModelServerPods:         cfg.ModelServerPods,
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

	modelScaler := modelscaler.NewModelScaler(mgr.GetClient(), namespace)

	modelAutoscaler := modelautoscaler.New(
		leaderElection,
		modelScaler,
		modelResolver,
		cfg.ModelAutoscaling,
	)

	modelProxy := modelproxy.NewHandler(modelScaler, modelResolver, 3, nil)
	openaiHandler := openaiserver.NewHandler(mgr.GetClient(), modelProxy)
	mux := http.NewServeMux()
	mux.Handle("/openai/", openaiHandler)
	apiServer := &http.Server{Addr: ":8000", Handler: mux}

	httpClient := &http.Client{}

	var msgrs []*messenger.Messenger
	for i, stream := range cfg.Messaging.Streams {
		msgr, err := messenger.NewMessenger(
			ctx,
			stream.RequestsURL,
			stream.ResponsesURL,
			stream.MaxHandlers,
			cfg.Messaging.ErrorMaxBackoff.Duration,
			modelScaler,
			modelResolver,
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
	}()

	Log.Info("run launched all goroutines")
	wg.Wait()
	Log.Info("run goroutines finished")

	return nil
}
