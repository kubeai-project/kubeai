package integration

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/kubeai-project/kubeai/internal/manager"
	"github.com/stretchr/testify/require"
	"gocloud.dev/pubsub"
	_ "gocloud.dev/pubsub/mempubsub"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// General //

var (
	testEnv       *envtest.Environment
	testK8sClient client.Client
	testK8sConfig *rest.Config
	testCtx       context.Context
	testCancel    context.CancelFunc
	testNS        = "default"
	// testHTTPClient is a client with a long timeout for use in tests
	// where requests may be held for long periods of time on purpose.
	testHTTPClient  = &http.Client{Timeout: 5 * time.Minute}
	cpuRuntimeClass = nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: cpuRuntimeClassName,
		},
		Handler: "my-cpu-runtime-handler",
	}
)

// Messenger //

var (
	testRequestsTopic         *pubsub.Topic
	testResponsesSubscription *pubsub.Subscription
)

const (
	memRequestsURL  = "mem://requests"
	memResponsesURL = "mem://responses"
)

// Config //

const (
	resourceProfileCPU       = "cpu"
	resourceProfileNvidiaGPU = "nvidia-gpu-l4"
	testVLLMDefualtImage     = "default-vllm-image:v1.2.3"
	testVLLMCPUImage         = "cpu-vllm-image:v1.2.3"
	cpuRuntimeClassName      = "my-cpu-runtime-class"
	cpuSchedulerName         = "my-cpu-scheduler"
)

// TestMain performs setup and teardown for integration tests - i.e. all Test*()
// functions in this package.
func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	testCtx, testCancel = context.WithCancel(ctrl.SetupSignalHandler())

	// Setup Kubernetes environment.
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../manifests/crds"},
		ErrorIfCRDPathMissing: true,
	}
	var err error
	testK8sConfig, err = testEnv.Start()
	requireNoError(err)

	testK8sClient, err = client.New(testK8sConfig, client.Options{Scheme: manager.Scheme})
	requireNoError(err)

	err = installCommonResources()
	requireNoError(err)

	// Configure the manager.
	os.Setenv("POD_NAMESPACE", testNS)

	// Test Cases //

	log.Println("running tests")
	code := m.Run()

	// Teardown //

	// TODO: Run cleanup on ctrl-C, etc.
	log.Println("cancelling main test context")
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

func installCommonResources() error {
	if err := testK8sClient.Create(testCtx, &cpuRuntimeClass); err != nil {
		return err
	}
	return nil
}

// initTest initializes the manager with the given System configuration
// and shuts it down when the test is done.
// SHOULD BE CALLED AT THE BEGINNING OF EACH TEST CASE.
func initTest(t *testing.T, cfg config.System) {
	autoscalerStateConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.ModelAutoscaling.StateConfigMapName,
			Namespace: testNS,
		},
	}
	require.NoError(t, testK8sClient.Create(testCtx, &autoscalerStateConfigMap))

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(testCtx)
	t.Cleanup(func() {
		cancel()
		t.Logf("Waiting for manager to stop")
		wg.Wait()
		t.Logf("Manager stopped")
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := manager.Run(ctx, testK8sConfig, cfg); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal(err)
		}
	}()
}

// baseSysCfg returns the System configuration for testing.
// A function is used to avoid test cases accidentally modifying a global configuration variable
// which would be tricky to debug.
func baseSysCfg(t *testing.T) config.System {
	return config.System{
		MetricsAddr:   "127.0.0.1:8080",
		HealthAddress: "127.0.0.1:8081",
		SecretNames: config.SecretNames{
			Huggingface: "huggingface",
			AWS:         "aws",
			GCP:         "gcp",
			Alibaba:     "alibaba",
		},
		ModelServers: config.ModelServers{
			VLLM: config.ModelServer{
				Images: map[string]string{
					"default":  testVLLMDefualtImage,
					"cpu-only": testVLLMCPUImage,
				},
			},
		},
		ModelLoading: config.ModelLoading{
			Image: "model-loader",
		},
		ModelServerPods: config.ModelServerPods{
			ImagePullSecrets: []corev1.LocalObjectReference{
				{Name: "test-secret1"},
				{Name: "test-secret2"},
			},
			ModelServiceAccountName: "test-service-account",
			ModelPodSecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(true),
			},
			ModelContainerSecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr.To(false),
			},
		},
		ResourceProfiles: map[string]config.ResourceProfile{
			resourceProfileCPU: {
				Requests: corev1.ResourceList{
					"cpu":    resource.MustParse("1"),
					"memory": resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					"memory": resource.MustParse("4Gi"),
				},
				NodeSelector: map[string]string{
					"compute-type": "cpu",
				},
				ImageName: "cpu-only",
				Tolerations: []corev1.Toleration{
					{
						Key:      "some-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				SchedulerName:    cpuSchedulerName,
				RuntimeClassName: ptr.To(cpuRuntimeClassName),
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "my-affinity-key",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"my-affinity-val"},
										},
									},
								},
							},
						},
					},
				},
			},
			resourceProfileNvidiaGPU: {
				Requests: corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
				Limits: corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
				NodeSelector: map[string]string{
					"compute-type": "gpu",
				},
			},
		},
		ModelAutoscaling: config.ModelAutoscaling{
			StateConfigMapName: strings.ToLower(t.Name()) + "-autoscaler-state",
		},
		LeaderElection: config.LeaderElection{
			// Speed up the election process for tests.
			// This is important because the manager is restarted for each test case.
			LeaseDuration: config.Duration{Duration: 1 * time.Second},
			RenewDeadline: config.Duration{Duration: time.Second / 2},
			RetryPeriod:   config.Duration{Duration: time.Second / 10},
		},
		AllowPodAddressOverride: true,
		// FixedSelfIPs is used to tell the autoscaler to scrape metrics from the local host
		// (because KubeAI is not running in a Pod).
		FixedSelfMetricAddrs: []string{"127.0.0.1:8080"},
	}
}
