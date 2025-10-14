package v1_test

import (
	"fmt"
	"testing"

	stdjson "encoding/json"

	"github.com/go-json-experiment/json"
	v1 "github.com/kubeai-project/kubeai/api/openai/v1"
	"github.com/stretchr/testify/require"
)

func TestCompletionRequestPrefix(t *testing.T) {
	cases := []struct {
		input            string
		n                int
		exp              string
		expErrorContains []string
	}{
		{`{}`, 0, "", []string{"missing", "prompt"}},
		{`{}`, 9, "", []string{"missing", "prompt"}},
		{`{"prompt": "abc"}`, 0, "", nil},
		{`{"prompt": "abc"}`, 9, "abc", nil},
		{`{"prompt": "abcefghijk"}`, 9, "abcefghij", nil},
		{`{"prompt": "世界"}`, 0, "", nil},
		{`{"prompt": "世界"}`, 1, "世", nil},
		{`{"prompt": "世界"}`, 2, "世界", nil},
		{`{"prompt": "世界"}`, 3, "世界", nil},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%q %d", c.input, c.n), func(t *testing.T) {
			var req v1.CompletionRequest
			require.NoError(t, json.Unmarshal([]byte(c.input), &req))
			require.Equal(t, c.exp, req.Prefix(c.n))
		})
	}
}

func TestCompletionRequest_JSON(t *testing.T) {
	cases := []struct {
		name          string
		json          string
		roundTripJSON string
		req           *v1.CompletionRequest
	}{
		{
			name: "real-world example - basic completion",
			json: `{
        "model": "deepseek-r1-distill-llama-8b-l4",
        "prompt": "Say this is a test",
        "max_tokens": 7,
        "temperature": 0
      }`,
			req: &v1.CompletionRequest{
				Model:       "deepseek-r1-distill-llama-8b-l4",
				Prompt:      "Say this is a test",
				MaxTokens:   7,
				Temperature: v1.Ptr[float32](0),
			},
		},
		{
			name: "real-world example - streaming completion",
			json: `{
        "model": "deepseek-r1-distill-llama-8b-l4",
        "prompt": "Say this is a test",
        "max_tokens": 7,
        "temperature": 0,
        "stream": true
      }`,
			req: &v1.CompletionRequest{
				Model:       "deepseek-r1-distill-llama-8b-l4",
				Prompt:      "Say this is a test",
				MaxTokens:   7,
				Temperature: v1.Ptr[float32](0),
				Stream:      true,
			},
		},
		{
			name: "empty request",
			json: `{"model":"", "prompt": null}`,
			req:  &v1.CompletionRequest{},
		},
		{
			name: "extra field test",
			json: `{"model":"test-model", "prompt": "test-prompt", "extra_field":"should be preserved"}`,
			req: &v1.CompletionRequest{
				Model:  "test-model",
				Prompt: "test-prompt",
			},
		},
		{
			name: "string prompt",
			json: `{"model":"test-model","prompt":"test-prompt"}`,
			req: &v1.CompletionRequest{
				Model:  "test-model",
				Prompt: "test-prompt",
			},
		},
		{
			name: "array prompt",
			json: `{"model":"test-model","prompt":["test-prompt1","test-prompt2"]}`,
			req: &v1.CompletionRequest{
				Model:  "test-model",
				Prompt: []interface{}{"test-prompt1", "test-prompt2"},
			},
		},
		{
			name: "all fields set",
			json: `{
				"model": "text-davinci-003",
				"prompt": "Write a story about a robot.",
				"max_tokens": 500,
				"temperature": 0.7,
				"top_p": 0.95,
				"n": 1,
				"stream": true,
				"stop": ["END", "STOP"],
				"presence_penalty": 0.5,
				"frequency_penalty": 0.8,
				"logit_bias": {"50256": -100},
				"seed": 42,
				"echo": true,
				"best_of": 3,
				"logprobs": 5,
				"suffix": "The End.",
				"user": "user123",
				"store": true,
				"metadata": {
					"user_id": "abc123",
					"session_id": "sess_456"
				}
			}`,
			req: &v1.CompletionRequest{
				Model:            "text-davinci-003",
				Prompt:           "Write a story about a robot.",
				MaxTokens:        500,
				Temperature:      v1.Ptr[float32](0.7),
				TopP:             v1.Ptr[float32](0.95),
				N:                v1.Ptr(1),
				Stream:           true,
				Stop:             []string{"END", "STOP"},
				PresencePenalty:  v1.Ptr[float32](0.5),
				FrequencyPenalty: v1.Ptr[float32](0.8),
				LogitBias:        map[string]int{"50256": -100},
				Seed:             func() *int { i := 42; return &i }(),
				Echo:             true,
				BestOf:           v1.Ptr(3),
				LogProbs:         v1.Ptr(5),
				Suffix:           "The End.",
				User:             "user123",
				Store:            true,
				Metadata: map[string]string{
					"user_id":    "abc123",
					"session_id": "sess_456",
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.json)), "test case should be valid json")

			var req v1.CompletionRequest
			err := json.Unmarshal([]byte(c.json), &req)
			require.NoError(t, err, "unmarshal error")

			if c.req != nil {
				unknown := req.Unknown
				req.Unknown = nil
				// Assert on equality without the unknown fields.
				require.EqualValues(t, *c.req, req, "expected struct values")
				req.Unknown = unknown
			}

			jsn, err := json.Marshal(req)
			require.NoError(t, err, "marshal error")
			if c.roundTripJSON != "" {
				requireEqualJSON(t, c.roundTripJSON, string(jsn), "expected exact round-trip JSON")
			} else {
				requireEqualJSON(t, c.json, string(jsn), "expected round-trip JSON to remain unchanged")
			}
		})
	}
}

