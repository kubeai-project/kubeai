package v1

import (
	"fmt"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// ResponsesInput is the input field of a Responses API request.
// It can be a plain string or an array of input items.
type ResponsesInput struct {
	String string
	Array  []ResponsesInputItem
}

func (r *ResponsesInput) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		r.String = s
		return nil
	}
	var arr []ResponsesInputItem
	if err := json.Unmarshal(data, &arr); err == nil {
		r.Array = arr
		return nil
	}
	return fmt.Errorf("input must be a string or an array of input items")
}

func (r ResponsesInput) MarshalJSON() ([]byte, error) {
	if r.Array != nil {
		return json.Marshal(r.Array)
	}
	return json.Marshal(r.String)
}

// ResponsesInputItem is a single item in a Responses API input array.
type ResponsesInputItem struct {
	Type    string              `json:"type,omitzero"`
	Role    string              `json:"role,omitzero"`
	Content *ChatMessageContent `json:"content,omitzero"`
	Unknown jsontext.Value      `json:",unknown"`
}

// ResponsesRequest is the request body for POST /v1/responses.
type ResponsesRequest struct {
	Model              string            `json:"model"`
	Input              ResponsesInput    `json:"input"`
	Instructions       string            `json:"instructions,omitzero"`
	PreviousResponseID string            `json:"previous_response_id,omitzero"`
	MaxOutputTokens    *int              `json:"max_output_tokens,omitzero"`
	Stream             bool              `json:"stream,omitzero"`
	Temperature        *float32          `json:"temperature,omitzero"`
	TopP               *float32          `json:"top_p,omitzero"`
	User               string            `json:"user,omitzero"`
	Metadata           map[string]string `json:"metadata,omitzero"`
	Store              bool              `json:"store,omitzero"`
	// Unknown fields are preserved to support backend-specific extensions.
	Unknown jsontext.Value `json:",unknown"`
}

func (r *ResponsesRequest) GetModel() string  { return r.Model }
func (r *ResponsesRequest) SetModel(m string) { r.Model = m }

func (r *ResponsesRequest) Prefix(n int) string {
	if r.Input.String != "" {
		return firstNChars(r.Input.String, n)
	}
	for _, item := range r.Input.Array {
		if item.Role == "user" && item.Content != nil {
			if item.Content.String != "" {
				return firstNChars(item.Content.String, n)
			}
			for _, part := range item.Content.Array {
				if part.Type == ChatMessagePartTypeText {
					return firstNChars(part.Text, n)
				}
			}
		}
	}
	return ""
}
