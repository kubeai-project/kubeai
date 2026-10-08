package v1

import (
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/stretchr/testify/require"
)

func TestMessagesPrefix(t *testing.T) {
	for _, tc := range []struct {
		name, body, prefix string
		n                  int
	}{
		{"string", `{"model":"chat","messages":[{"role":"assistant","content":"skip"},{"role":"user","content":"héllo world"}]}`, "héllo", 5},
		{"blocks", `{"model":"chat","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"abc"}},{"type":"text","text":"你好"},{"type":"tool_result","content":"skip"},{"type":"text","text":"hello"}]}]}`, "你好he", 4},
		{"no user", `{"model":"chat","messages":[{"role":"assistant","content":"skip"}]}`, "", 5},
		{"missing", `{"model":"chat"}`, "", 5},
		{"invalid content", `{"model":"chat","messages":[{"role":"user","content":123}]}`, "", 5},
		{"zero", `{"model":"chat","messages":[{"role":"user","content":"hello"}]}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request MessagesRequest
			require.NoError(t, json.Unmarshal([]byte(tc.body), &request))
			require.Equal(t, tc.prefix, request.Prefix(tc.n))
		})
	}
}