func TestCompletionResponse_JSON(t *testing.T) {
	cases := []struct {
		name          string
		json          string
		roundTripJSON string
		resp          *v1.CompletionResponse
	}{
		{
			name: "openapi spec example - no streaming",
			json: `{
  "id": "cmpl-uqkvlQyYK7bGYrRHQ0eXlWi7",
  "object": "text_completion",
  "created": 1589478378,
  "model": "VAR_completion_model_id",
  "system_fingerprint": "fp_44709d6fcb",
  "choices": [
    {
      "text": "\n\nThis is indeed a test",
      "index": 0,
      "logprobs": null,
      "finish_reason": "length"
    }
  ],
  "usage": {
    "prompt_tokens": 5,
    "completion_tokens": 7,
    "total_tokens": 12
  }
}`,
			resp: &v1.CompletionResponse{
				ID:                "cmpl-uqkvlQyYK7bGYrRHQ0eXlWi7",
				Object:            "text_completion",
				Created:           1589478378,
				Model:             "VAR_completion_model_id",
				SystemFingerprint: "fp_44709d6fcb",
				Choices: []v1.CompletionChoice{
					{
						Text:         "\n\nThis is indeed a test",
						Index:        0,
						LogProbs:     nil,
						FinishReason: v1.Ptr("length"),
					},
				},
				Usage: &v1.CompletionUsage{
					PromptTokens:     5,
					CompletionTokens: 7,
					TotalTokens:      12,
				},
			},
		},
		{
			name: "openapi spec example - streaming",
			json: `{
  "id": "cmpl-7iA7iJjj8V2zOkCGvWF2hAkDWBQZe",
  "object": "text_completion",
  "created": 1690759702,
  "choices": [
    {
      "text": "This",
      "index": 0,
      "logprobs": null,
      "finish_reason": null
    }
  ],
  "model": "gpt-3.5-turbo-instruct",
  "system_fingerprint": "fp_44709d6fcb"
}`,
			resp: &v1.CompletionResponse{
				ID:                "cmpl-7iA7iJjj8V2zOkCGvWF2hAkDWBQZe",
				Object:            "text_completion",
				Created:           1690759702,
				Model:             "gpt-3.5-turbo-instruct",
				SystemFingerprint: "fp_44709d6fcb",
				Choices: []v1.CompletionChoice{
					{
						Text:     "This",
						Index:    0,
						LogProbs: nil,
						// FinishReason is omitted in streaming responses until the final chunk
					},
				},
			},
		},
		{
			name: "empty response",
			json: `{"object":"","model":"","choices":[]}`,
			resp: &v1.CompletionResponse{Choices: []v1.CompletionChoice{}},
		},
		{
			name: "basic response",
			json: `{
				"id": "cmpl-123",
				"object": "text_completion",
				"created": 1677858242,
				"model": "text-davinci-003",
				"choices": [
					{
						"text": "This is a test response.",
						"index": 0,
						"finish_reason": "stop",
						"logprobs": null
					}
				]
			}`,
			resp: &v1.CompletionResponse{
				ID:      "cmpl-123",
				Object:  "text_completion",
				Created: 1677858242,
				Model:   "text-davinci-003",
				Choices: []v1.CompletionChoice{
					{
						Text:         "This is a test response.",
						Index:        0,
						FinishReason: v1.Ptr("stop"),
					},
				},
			},
		},
		{
			name: "multiple choices",
			json: `{
				"id": "cmpl-456",
				"object": "text_completion",
				"created": 1689324671,
				"model": "text-davinci-003",
				"choices": [
					{
						"text": "First completion option.",
						"index": 0,
						"finish_reason": "stop",
						"logprobs": null
					},
					{
						"text": "Second completion option.",
						"index": 1,
						"finish_reason": "length",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 10,
					"completion_tokens": 15,
					"total_tokens": 25
				}
			}`,
			resp: &v1.CompletionResponse{
				ID:      "cmpl-456",
				Object:  "text_completion",
				Created: 1689324671,
				Model:   "text-davinci-003",
				Choices: []v1.CompletionChoice{
					{
						Text:         "First completion option.",
						Index:        0,
						FinishReason: v1.Ptr("stop"),
					},
					{
						Text:         "Second completion option.",
						Index:        1,
						FinishReason: v1.Ptr("length"),
					},
				},
				Usage: &v1.CompletionUsage{
					PromptTokens:     10,
					CompletionTokens: 15,
					TotalTokens:      25,
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.json)), "test case should be valid json")

			var resp v1.CompletionResponse
			err := json.Unmarshal([]byte(c.json), &resp)
			require.NoError(t, err, "unmarshal error")

			if c.resp != nil {
				// Set aside the unknown fields to check equality without unknown fields
				unknown := resp.Unknown
				resp.Unknown = nil
				// Assert on equality without the unknown fields.
				require.EqualValues(t, *c.resp, resp, "expected struct values")
				resp.Unknown = unknown
			}

			jsn, err := json.Marshal(resp)
			require.NoError(t, err, "marshal error")
			if c.roundTripJSON != "" {
				requireEqualJSON(t, c.roundTripJSON, string(jsn), "expected exact round-trip JSON")
			} else {
				requireEqualJSON(t, c.json, string(jsn), "expected round-trip JSON to remain unchanged")
			}
		})
	}
}
