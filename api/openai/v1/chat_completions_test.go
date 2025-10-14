package v1_test

import (
	stdjson "encoding/json"
	"fmt"
	"testing"

	"github.com/go-json-experiment/json"
	v1 "github.com/kubeai-project/kubeai/api/openai/v1"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionRequestPrefix(t *testing.T) {
	cases := []struct {
		input            string
		n                int
		exp              string
		expErrorContains []string
	}{
		{`{}`, 0, "", []string{"missing", "messages"}},
		{`{}`, 0, "", []string{"missing", "messages"}},
		{`{"messages": []}`, 0, "", []string{"empty"}},
		{`{"messages": []}`, 9, "", []string{"empty"}},
		{`{"messages": [{"role": "user", "content": "abc"}]}`, 0, "", nil},
		{`{"messages": [{"role": "user", "content": "abc"}]}`, 9, "abc", nil},
		{`{"messages": [{"role": "user", "content": "abcefghijk"}]}`, 9, "abcefghij", nil},
		{`{"messages": [{"role": "user", "content": "世界"}]}`, 0, "", nil},
		{`{"messages": [{"role": "user", "content": "世界"}]}`, 1, "世", nil},
		{`{"messages": [{"role": "user", "content": "世界"}]}`, 2, "世界", nil},
		{`{"messages": [{"role": "user", "content": "世界"}]}`, 3, "世界", nil},
		{`{"messages": [{"role": "user", "content": "abc"}, {"role": "user", "content": "xyz"}]}`, 0, "", nil},
		{`{"messages": [{"role": "user", "content": "abc"}, {"role": "user", "content": "xyz"}]}`, 9, "abc", nil},
		{`{"messages": [{"role": "system", "content": "abc"}, {"role": "user", "content": "xyz"}]}`, 0, "", nil},
		{`{"messages": [{"role": "system", "content": "abc"}, {"role": "user", "content": "xyz"}]}`, 9, "xyz", nil},
		{`{"messages": [{"role": "system", "content": "abc"}]}`, 9, "", []string{"no", "user", "found"}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%q %d", c.input, c.n), func(t *testing.T) {
			var body v1.ChatCompletionRequest
			require.NoError(t, json.Unmarshal([]byte(c.input), &body))
			require.Equal(t, c.exp, body.Prefix(c.n))
		})
	}
}

