package v1

import (
	"encoding/json/jsontext"
)

// ClassifyRequest represents a request to score texts with a sequence
// classification model, as served by Infinity at /v1/classify.
type ClassifyRequest struct {
	Input any    `json:"input"`
	Model string `json:"model"`

	// Unknown fields should be preserved to fully support engines that accept
	// extended parameters.
	Unknown jsontext.Value `json:",embed"`
}

func (r *ClassifyRequest) GetModel() string  { return r.Model }
func (r *ClassifyRequest) SetModel(m string) { r.Model = m }
