package config_test

import (
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAutoscalingConfig(t *testing.T) {
	cases := []struct {
		name                                  string
		cfg                                   config.ModelAutoscaling
		scaleDownDelaySeconds                 int64
		expectedRequiredConsecutiveScaleDowns int
		expectedAverageWindowCount            int
	}{
		{
			name: "default",
			cfg: config.ModelAutoscaling{
				Interval:   config.Duration{Duration: 10 * time.Second},
				TimeWindow: config.Duration{Duration: 10 * time.Minute},
			},
			scaleDownDelaySeconds:                 30,
			expectedRequiredConsecutiveScaleDowns: 3,
			// 10 * 60 / 10
			expectedAverageWindowCount: 60,
		},
		{
			name: "even",
			cfg: config.ModelAutoscaling{
				Interval:   config.Duration{Duration: 1 * time.Second},
				TimeWindow: config.Duration{Duration: 10 * time.Second},
			},
			scaleDownDelaySeconds:                 10,
			expectedRequiredConsecutiveScaleDowns: 10,
			expectedAverageWindowCount:            10,
		},
		{
			name: "with-remainder",
			cfg: config.ModelAutoscaling{
				Interval:   config.Duration{Duration: 2 * time.Second},
				TimeWindow: config.Duration{Duration: 5 * time.Second},
			},
			scaleDownDelaySeconds:                 3,
			expectedRequiredConsecutiveScaleDowns: 2,
			expectedAverageWindowCount:            3,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.expectedRequiredConsecutiveScaleDowns, c.cfg.RequiredConsecutiveScaleDowns(c.scaleDownDelaySeconds))
		})
	}
}

func TestCacheSharedFilesystemStorageSize(t *testing.T) {
	cases := []struct {
		name                string
		storageSize         string
		expectedStorageSize string
	}{
		{
			name:                "custom storage size",
			storageSize:         "50Gi",
			expectedStorageSize: "50Gi",
		},
		{
			name:                "empty defaults to 10Gi",
			storageSize:         "",
			expectedStorageSize: "10Gi",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sys := config.System{
				CacheProfiles: map[string]config.CacheProfile{
					"test": {
						SharedFilesystem: &config.CacheSharedFilesystem{
							StorageClassName: "test-storage-class",
							StorageSize:      c.storageSize,
						},
					},
				},
			}
			// Ignore validation error, we only check defaulting logic.
			_ = sys.DefaultAndValidate()
			require.Equal(t, c.expectedStorageSize, sys.CacheProfiles["test"].SharedFilesystem.StorageSize)
		})
	}
}

func TestProxyMode(t *testing.T) {
	cases := []struct {
		name        string
		proxyMode   config.ProxyMode
		expectError bool
		expected    config.ProxyMode
	}{
		{
			name:        "default",
			proxyMode:   "",
			expectError: false,
			expected:    config.ProxyModeInternal,
		},
		{
			name:        "internal",
			proxyMode:   config.ProxyModeInternal,
			expectError: false,
			expected:    config.ProxyModeInternal,
		},
		{
			name:        "external",
			proxyMode:   config.ProxyModeExternal,
			expectError: false,
			expected:    config.ProxyModeExternal,
		},
		{
			name:        "invalid",
			proxyMode:   "invalid",
			expectError: true,
			expected:    "invalid", // keeps it as invalid if it fails validation
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sys := config.System{
				MetricsAddr:   ":8080",
				HealthAddress: ":8081",
				SecretNames: config.SecretNames{
					Alibaba:     "alibaba",
					AWS:         "aws",
					GCP:         "gcp",
					Huggingface: "hf",
				},
				ModelServers: config.ModelServers{
					VLLM: config.ModelServer{
						Images: map[string]string{
							"default": "vllm:latest",
						},
					},
				},
				ModelLoading: config.ModelLoading{Image: "loader:latest"},
				ResourceProfiles: map[string]config.ResourceProfile{
					"cpu": {},
				},
				ModelAutoscaling: config.ModelAutoscaling{
					StateConfigMapName: "test-state",
				},
				Proxy: config.Proxy{
					Mode: c.proxyMode,
				},
			}

			err := sys.DefaultAndValidate()
			if c.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, c.expected, sys.Proxy.Mode)
			}
		})
	}
}

