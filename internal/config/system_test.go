package config_test

import (
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
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
