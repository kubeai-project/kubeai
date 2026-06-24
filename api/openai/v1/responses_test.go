package v1_test

import (
	stdjson "encoding/json"
	"fmt"
	"testing"

	"github.com/go-json-experiment/json"
	v1 "github.com/kubeai-project/kubeai/api/openai/v1"
	"github.com/stretchr/testify/require"
)

func TestResponsesRequestPrefix(t *testing.T) {
	cases := []struct {
		input string
		n     int
		exp   string
	}{
		// String input
		{`{"model":"m","input":""}`, 9, ""},
		{`{"model":"m","input":"hello"}`, 0, ""},
		{`{"model":"m","input":"hello"}`, 5, "hello"},
		{`{"model":"m","input":"hello world"}`, 5, "hello"},
		{`{"model":"m","input":"世界"}`, 1, "世"},
		{`{"model":"m","input":"世界"}`, 2, "世界"},
		{`{"model":"m","input":"世界"}`, 9, "世界"},
		// Array input — user message
		{`{"model":"m","input":[{"role":"user","content":"abc"}]}`, 9, "abc"},
		{`{"model":"m","input":[{"role":"user","content":"abcdefghij"}]}`, 5, "abcde"},
		// Array input — skips system, picks first user
		{`{"model":"m","input":[{"role":"system","content":"sys"},{"role":"user","content":"usr"}]}`, 9, "usr"},
		// Array input — no user message
		{`{"model":"m","input":[{"role":"system","content":"sys"}]}`, 9, ""},
		// Array input — multipart content, picks first text part
		{fmt.Sprintf(`{"model":"m","input":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"http://x"}}]}]}`), 9, "hi"},
		// Empty array
		{`{"model":"m","input":[]}`, 9, ""},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%q n=%d", c.input, c.n), func(t *testing.T) {
			var req v1.ResponsesRequest
			require.NoError(t, json.Unmarshal([]byte(c.input), &req))
			require.Equal(t, c.exp, req.Prefix(c.n))
		})
	}
}

func TestResponsesRequest_JSON(t *testing.T) {
	cases := []struct {
		name          string
		inputJSON     string
		roundTripJSON string
		req           *v1.ResponsesRequest
	}{
		{
			name:      "string input",
			inputJSON: `{"model":"gpt-4o","input":"Tell me a joke"}`,
			req: &v1.ResponsesRequest{
				Model: "gpt-4o",
				Input: v1.ResponsesInput{String: "Tell me a joke"},
			},
		},
		{
			name:      "array input with single user message",
			inputJSON: `{"model":"gpt-4o","input":[{"role":"user","content":"Hello"}]}`,
			req: &v1.ResponsesRequest{
				Model: "gpt-4o",
				Input: v1.ResponsesInput{
					Array: []v1.ResponsesInputItem{
						{Role: "user", Content: &v1.ChatMessageContent{String: "Hello"}},
					},
				},
			},
		},
		{
			name: "array input with type field",
			inputJSON: `{"model":"gpt-4o","input":[{"type":"message","role":"user","content":"Hi"}]}`,
			req: &v1.ResponsesRequest{
				Model: "gpt-4o",
				Input: v1.ResponsesInput{
					Array: []v1.ResponsesInputItem{
						{Type: "message", Role: "user", Content: &v1.ChatMessageContent{String: "Hi"}},
					},
				},
			},
		},
		{
			name: "instructions and stream",
			inputJSON: `{"model":"gpt-4o","input":"Hello","instructions":"You are helpful","stream":true}`,
			req: &v1.ResponsesRequest{
				Model:        "gpt-4o",
				Input:        v1.ResponsesInput{String: "Hello"},
				Instructions: "You are helpful",
				Stream:       true,
			},
		},
		{
			name:      "previous_response_id",
			inputJSON: `{"model":"gpt-4o","input":"Follow up","previous_response_id":"resp_abc123"}`,
			req: &v1.ResponsesRequest{
				Model:              "gpt-4o",
				Input:              v1.ResponsesInput{String: "Follow up"},
				PreviousResponseID: "resp_abc123",
			},
		},
		{
			name:      "max_output_tokens",
			inputJSON: `{"model":"gpt-4o","input":"Hi","max_output_tokens":512}`,
			req: &v1.ResponsesRequest{
				Model:           "gpt-4o",
				Input:           v1.ResponsesInput{String: "Hi"},
				MaxOutputTokens: v1.Ptr(512),
			},
		},
		{
			name:      "extra backend field preserved",
			inputJSON: `{"model":"gpt-4o","input":"Hi","vllm_extra_param":"value"}`,
			req: &v1.ResponsesRequest{
				Model: "gpt-4o",
				Input: v1.ResponsesInput{String: "Hi"},
			},
		},
		{
			name: "multipart content in array input",
			inputJSON: `{"model":"gpt-4o","input":[{"role":"user","content":[{"type":"text","text":"What is this?"},{"type":"image_url","image_url":{"url":"https://example.com/img.jpg"}}]}]}`,
			req: &v1.ResponsesRequest{
				Model: "gpt-4o",
				Input: v1.ResponsesInput{
					Array: []v1.ResponsesInputItem{
						{
							Role: "user",
							Content: &v1.ChatMessageContent{
								Array: []v1.ChatMessageContentPart{
									{Type: "text", Text: "What is this?"},
									{Type: "image_url", ImageURL: &v1.ChatMessageImageURL{URL: "https://example.com/img.jpg"}},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multi-turn conversation",
			inputJSON: `{"model":"gpt-4o","input":[{"role":"system","content":"Be concise"},{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi!"},{"role":"user","content":"How are you?"}]}`,
			req: &v1.ResponsesRequest{
				Model: "gpt-4o",
				Input: v1.ResponsesInput{
					Array: []v1.ResponsesInputItem{
						{Role: "system", Content: &v1.ChatMessageContent{String: "Be concise"}},
						{Role: "user", Content: &v1.ChatMessageContent{String: "Hello"}},
						{Role: "assistant", Content: &v1.ChatMessageContent{String: "Hi!"}},
						{Role: "user", Content: &v1.ChatMessageContent{String: "How are you?"}},
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.inputJSON)), "test case must be valid JSON")

			var req v1.ResponsesRequest
			require.NoError(t, json.Unmarshal([]byte(c.inputJSON), &req), "unmarshal error")

			if c.req != nil {
				unknown := req.Unknown
				req.Unknown = nil
				require.EqualValues(t, *c.req, req, "expected struct values")
				req.Unknown = unknown
			}

			out, err := json.Marshal(req)
			require.NoError(t, err, "marshal error")

			if c.roundTripJSON != "" {
				require.JSONEq(t, c.roundTripJSON, string(out), "expected specific round-trip JSON")
			} else {
				require.JSONEq(t, c.inputJSON, string(out), "expected round-trip JSON to remain unchanged")
			}
		})
	}
}
