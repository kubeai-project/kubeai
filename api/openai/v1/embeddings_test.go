package v1_test

import (
	"testing"

	stdjson "encoding/json"

	"github.com/go-json-experiment/json"
	v1 "github.com/kubeai-project/kubeai/api/openai/v1"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingRequest_JSON(t *testing.T) {
	cases := []struct {
		name          string
		json          string
		roundTripJSON string
		req           *v1.EmbeddingRequest
	}{
		{
			name: "real world example",
			json: `{
        "input": "What is the meaning of life?",
        "model": "text-embedding-ada-002",
        "encoding_format": "float"
}`,
			req: &v1.EmbeddingRequest{
				Model:          "text-embedding-ada-002",
				Input:          "What is the meaning of life?",
				EncodingFormat: v1.EmbeddingEncodingFormatFloat,
			},
		},
		{
			name: "empty request",
			json: `{"model":"","input":null}`,
			req:  &v1.EmbeddingRequest{},
		},
		{
			name: "minimal",
			json: `{"model":"test","input": "hey"}`,
			req:  &v1.EmbeddingRequest{Model: "test", Input: "hey"},
		},
		{
			name: "extra field test",
			json: `{"model":"text-embedding-ada-002","input":"test","extra_field":"should be preserved"}`,
			req: &v1.EmbeddingRequest{
				Model: "text-embedding-ada-002",
				Input: "test",
			},
		},
		{
			name: "string input",
			json: `{"model":"text-embedding-ada-002","input":"This is a test string"}`,
			req: &v1.EmbeddingRequest{
				Model: "text-embedding-ada-002",
				Input: "This is a test string",
			},
		},
		{
			name: "array input",
			json: `{"model":"text-embedding-ada-002","input":["This is a test string","And another string"]}`,
			req: &v1.EmbeddingRequest{
				Model: "text-embedding-ada-002",
				Input: []interface{}{"This is a test string", "And another string"},
			},
		},
		{
			name: "with encoding format float",
			json: `{"model":"text-embedding-ada-002","input":"Test","encoding_format":"float"}`,
			req: &v1.EmbeddingRequest{
				Model:          "text-embedding-ada-002",
				Input:          "Test",
				EncodingFormat: v1.EmbeddingEncodingFormatFloat,
			},
		},
		{
			name: "with encoding format base64",
			json: `{"model":"text-embedding-ada-002","input":"Test","encoding_format":"base64"}`,
			req: &v1.EmbeddingRequest{
				Model:          "text-embedding-ada-002",
				Input:          "Test",
				EncodingFormat: v1.EmbeddingEncodingFormatBase64,
			},
		},
		{
			name: "all fields set",
			json: `{
			"model": "text-embedding-3-large",
			"input": "The food was delicious and the waiter...",
			"encoding_format": "float",
			"user": "user123",
			"dimensions": 256
		}`,
			req: &v1.EmbeddingRequest{
				Model:          "text-embedding-3-large",
				Input:          "The food was delicious and the waiter...",
				EncodingFormat: v1.EmbeddingEncodingFormatFloat,
				User:           "user123",
				Dimensions:     256,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.json)), "test case should be valid json")

			var req v1.EmbeddingRequest
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

func TestEmbeddingResponse_JSON(t *testing.T) {
	cases := []struct {
		name          string
		json          string
		roundTripJSON string
		resp          *v1.EmbeddingResponse
	}{
		{
			name: "real world example",
			json: `{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "embedding": [0.0022756963, -0.009305916, 0.015742613, -0.0077253063, -0.0047450014],
      "index": 0
    }
  ],
  "model": "text-embedding-ada-002-v2",
  "usage": {
    "prompt_tokens": 8,
    "total_tokens": 8
  }
}`,
			resp: &v1.EmbeddingResponse{
				Object: "list",
				Data: []v1.Embedding{
					{
						Object:    "embedding",
						Embedding: []float32{0.0022756963, -0.009305916, 0.015742613, -0.0077253063, -0.0047450014},
						Index:     0,
					},
				},
				Model: "text-embedding-ada-002-v2",
				Usage: &v1.EmbeddingUsage{
					PromptTokens: 8,
					TotalTokens:  8,
				},
			},
		},
		{
			name: "empty response",
			json: `{"object":"","data":[],"model":""}`,
			resp: &v1.EmbeddingResponse{Data: []v1.Embedding{}},
		},
		{
			name: "single embedding",
			json: `{
			"object": "list",
			"data": [
				{
					"object": "embedding",
					"embedding": [0.1, 0.2, 0.3],
					"index": 0
				}
			],
			"model": "text-embedding-ada-002",
			"usage": {
				"prompt_tokens": 10,
				"total_tokens": 20
			}
		}`,
			resp: &v1.EmbeddingResponse{
				Object: "list",
				Data: []v1.Embedding{
					{
						Object:    "embedding",
						Embedding: []float32{0.1, 0.2, 0.3},
						Index:     0,
					},
				},
				Model: "text-embedding-ada-002",
				Usage: &v1.EmbeddingUsage{
					PromptTokens: 10,
					TotalTokens:  20,
				},
			},
		},
		{
			name: "multiple embeddings",
			json: `{
			"object": "list",
			"data": [
				{
					"object": "embedding",
					"embedding": [0.1, 0.2, 0.3],
					"index": 0
				},
				{
					"object": "embedding",
					"embedding": [0.4, 0.5, 0.6],
					"index": 1
				}
			],
			"model": "text-embedding-ada-002"
		}`,
			resp: &v1.EmbeddingResponse{
				Object: "list",
				Data: []v1.Embedding{
					{
						Object:    "embedding",
						Embedding: []float32{0.1, 0.2, 0.3},
						Index:     0,
					},
					{
						Object:    "embedding",
						Embedding: []float32{0.4, 0.5, 0.6},
						Index:     1,
					},
				},
				Model: "text-embedding-ada-002",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.json)), "test case should be valid json")

			var resp v1.EmbeddingResponse
			err := json.Unmarshal([]byte(c.json), &resp)
			require.NoError(t, err, "unmarshal error")

			if c.resp != nil {
				// Set aside the unknown fields to check equality without unknown fields
				unknown := resp.Unknown
				resp.Unknown = nil
				require.EqualValues(t, *c.resp, resp, "expected struct values")
				// Restore the proxy
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