func TestChatCompletionRequest_JSON(t *testing.T) {
	cases := []struct {
		name          string
		json          string
		roundTripJSON string
		req           *v1.ChatCompletionRequest
	}{
		// Real-world examples from reference folder
		{
			name: "extra field test",
			json: `{
				"model": "deepseek-r1-distill-llama-8b-l4",
				"messages": [
					{
						"role": "system",
						"content": "You are a helpful assistant."
					},
					{
						"role": "user",
						"content": "Hello!"
					}
				],
				"extra_field": "should be preserved"
			}`,
			req: &v1.ChatCompletionRequest{
				Model: "deepseek-r1-distill-llama-8b-l4",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "system",
						Content: &v1.ChatMessageContent{String: "You are a helpful assistant."},
					},
					{
						Role:    "user",
						Content: &v1.ChatMessageContent{String: "Hello!"},
					},
				},
			},
		},
		{
			name: "basic chat completion",
			json: `{
				"model": "deepseek-r1-distill-llama-8b-l4",
				"messages": [
					{
						"role": "system",
						"content": "You are a helpful assistant."
					},
					{
						"role": "user",
						"content": "Hello!"
					}
				]
			}`,
			req: &v1.ChatCompletionRequest{
				Model: "deepseek-r1-distill-llama-8b-l4",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "system",
						Content: &v1.ChatMessageContent{String: "You are a helpful assistant."},
					},
					{
						Role:    "user",
						Content: &v1.ChatMessageContent{String: "Hello!"},
					},
				},
			},
		},

		// OpenAPI example: Image input
		{
			name: "openapi image input example",
			json: `{
				"model": "gpt-4o",
				"messages": [
					{
						"role": "user",
						"content": [
							{
								"type": "text",
								"text": "What's in this image?"
							},
							{
								"type": "image_url",
								"image_url": {
									"url": "https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/2560px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg"
								}
							}
						]
					}
				],
				"max_tokens": 300
			}`,
			req: &v1.ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []v1.ChatCompletionMessage{
					{
						Role: "user",
						Content: &v1.ChatMessageContent{
							Array: []v1.ChatMessageContentPart{
								{
									Type: "text",
									Text: "What's in this image?",
								},
								{
									Type: "image_url",
									ImageURL: &v1.ChatMessageImageURL{
										URL: "https://upload.wikimedia.org/wikipedia/commons/thumb/d/dd/Gfp-wisconsin-madison-the-nature-boardwalk.jpg/2560px-Gfp-wisconsin-madison-the-nature-boardwalk.jpg",
									},
								},
							},
						},
					},
				},
				MaxTokens: 300,
			},
		},

		// Real-world example - streaming chat completion
		{
			name: "real-world example - streaming chat completion",
			json: `{
				"model": "deepseek-r1-distill-llama-8b-l4",
				"messages": [
					{
						"role": "system",
						"content": "You are a helpful assistant."
					},
					{
						"role": "user",
						"content": "Hello!"
					}
				],
				"stream": true
			}`,
			req: &v1.ChatCompletionRequest{
				Model: "deepseek-r1-distill-llama-8b-l4",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "system",
						Content: &v1.ChatMessageContent{String: "You are a helpful assistant."},
					},
					{
						Role:    "user",
						Content: &v1.ChatMessageContent{String: "Hello!"},
					},
				},
				Stream: true,
			},
		},

		// OpenAPI example: Functions/Tools
		{
			name: "openapi function calling example",
			json: `{
				"model": "gpt-4o",
				"messages": [
					{
						"role": "user",
						"content": "What's the weather like in Boston today?"
					}
				],
				"tools": [
					{
						"type": "function",
						"function": {
							"name": "get_current_weather",
							"description": "Get the current weather in a given location",
							"parameters": {
								"type": "object",
								"properties": {
									"location": {
										"type": "string",
										"description": "The city and state, e.g. San Francisco, CA"
									},
									"unit": {
										"type": "string",
										"enum": ["celsius", "fahrenheit"]
									}
								},
								"required": ["location"]
							}
						}
					}
				],
				"tool_choice": "auto"
			}`,
			req: &v1.ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "user",
						Content: &v1.ChatMessageContent{String: "What's the weather like in Boston today?"},
					},
				},
				Tools: []v1.Tool{
					{
						Type: "function",
						Function: &v1.FunctionDefinition{
							Name:        "get_current_weather",
							Description: "Get the current weather in a given location",
							Parameters: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"location": map[string]interface{}{
										"type":        "string",
										"description": "The city and state, e.g. San Francisco, CA",
									},
									"unit": map[string]interface{}{
										"type": "string",
										"enum": []interface{}{"celsius", "fahrenheit"},
									},
								},
								"required": []interface{}{"location"},
							},
						},
					},
				},
				ToolChoice: "auto",
			},
		},

		// OpenAPI example: Logprobs
		{
			name: "openapi logprobs example",
			json: `{
				"model": "gpt-4o",
				"messages": [
					{
						"role": "user",
						"content": "Hello!"
					}
				],
				"logprobs": true,
				"top_logprobs": 2
			}`,
			req: &v1.ChatCompletionRequest{
				Model: "gpt-4o",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "user",
						Content: &v1.ChatMessageContent{String: "Hello!"},
					},
				},
				LogProbs:    true,
				TopLogProbs: v1.Ptr(2),
			},
		},
		{
			name: "empty request",
			json: `{"model":"","messages":null}`,
			req:  &v1.ChatCompletionRequest{},
		},
		{
			name:          "null values",
			json:          `{"model": null, "messages": null, "store": null, "reasoning_effort": null, "metadata": null, "service_tier": null}`,
			roundTripJSON: `{"model": "", "messages": null}`,
			req:           &v1.ChatCompletionRequest{},
		},
		{
			name: "extra field",
			json: `{"model":"","messages":null,"eexxttrraa":"val"}`,
			req:  &v1.ChatCompletionRequest{},
		},
		{
			name: "single content message",
			json: `{"model":"test-model","messages":[{"role":"user","content":"test-prefix"}],"extra":"val"}`,
			req: &v1.ChatCompletionRequest{
				Model: "test-model",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "user",
						Content: &v1.ChatMessageContent{String: "test-prefix"},
					},
				},
			},
		},
		{
			name: "multi content message",
			json: `{
	"model":"test-model",
	"messages":[
		{
			"role":"user",
			"content": [
		        {"type": "text", "text": "What's in this image?"},
		        {
		            "type": "image_url",
		            "image_url": {
		            	"url": "https://example.com/image.jpg"
		            }
		        }
			]
		}
	],
	"extra":"val"
}`,
			req: &v1.ChatCompletionRequest{
				Model: "test-model",
				Messages: []v1.ChatCompletionMessage{
					{
						Role: "user",
						Content: &v1.ChatMessageContent{
							Array: []v1.ChatMessageContentPart{
								{Type: "text", Text: "What's in this image?"},
								{
									Type: "image_url",
									ImageURL: &v1.ChatMessageImageURL{
										URL: "https://example.com/image.jpg",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "all fields set",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "system",
				"content": "You are a helpful assistant."
			},
			{
				"role": "user",
				"name": "John",
				"content": "Hello!"
			},
			{
				"role": "assistant",
				"content": "Hi there!",
				"tool_calls": [
					{
						"id": "call_123",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"San Francisco\",\"unit\":\"celsius\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_123",
				"content": "{\"temperature\":18,\"unit\":\"celsius\",\"description\":\"Partly cloudy\"}"
			}
		],
		"max_tokens": 100,
		"max_completion_tokens": 150,
		"temperature": 0.7,
		"top_p": 0.95,
		"n": 1,
		"stream": true,
		"stop": ["END", "STOP"],
		"presence_penalty": 0.5,
		"response_format": {
			"type": "json_object"
		},
		"seed": 42,
		"frequency_penalty": 0.8,
		"logit_bias": {"50256": -100},
		"logprobs": true,
		"top_logprobs": 3,
		"user": "user123",
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get the current weather in a location",
					"parameters": {
						"type": "object",
						"properties": {
							"location": {
								"type": "string",
								"description": "The city and state, e.g. San Francisco, CA"
							},
							"unit": {
								"type": "string",
								"enum": ["celsius", "fahrenheit"]
							}
						},
						"required": ["location"]
					}
				}
			}
		],
		"tool_choice": {
			"type": "function",
			"function": {
				"name": "get_weather"
			}
		},
		"stream_options": {
			"include_usage": true
		},
		"parallel_tool_calls": true,
		"store": true,
		"reasoning_effort": "high",
		"metadata": {
			"user_id": "abc123",
			"conversation_id": "conv_456"
		}
	}`,
			req: &v1.ChatCompletionRequest{
				Model: "gpt-4",
				Messages: []v1.ChatCompletionMessage{
					{
						Role:    "system",
						Content: &v1.ChatMessageContent{String: "You are a helpful assistant."},
					},
					{
						Role:    "user",
						Name:    "John",
						Content: &v1.ChatMessageContent{String: "Hello!"},
					},
					{
						Role:    "assistant",
						Content: &v1.ChatMessageContent{String: "Hi there!"},
						ToolCalls: []v1.ToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: v1.FunctionCall{
									Name:      "get_weather",
									Arguments: "{\"location\":\"San Francisco\",\"unit\":\"celsius\"}",
								},
							},
						},
					},
					{
						Role:       "tool",
						ToolCallID: "call_123",
						Content:    &v1.ChatMessageContent{String: "{\"temperature\":18,\"unit\":\"celsius\",\"description\":\"Partly cloudy\"}"},
					},
				},
				MaxTokens:           100,
				MaxCompletionTokens: 150,
				Temperature:         v1.Ptr[float32](0.7),
				TopP:                v1.Ptr[float32](0.95),
				N:                   v1.Ptr(1),
				Stream:              true,
				Stop:                []string{"END", "STOP"},
				PresencePenalty:     v1.Ptr[float32](0.5),
				ResponseFormat: &v1.ChatCompletionResponseFormat{
					Type: "json_object",
				},
				Seed:             func() *int { i := 42; return &i }(),
				FrequencyPenalty: v1.Ptr[float32](0.8),
				LogitBias:        map[string]int{"50256": -100},
				LogProbs:         true,
				TopLogProbs:      v1.Ptr(3),
				User:             "user123",
				Tools: []v1.Tool{
					{
						Type: "function",
						Function: &v1.FunctionDefinition{
							Name:        "get_weather",
							Description: "Get the current weather in a location",
							Parameters: map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"location": map[string]interface{}{
										"type":        "string",
										"description": "The city and state, e.g. San Francisco, CA",
									},
									"unit": map[string]interface{}{
										"type": "string",
										"enum": []interface{}{"celsius", "fahrenheit"},
									},
								},
								"required": []interface{}{"location"},
							},
						},
					},
				},
				ToolChoice: map[string]interface{}{
					"type": "function",
					"function": map[string]interface{}{
						"name": "get_weather",
					},
				},
				StreamOptions: &v1.StreamOptions{
					IncludeUsage: true,
				},
				ParallelToolCalls: v1.Ptr(true),
				Store:             true,
				ReasoningEffort:   "high",
				Metadata: map[string]string{
					"user_id":         "abc123",
					"conversation_id": "conv_456",
				},
			},
		},
		{
			name: "auto tool choice",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "user",
				"content": "What's the weather like in San Francisco?"
			}
		],
		"tool_choice": "auto",
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get the current weather in a location",
					"parameters": {
						"type": "object",
						"properties": {
							"location": {
								"type": "string",
								"description": "The city and state, e.g. San Francisco, CA"
							}
						},
						"required": ["location"]
					}
				}
			}
		]
	}`,
			req: nil,
		},
		{
			name: "none tool choice",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "user",
				"content": "Just respond normally without using any tools."
			}
		],
		"tool_choice": "none",
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get the current weather in a location",
					"parameters": {
						"type": "object",
						"properties": {
							"location": {
								"type": "string",
								"description": "The city and state, e.g. San Francisco, CA"
							}
						},
						"required": ["location"]
					}
				}
			}
		]
	}`,
		},
		{
			name: "multiple tool calls",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "user",
				"content": "What's the weather like in San Francisco and New York?"
			},
			{
				"role": "assistant",
				"content": null,
				"tool_calls": [
					{
						"id": "call_sf123",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"San Francisco\"}"
						}
					},
					{
						"id": "call_ny456",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"New York\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_sf123",
				"content": "{\"temperature\":18,\"unit\":\"celsius\",\"description\":\"Sunny\"}"
			},
			{
				"role": "tool",
				"tool_call_id": "call_ny456",
				"content": "{\"temperature\":12,\"unit\":\"celsius\",\"description\":\"Cloudy\"}"
			}
		]
	}`,
		},
		{
			name: "request with vision model",
			json: `{
		"model": "gpt-4-vision",
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "text",
						"text": "What's in this image?"
					},
					{
						"type": "image_url",
						"image_url": {
							"url": "https://example.com/image.jpg",
							"detail": "high"
						}
					},
					{
						"type": "image_url",
						"image_url": {
							"url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEASABIAAD...",
							"detail": "low"
						}
					}
				]
			}
		],
		"max_tokens": 300
	}`,
		},
		{
			name: "request with image captions",
			json: `{
		"model": "gpt-4-vision",
		"messages": [
			{
				"role": "user",
				"content": [
					{
						"type": "text",
						"text": "Describe these images"
					},
					{
						"type": "image_url",
						"image_url": {
							"url": "https://example.com/image1.jpg",
							"detail": "high"
						}
					},
					{
						"type": "image_url",
						"image_url": {
							"url": "https://example.com/image2.jpg"
						}
					}
				]
			}
		],
		"response_format": {"type": "text"}
	}`,
		},
		{
			name: "function calling request",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "user",
				"content": "What's the weather in Boston and San Francisco?"
			}
		],
		"functions": [
			{
				"name": "get_weather",
				"description": "Get the current weather in a location",
				"parameters": {
					"type": "object",
					"properties": {
						"location": {
							"type": "string",
							"description": "The city and state, e.g. San Francisco, CA"
						}
					},
					"required": ["location"]
				}
			}
		]
	}`,
		},
		{
			name: "logprobs with top_logprobs",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "user",
				"content": "Generate a random sentence."
			}
		],
		"logprobs": true,
		"top_logprobs": 5
	}`,
		},
		{
			name: "request with system_fingerprint",
			json: `{
		"model": "gpt-4-0125-preview",
		"messages": [
			{
				"role": "user", 
				"content": "Hello"
			}
		],
		"system_fingerprint": "fp_44709d6fcb"
	}`,
		},
		{
			name: "request with dimensions in response format",
			json: `{
		"model": "gpt-4",
		"messages": [
			{
				"role": "user", 
				"content": "Generate something"
			}
		],
		"response_format": {
			"type": "json_object",
			"json_schema": {
				"name": "something",
				"schema": {
					"abc": {
						"type": "array",
						"items": {"type": "number"},
						"dimensions": 1536
					}
				}
			}
		}
	}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.json)), "test case should be valid json")

			var req v1.ChatCompletionRequest
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
				require.JSONEq(t, c.roundTripJSON, string(jsn), "expected specific round-trip JSON")
			} else {
				require.JSONEq(t, c.json, string(jsn), "expected round-trip JSON to remain unchanged")
			}
		})
	}
}

func TestChatCompletionRequest_InvalidMessageContent(t *testing.T) {
	validArray := v1.ChatMessageContent{
		Array: []v1.ChatMessageContentPart{
			{Type: "text", Text: "test"},
			{Type: "image_url", ImageURL: &v1.ChatMessageImageURL{
				URL: "https://example.com/image.jpg",
			}},
		},
	}
	_, err := json.Marshal(validArray)
	require.NoError(t, err)

	validString := v1.ChatMessageContent{
		String: "test",
	}
	_, err = json.Marshal(validString)
	require.NoError(t, err)

	invalidBoth := v1.ChatMessageContent{
		Array: []v1.ChatMessageContentPart{
			{Type: "text", Text: "test"},
		},
		String: "test",
	}
	_, err = json.Marshal(invalidBoth)
	require.ErrorContains(t, err, "String and Array cannot be specified at the same time")
}

func TestChatCompletionResponse_JSON(t *testing.T) {
	cases := []struct {
		name          string
		json          string
		roundTripJSON string
		resp          *v1.ChatCompletionResponse
	}{
		// OpenAPI example: Default response
		{
			name: "openapi default response example",
			json: `{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"created": 1677652288,
				"model": "gpt-4o-mini",
				"system_fingerprint": "fp_44709d6fcb",
				"choices": [{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "\n\nHello there, how may I assist you today?"
					},
					"finish_reason": "stop",
					"logprobs": null
				}],
				"service_tier": "default",
				"usage": {
					"prompt_tokens": 9,
					"completion_tokens": 12,
					"total_tokens": 21,
					"completion_tokens_details": {
						"reasoning_tokens": 0,
						"accepted_prediction_tokens": 0,
						"rejected_prediction_tokens": 0
					}
				}
			}`,
			resp: &v1.ChatCompletionResponse{
				ID:                "chatcmpl-123",
				Object:            "chat.completion",
				Created:           1677652288,
				Model:             "gpt-4o-mini",
				SystemFingerprint: "fp_44709d6fcb",
				Choices: []v1.ChatCompletionChoice{
					{
						Index: 0,
						Message: v1.ChatCompletionMessage{
							Role:    "assistant",
							Content: &v1.ChatMessageContent{String: "\n\nHello there, how may I assist you today?"},
						},
						FinishReason: func() *v1.FinishReason { r := v1.FinishReasonStop; return &r }(),
					},
				},
				ServiceTier: "default",
				Usage: &v1.CompletionUsage{
					PromptTokens:     9,
					CompletionTokens: 12,
					TotalTokens:      21,
					CompletionTokensDetails: &v1.CompletionTokensDetails{
						ReasoningTokens:          v1.Ptr(0),
						AcceptedPredictionTokens: v1.Ptr(0),
						RejectedPredictionTokens: v1.Ptr(0),
					},
				},
			},
		},

		// OpenAPI example: Image response
		{
			name: "openapi image response example",
			json: `{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"created": 1677652288,
				"model": "gpt-4o-mini",
				"system_fingerprint": "fp_44709d6fcb",
				"choices": [{
					"index": 0,
					"message": {
						"role": "assistant",
						"content": "\n\nThis image shows a wooden boardwalk extending through a lush green marshland."
					},
					"finish_reason": "stop",
					"logprobs": null
				}],
				"usage": {
					"prompt_tokens": 9,
					"completion_tokens": 12,
					"total_tokens": 21,
					"completion_tokens_details": {
						"reasoning_tokens": 0,
						"accepted_prediction_tokens": 0,
						"rejected_prediction_tokens": 0
					}
				}
			}`,
			resp: &v1.ChatCompletionResponse{
				ID:                "chatcmpl-123",
				Object:            "chat.completion",
				Created:           1677652288,
				Model:             "gpt-4o-mini",
				SystemFingerprint: "fp_44709d6fcb",
				Choices: []v1.ChatCompletionChoice{
					{
						Index: 0,
						Message: v1.ChatCompletionMessage{
							Role:    "assistant",
							Content: &v1.ChatMessageContent{String: "\n\nThis image shows a wooden boardwalk extending through a lush green marshland."},
						},
						FinishReason: func() *v1.FinishReason { r := v1.FinishReasonStop; return &r }(),
					},
				},
				Usage: &v1.CompletionUsage{
					PromptTokens:     9,
					CompletionTokens: 12,
					TotalTokens:      21,
					CompletionTokensDetails: &v1.CompletionTokensDetails{
						ReasoningTokens:          v1.Ptr(0),
						AcceptedPredictionTokens: v1.Ptr(0),
						RejectedPredictionTokens: v1.Ptr(0),
					},
				},
			},
		},

		// OpenAPI example: Function Calling
		{
			name: "openapi function call response example",
			json: `{
				"id": "chatcmpl-abc123",
				"object": "chat.completion",
				"created": 1699896916,
				"model": "gpt-4o-mini",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": null,
							"tool_calls": [
								{
									"id": "call_abc123",
									"type": "function",
									"function": {
										"name": "get_current_weather",
										"arguments": "{\n\"location\": \"Boston, MA\"\n}"
									}
								}
							]
						},
						"finish_reason": "tool_calls",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 82,
					"completion_tokens": 17,
					"total_tokens": 99,
					"completion_tokens_details": {
						"reasoning_tokens": 0,
						"accepted_prediction_tokens": 0,
						"rejected_prediction_tokens": 0
					}
				}
			}`,
			resp: &v1.ChatCompletionResponse{
				ID:      "chatcmpl-abc123",
				Object:  "chat.completion",
				Created: 1699896916,
				Model:   "gpt-4o-mini",
				Choices: []v1.ChatCompletionChoice{
					{
						Index: 0,
						Message: v1.ChatCompletionMessage{
							Role:    "assistant",
							Content: nil,
							ToolCalls: []v1.ToolCall{
								{
									ID:   "call_abc123",
									Type: "function",
									Function: v1.FunctionCall{
										Name:      "get_current_weather",
										Arguments: "{\n\"location\": \"Boston, MA\"\n}",
									},
								},
							},
						},
						FinishReason: func() *v1.FinishReason { r := v1.FinishReasonToolCalls; return &r }(),
					},
				},
				Usage: &v1.CompletionUsage{
					PromptTokens:     82,
					CompletionTokens: 17,
					TotalTokens:      99,
					CompletionTokensDetails: &v1.CompletionTokensDetails{
						ReasoningTokens:          v1.Ptr(0),
						AcceptedPredictionTokens: v1.Ptr(0),
						RejectedPredictionTokens: v1.Ptr(0),
					},
				},
			},
		},

		{
			// Note: updated message to not include "refusal",
			name: "actual basic chat completion response",
			json: `{
  "id": "chatcmpl-B9Fi8c7rZHlYbOIjgoxXlXEmPdx97",
  "object": "chat.completion",
  "created": 1741545044,
  "model": "gpt-4o-2024-08-06",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I assist you today?"
      },
      "finish_reason": "stop",
      "logprobs": null
    }
  ],
  "usage": {
    "prompt_tokens": 19,
    "completion_tokens": 10,
    "total_tokens": 29,
    "prompt_tokens_details": {
      "cached_tokens": 0,
      "audio_tokens": 0
    },
    "completion_tokens_details": {
      "reasoning_tokens": 0,
      "audio_tokens": 0,
      "accepted_prediction_tokens": 0,
      "rejected_prediction_tokens": 0
    }
  },
  "service_tier": "default",
  "system_fingerprint": "fp_f9f4fb6dbf"
}`,
		},
		{
			// Updated to not specify .refusal as null
			name: "actual logprobs response example",
			json: `{
  "id": "chatcmpl-B9FejZsDe9csWCT7iFc3YTOAuwQyn",
  "object": "chat.completion",
  "created": 1741544833,
  "model": "gpt-4o-mini-2024-07-18",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I assist you today?"
      },
      "logprobs": {
        "content": [
          {
            "token": "Hello",
            "logprob": -0.0024763736873865128,
            "bytes": [
              72,
              101,
              108,
              108,
              111
            ],
            "top_logprobs": [
              {
                "token": "Hello",
                "logprob": -0.0024763736873865128,
                "bytes": [
                  72,
                  101,
                  108,
                  108,
                  111
                ]
              },
              {
                "token": "Hi",
                "logprob": -6.002476215362549,
                "bytes": [
                  72,
                  105
                ]
              }
            ]
          },
          {
            "token": "!",
            "logprob": -4.320199877838604e-7,
            "bytes": [
              33
            ],
            "top_logprobs": [
              {
                "token": "!",
                "logprob": -4.320199877838604e-7,
                "bytes": [
                  33
                ]
              },
              {
                "token": " there",
                "logprob": -15.0,
                "bytes": [
                  32,
                  116,
                  104,
                  101,
                  114,
                  101
                ]
              }
            ]
          },
          {
            "token": " How",
            "logprob": -1.7432603272027336e-6,
            "bytes": [
              32,
              72,
              111,
              119
            ],
            "top_logprobs": [
              {
                "token": " How",
                "logprob": -1.7432603272027336e-6,
                "bytes": [
                  32,
                  72,
                  111,
                  119
                ]
              },
              {
                "token": " What",
                "logprob": -13.375001907348633,
                "bytes": [
                  32,
                  87,
                  104,
                  97,
                  116
                ]
              }
            ]
          },
          {
            "token": " can",
            "logprob": -1.5048530030981055e-6,
            "bytes": [
              32,
              99,
              97,
              110
            ],
            "top_logprobs": [
              {
                "token": " can",
                "logprob": -1.5048530030981055e-6,
                "bytes": [
                  32,
                  99,
                  97,
                  110
                ]
              },
              {
                "token": " may",
                "logprob": -13.500001907348633,
                "bytes": [
                  32,
                  109,
                  97,
                  121
                ]
              }
            ]
          },
          {
            "token": " I",
            "logprob": 0.0,
            "bytes": [
              32,
              73
            ],
            "top_logprobs": [
              {
                "token": " I",
                "logprob": 0.0,
                "bytes": [
                  32,
                  73
                ]
              },
              {
                "token": "I",
                "logprob": -19.125,
                "bytes": [
                  73
                ]
              }
            ]
          },
          {
            "token": " assist",
            "logprob": -0.0007100477814674377,
            "bytes": [
              32,
              97,
              115,
              115,
              105,
              115,
              116
            ],
            "top_logprobs": [
              {
                "token": " assist",
                "logprob": -0.0007100477814674377,
                "bytes": [
                  32,
                  97,
                  115,
                  115,
                  105,
                  115,
                  116
                ]
              },
              {
                "token": " help",
                "logprob": -7.2507100105285645,
                "bytes": [
                  32,
                  104,
                  101,
                  108,
                  112
                ]
              }
            ]
          },
          {
            "token": " you",
            "logprob": 0.0,
            "bytes": [
              32,
              121,
              111,
              117
            ],
            "top_logprobs": [
              {
                "token": " you",
                "logprob": 0.0,
                "bytes": [
                  32,
                  121,
                  111,
                  117
                ]
              },
              {
                "token": "你",
                "logprob": -17.75,
                "bytes": [
                  228,
                  189,
                  160
                ]
              }
            ]
          },
          {
            "token": " today",
            "logprob": 0.0,
            "bytes": [
              32,
              116,
              111,
              100,
              97,
              121
            ],
            "top_logprobs": [
              {
                "token": " today",
                "logprob": 0.0,
                "bytes": [
                  32,
                  116,
                  111,
                  100,
                  97,
                  121
                ]
              },
              {
                "token": " اليوم",
                "logprob": -20.125,
                "bytes": [
                  32,
                  216,
                  167,
                  217,
                  132,
                  217,
                  138,
                  217,
                  136,
                  217,
                  133
                ]
              }
            ]
          },
          {
            "token": "?",
            "logprob": 0.0,
            "bytes": [
              63
            ],
            "top_logprobs": [
              {
                "token": "?",
                "logprob": 0.0,
                "bytes": [
                  63
                ]
              },
              {
                "token": "?\n",
                "logprob": -17.25,
                "bytes": [
                  63,
                  10
                ]
              }
            ]
          }
        ]
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 9,
    "completion_tokens": 10,
    "total_tokens": 19,
    "prompt_tokens_details": {
      "cached_tokens": 0,
      "audio_tokens": 0
    },
    "completion_tokens_details": {
      "reasoning_tokens": 0,
      "audio_tokens": 0,
      "accepted_prediction_tokens": 0,
      "rejected_prediction_tokens": 0
    }
  },
  "service_tier": "default",
  "system_fingerprint": "fp_06737a9306"
}`,
			resp: nil, // We'll skip the full response validation since it's complex
		},
		{
			name: "empty response",
			json: `{"object":"", "created": 0, "id": "", "model":"","choices":[]}`,
			resp: &v1.ChatCompletionResponse{Choices: []v1.ChatCompletionChoice{}},
		},
		{
			name: "basic response",
			json: `{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"created": 1677858242,
				"model": "gpt-3.5-turbo-0613",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "This is a test response."
						},
						"finish_reason": "stop",
						"logprobs": null
					}
				]
			}`,
			resp: &v1.ChatCompletionResponse{
				ID:      "chatcmpl-123",
				Object:  "chat.completion",
				Created: 1677858242,
				Model:   "gpt-3.5-turbo-0613",
				Choices: []v1.ChatCompletionChoice{
					{
						Index: 0,
						Message: v1.ChatCompletionMessage{
							Role:    "assistant",
							Content: &v1.ChatMessageContent{String: "This is a test response."},
						},
						FinishReason: func() *v1.FinishReason { r := v1.FinishReasonStop; return &r }(),
					},
				},
			},
		},
		{
			name: "with function call",
			json: `{
				"id": "chatcmpl-456",
				"object": "chat.completion",
				"created": 1699807964,
				"model": "gpt-4-1106-preview",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "",
							"tool_calls": [
								{
									"id": "call_abc123",
									"type": "function",
									"function": {
										"name": "get_weather",
										"arguments": "{\"location\":\"San Francisco\",\"unit\":\"celsius\"}"
									}
								}
							]
						},
						"finish_reason": "tool_calls",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 82,
					"completion_tokens": 29,
					"total_tokens": 111
				},
				"system_fingerprint": "fp_44709d6fcb"
			}`,
			resp: &v1.ChatCompletionResponse{
				ID:      "chatcmpl-456",
				Object:  "chat.completion",
				Created: 1699807964,
				Model:   "gpt-4-1106-preview",
				Choices: []v1.ChatCompletionChoice{
					{
						Index: 0,
						Message: v1.ChatCompletionMessage{
							Role:    "assistant",
							Content: &v1.ChatMessageContent{String: ""},
							ToolCalls: []v1.ToolCall{
								{
									ID:   "call_abc123",
									Type: "function",
									Function: v1.FunctionCall{
										Name:      "get_weather",
										Arguments: "{\"location\":\"San Francisco\",\"unit\":\"celsius\"}",
									},
								},
							},
						},
						FinishReason: func() *v1.FinishReason { r := v1.FinishReasonToolCalls; return &r }(),
					},
				},
				Usage: &v1.CompletionUsage{
					PromptTokens:     82,
					CompletionTokens: 29,
					TotalTokens:      111,
				},
				SystemFingerprint: "fp_44709d6fcb",
			},
		},
		{
			name: "response with filter results",
			json: `{
				"id": "chatcmpl-789",
				"object": "chat.completion",
				"created": 1697725363,
				"model": "gpt-4",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "I cannot provide information about that topic."
						},
						"finish_reason": "stop",
						"logprobs": null,
						"content_filter_results": {
							"hate": {
								"filtered": true,
								"severity": "high"
							},
							"self_harm": {
								"filtered": false,
								"severity": "low"
							}
						}
					}
				],
				"usage": {
					"prompt_tokens": 20,
					"completion_tokens": 12,
					"total_tokens": 32
				},
				"prompt_filter_results": [
					{
						"index": 0,
						"content_filter_results": {
							"hate": {
								"filtered": false,
								"severity": "low"
							},
							"self_harm": {
								"filtered": false,
								"severity": "low"
							}
						}
					}
				]
			}`,
			resp: &v1.ChatCompletionResponse{
				ID:      "chatcmpl-789",
				Object:  "chat.completion",
				Created: 1697725363,
				Model:   "gpt-4",
				Choices: []v1.ChatCompletionChoice{
					{
						Index: 0,
						Message: v1.ChatCompletionMessage{
							Role:    "assistant",
							Content: &v1.ChatMessageContent{String: "I cannot provide information about that topic."},
						},
						FinishReason: func() *v1.FinishReason { r := v1.FinishReasonStop; return &r }(),
						ContentFilterResults: &v1.ContentFilterResults{
							Hate: &v1.Hate{
								Filtered: true,
								Severity: "high",
							},
							SelfHarm: &v1.SelfHarm{
								Filtered: false,
								Severity: "low",
							},
						},
					},
				},
				Usage: &v1.CompletionUsage{
					PromptTokens:     20,
					CompletionTokens: 12,
					TotalTokens:      32,
				},
				PromptFilterResults: []v1.PromptFilterResult{
					{
						Index: 0,
						ContentFilterResults: v1.ContentFilterResults{
							Hate: &v1.Hate{
								Filtered: false,
								Severity: "low",
							},
							SelfHarm: &v1.SelfHarm{
								Filtered: false,
								Severity: "low",
							},
						},
					},
				},
			},
		},
		{
			name: "multiple choices response",
			json: `{
				"id": "chatcmpl-multiplechoices",
				"object": "chat.completion",
				"created": 1700010000,
				"model": "gpt-4",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "First response option"
						},
						"finish_reason": "stop",
						"logprobs": null
					},
					{
						"index": 1,
						"message": {
							"role": "assistant",
							"content": "Second response option"
						},
						"finish_reason": "stop",
						"logprobs": null
					},
					{
						"index": 2,
						"message": {
							"role": "assistant",
							"content": "Third response option"
						},
						"finish_reason": "stop",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 15,
					"completion_tokens": 30,
					"total_tokens": 45
				}
			}`,
		},
		{
			name: "response with length finish reason",
			json: `{
				"id": "chatcmpl-length123",
				"object": "chat.completion",
				"created": 1700020000,
				"model": "gpt-4",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "This is a response that reached the maximum token limit..."
						},
						"finish_reason": "length",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 10,
					"completion_tokens": 100,
					"total_tokens": 110
				}
			}`,
		},
		{
			name: "response with content moderation",
			json: `{
				"id": "chatcmpl-moderation123",
				"object": "chat.completion",
				"created": 1700030000,
				"model": "gpt-4",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": ""
						},
						"finish_reason": "content_filter",
						"logprobs": null,
						"content_filter_results": {
							"hate": {
								"filtered": true,
								"severity": "high"
							},
							"self_harm": {
								"filtered": false,
								"severity": "low"
							},
							"sexual": {
								"filtered": false,
								"severity": "low"
							},
							"violence": {
								"filtered": true,
								"severity": "medium"
							}
						}
					}
				],
				"usage": {
					"prompt_tokens": 25,
					"completion_tokens": 0,
					"total_tokens": 25
				}
			}`,
		},
		{
			name: "response with JSON format",
			json: `{
				"id": "chatcmpl-json123",
				"object": "chat.completion",
				"created": 1700040000,
				"model": "gpt-4",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "{\"name\":\"John Doe\",\"age\":30,\"city\":\"New York\",\"skills\":[\"programming\",\"design\",\"writing\"]}"
						},
						"finish_reason": "stop",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 20,
					"completion_tokens": 25,
					"total_tokens": 45
				}
			}`,
		},
		{
			name: "response with function call (deprecated format)",
			json: `{
				"id": "chatcmpl-function123",
				"object": "chat.completion",
				"created": 1700050000,
				"model": "gpt-4",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "",
							"function_call": {
								"name": "get_weather",
								"arguments": "{\"location\":\"Paris\",\"unit\":\"celsius\"}"
							}
						},
						"finish_reason": "function_call",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 30,
					"completion_tokens": 20,
					"total_tokens": 50
				}
			}`,
		},
		{
			name: "response with vision content",
			json: `{
				"id": "chatcmpl-vision123",
				"object": "chat.completion",
				"created": 1700060000,
				"model": "gpt-4-vision",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "The image shows a scenic landscape with mountains and a lake."
						},
						"finish_reason": "stop",
						"logprobs": null
					}
				],
				"usage": {
					"prompt_tokens": 150,
					"completion_tokens": 15,
					"total_tokens": 165
				}
			}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.True(t, stdjson.Valid([]byte(c.json)), "test case should be valid json")

			var resp v1.ChatCompletionResponse
			err := json.Unmarshal([]byte(c.json), &resp)
			require.NoError(t, err, "unmarshal error")

			if c.resp != nil {
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

func requireEqualJSON(t *testing.T, exp, act, msg string) {
	t.Logf("expected json: %s", exp)
	t.Logf("actual json: %s", act)
	require.JSONEq(t, exp, act, msg)
}
