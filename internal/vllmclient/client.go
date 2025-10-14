package vllmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	HTTPClient *http.Client
}

type LoadAdapterRequest struct {
	LoraName string `json:"lora_name"`
	LoraPath string `json:"lora_path"`

	Options LoadAdapterRequestOptions `json:"-"`
}

type LoadAdapterRequestOptions struct {
	IgnoreAlreadyLoaded bool
}

// Load a LoRa adapter into the VLLM model server.
// See: https://docs.vllm.ai/en/latest/models/lora.html#dynamically-serving-lora-adapters
func (c *Client) LoadLoraAdapter(ctx context.Context, addr string, req LoadAdapterRequest) error {
	if err := c.post(ctx, addr, "/v1/load_lora_adapter", req, nil, func(status int, resp errorResponse) error {
		// example: {"error":{"message":"Call to add_lora method failed: Loading lora sql_adapter failed: No adapter found for /path/to/sql-lora-adapter","type":"NotFoundError","param":null,"code":404}}
		if req.Options.IgnoreAlreadyLoaded {
			if status == 400 && resp.VllmError.Type == "InvalidUserInput" &&
				strings.Contains(resp.VllmError.Message, "already") &&
				strings.Contains(resp.VllmError.Message, "loaded") {
				return nil
			}
		}
		return fmt.Errorf("unexpected status code: %d: %s", status, resp.VllmError.Message)
	}); err != nil {
		return err
	}
	return nil
}

type UnloadAdapterRequest struct {
	LoraName string `json:"lora_name"`

	Options UnloadAdapterRequestOptions `json:"-"`
}

type UnloadAdapterRequestOptions struct {
	IgnoreNotFound bool
}

// Unload a LoRa adapter from the VLLM model server.
// See: https://docs.vllm.ai/en/latest/models/lora.html#dynamically-serving-lora-adapters
func (c *Client) UnloadLoraAdapter(ctx context.Context, addr string, req UnloadAdapterRequest) error {
	if err := c.post(ctx, addr, "/v1/unload_lora_adapter", req, nil, func(status int, resp errorResponse) error {
		// example: {"object":"error","message":"The lora adapter 'xyzabc' cannot be found.","type":"InvalidUserInput","param":null,"code":400}
		if req.Options.IgnoreNotFound {
			if status == 400 && resp.VllmError.Type == "InvalidUserInput" &&
				strings.Contains(resp.VllmError.Message, "cannot be found") {
				return nil
			}
		}
		return fmt.Errorf("unexpected status code: %d: %s", status, resp.VllmError.Message)
	}); err != nil {
		return err
	}
	return nil
}

type vllmError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type errorResponse struct {
	VllmError vllmError `json:"error"`
}

func (c *Client) post(ctx context.Context, addr string, path string, req, resp interface{}, errorHandler func(status int, resp errorResponse) error) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshalling body as json: %w", err)
	}

	url := addr + path
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending http request: POST %s: %w", url, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode > 299 {
		if errorHandler != nil {
			var errResp errorResponse
			if err := json.NewDecoder(httpResp.Body).Decode(&errResp); err != nil {
				return fmt.Errorf("decoding error response body: %w", err)
			}
			return errorHandler(httpResp.StatusCode, errResp)
		}
		respBody, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("unexpected status code: POST %s: %d: %s", url, httpResp.StatusCode, string(respBody))
	}

	if resp != nil {
		if err := json.NewDecoder(httpResp.Body).Decode(resp); err != nil {
			return fmt.Errorf("decoding response body: %w", err)
		}
	}

	return nil
}
