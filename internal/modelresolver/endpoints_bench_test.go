package modelresolver

import (
	"context"
	"testing"
)

func BenchmarkEndpointGroup(b *testing.B) {
	e := newEndpointGroup()
	e.setAddrs(map[string]struct{}{"10.0.0.1": {}})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, f, err := e.getBestAddr(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			f()
		}
	})
}