func sysWithDRAProfile(dra config.DRAConfig) config.System {
	return config.System{
		MetricsAddr:   ":8080",
		HealthAddress: ":8081",
		SecretNames: config.SecretNames{
			Alibaba: "alibaba", AWS: "aws", GCP: "gcp", Huggingface: "hf",
		},
		ModelServers: config.ModelServers{
			VLLM: config.ModelServer{Images: map[string]string{"default": "vllm:latest"}},
		},
		ModelLoading:     config.ModelLoading{Image: "loader:latest"},
		ModelAutoscaling: config.ModelAutoscaling{StateConfigMapName: "state"},
		Proxy:            config.Proxy{Mode: config.ProxyModeInternal},
		ResourceProfiles: map[string]config.ResourceProfile{
			"dra-profile": {DRA: &dra},
		},
	}
}

func TestDRAConfig(t *testing.T) {
	t.Parallel()

	t.Run("template path applies defaults", func(t *testing.T) {
		t.Parallel()
		sys := sysWithDRAProfile(config.DRAConfig{
			ResourceClaimTemplateName: "nvidia-h100-exclusive",
		})
		require.NoError(t, sys.DefaultAndValidate())
		dra := sys.ResourceProfiles["dra-profile"].DRA
		require.Equal(t, "gpu-claim", dra.ClaimName)
		require.Equal(t, "gpu", dra.ClaimRequest)
	})

	t.Run("shared path applies defaults", func(t *testing.T) {
		t.Parallel()
		sys := sysWithDRAProfile(config.DRAConfig{
			ResourceClaimName: "nvidia-mps-shared",
		})
		require.NoError(t, sys.DefaultAndValidate())
		dra := sys.ResourceProfiles["dra-profile"].DRA
		require.Equal(t, "gpu-claim", dra.ClaimName)
		require.Equal(t, "gpu", dra.ClaimRequest)
	})

	t.Run("explicit claimName and claimRequest not overwritten", func(t *testing.T) {
		t.Parallel()
		sys := sysWithDRAProfile(config.DRAConfig{
			ResourceClaimTemplateName: "my-template",
			ClaimName:                 "my-claim",
			ClaimRequest:              "my-request",
		})
		require.NoError(t, sys.DefaultAndValidate())
		dra := sys.ResourceProfiles["dra-profile"].DRA
		require.Equal(t, "my-claim", dra.ClaimName)
		require.Equal(t, "my-request", dra.ClaimRequest)
	})

	t.Run("both resourceClaimTemplateName and resourceClaimName returns error", func(t *testing.T) {
		t.Parallel()
		sys := sysWithDRAProfile(config.DRAConfig{
			ResourceClaimTemplateName: "my-template",
			ResourceClaimName:         "my-claim",
		})
		err := sys.DefaultAndValidate()
		require.ErrorContains(t, err, "mutually exclusive")
	})

	t.Run("neither resourceClaimTemplateName nor resourceClaimName returns error", func(t *testing.T) {
		t.Parallel()
		sys := sysWithDRAProfile(config.DRAConfig{})
		err := sys.DefaultAndValidate()
		require.ErrorContains(t, err, "requires either")
	})

	t.Run("dra with GPU device plugin limit returns error", func(t *testing.T) {
		t.Parallel()
		sys := sysWithDRAProfile(config.DRAConfig{
			ResourceClaimTemplateName: "nvidia-h100-exclusive",
		})
		sys.ResourceProfiles["dra-profile"] = config.ResourceProfile{
			DRA: sys.ResourceProfiles["dra-profile"].DRA,
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
		}
		err := sys.DefaultAndValidate()
		require.ErrorContains(t, err, "mutually exclusive with GPU")
	})
}
