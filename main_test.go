package main

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	testNamespace  = "test"
	testK8sClient  client.Client
	testEnv        *envtest.Environment
	testCtx        context.Context
	testCancel     context.CancelFunc
	testServer     *httptest.Server
	testHTTPClient = &http.Client{Timeout: 10 * time.Second}
)

func TestMain(m *testing.M) {
	AdditionalProxyRewrite = func(r *httputil.ProxyRequest) {
		// EndpointSlices do not allow for specifying local IPs (used in mock backend)
		// so we remap the requests here.
		r.SetURL(&url.URL{
			Scheme: r.Out.URL.Scheme,
			Host:   "127.0.0.1:" + r.Out.URL.Port(),
		})
	}
	log.Println("bootstrapping test environment")
	testEnv = &envtest.Environment{}
	cfg, err := testEnv.Start()
	requireNoError(err)

	requireNoError(clientgoscheme.AddToScheme(scheme))

	testK8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	requireNoError(err)

	testCtx, testCancel = context.WithCancel(ctrl.SetupSignalHandler())

	requireNoError(testK8sClient.Create(testCtx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
	}))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	requireNoError(err)

	fifo := NewFIFOQueueManager(1, 1000)

	endpoints, err := NewEndpointsManager(mgr)
	requireNoError(err)
	endpoints.EndpointSizeCallback = fifo.UpdateQueueSize

	scaler, err := NewDeploymentManager(mgr)
	requireNoError(err)
	scaler.Namespace = testNamespace
	scaler.ScaleDownPeriod = 1 * time.Second

	autoscaler, err := NewAutoscaler(mgr)
	requireNoError(err)
	autoscaler.Interval = 1 * time.Second
	autoscaler.AverageCount = 1 // 10 * 3 seconds = 30 sec avg
	autoscaler.Scaler = scaler
	autoscaler.FIFO = fifo
	go autoscaler.Start()

	handler := &Handler{
		Deployments: scaler,
		Endpoints:   endpoints,
		FIFO:        fifo,
	}
	testServer = httptest.NewServer(handler)
	defer testServer.Close()

	go func() {
		log.Println("starting manager")
		requireNoError(mgr.Start(testCtx))
	}()

	log.Println("running tests")
	code := m.Run()

	// TODO: Run cleanup on ctrl-C, etc.
	log.Println("stopping manager")
	testCancel()
	log.Println("stopping test environment")
	requireNoError(testEnv.Stop())

	os.Exit(code)
}

func requireNoError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
