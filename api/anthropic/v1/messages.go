package v1

import (
	"strings"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// MessagesRequest is the routing envelope for Anthropic's Messages API.
// The engine validates the full payload; retain extensions and content blocks
// without translating them into OpenAI messages.
type MessagesRequest struct {
	Model   string         `json:"model"`
	Unknown jsontext.Value `json:",unknown"`
}

func (r *MessagesRequest) GetModel() string      { return r.Model }
func (r *MessagesRequest) SetModel(model string) { r.Model = model }

// Prefix follows chat routing: use text from the first user message, skipping
// image and tool blocks. This inspection never changes the forwarded payload.
func (r *MessagesRequest) Prefix(n int) string {
	if n <= 0 {
		return ""
	}
	var envelope struct {
		Messages []struct {
			Role    string         `json:"role"`
			Content jsontext.Value `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(r.Unknown, &envelope); err != nil {
		return ""
	}
	for _, message := range envelope.Messages {
		if message.Role != "user" {
			continue
		}
		var text string
		if err := json.Unmarshal(message.Content, &text); err != nil {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(message.Content, &blocks); err != nil {
				return ""
			}
			var b strings.Builder
			for _, block := range blocks {
				if block.Type == "text" {
					b.WriteString(block.Text)
				}
			}
			text = b.String()
		}
		runes := []rune(text)
		return string(runes[:min(n, len(runes))])
	}
	return ""
}
