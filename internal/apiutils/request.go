package apiutils

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	"github.com/go-json-experiment/json"

	"context"

	"github.com/google/uuid"
	k8sv1 "github.com/substratusai/kubeai/api/k8s/v1"
	openaiv1 "github.com/substratusai/kubeai/api/openai/v1"
)

var (
	ErrBadRequest    = fmt.Errorf("bad request")
	ErrModelNotFound = fmt.Errorf("model not found")
)

// modelRequest represents a request that will be made to a given model.
type modelRequest interface {
	GetModel() string
	SetModel(string)
}

// inferenceRequest should be implemented by inference requests so that
// prefixes can be examined to make routing decisions.
type inferenceRequest interface {
	Prefix(int) string
}

type Request struct {
	Body         []byte
	modelRequest modelRequest

	Selectors []string

	ID string

	// RequestedModel is the model name requested by the client.
	// This might contain the adapter name as well.
	RequestedModel string

	Model   string
	Adapter string

	LoadBalancing k8sv1.LoadBalancing

	Prefix string

	// RoutingKey is the value from the Routing-Key HTTP header (case-insensitive)
	RoutingKey string

	ContentLength int64
}

type ModelClient interface {
	LookupModel(ctx context.Context, model, adapter string, selectors []string) (*k8sv1.Model, error)
}

func ParseRequest(ctx context.Context, client ModelClient, body io.Reader, path string, headers http.Header) (*Request, error) {
	r := &Request{
		ID: uuid.New().String(),
	}

	r.Selectors = headers.Values("X-Label-Selector")
	r.RoutingKey = headers.Get("Routing-Key")

	// Parse media type (with params - which are used for multipart form data)
	var (
		contentType = headers.Get("Content-Type")
		mediaType   string
		mediaParams map[string]string
	)
	if contentType == "" {
		mediaType = "application/json"
		mediaParams = map[string]string{}
	} else {
		var err error
		mediaType, mediaParams, err = mime.ParseMediaType(contentType)
		if err != nil {
			return nil, fmt.Errorf("%w: parse media type: %w", ErrBadRequest, err)
		}
	}

	switch mediaType {
	// Multipart form data is used for endpoints that accept file uploads:
	case "multipart/form-data":
		if err := r.readyMultiPartBody(body, mediaParams); err != nil {
			return nil, fmt.Errorf("%w: reading multipart form data: %w", ErrBadRequest, err)
		}

	// Assume "application/json":
	default:
		if err := r.readJSONBody(body, path); err != nil {
			return nil, fmt.Errorf("%w: reading model from body: %w", ErrBadRequest, err)
		}
	}

	if err := r.lookupModel(ctx, client, path); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Request) readyMultiPartBody(body io.Reader, mediaParams map[string]string) error {
	boundary := mediaParams["boundary"]
	if boundary == "" {
		return fmt.Errorf("no boundary specified in multipart form data")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// Keep the same boundary as the initial request (probably not necessary)
	mw.SetBoundary(boundary)

	// Iterate over the parts of the multipart form data:
	// - If the part is named "model", save the value to the proxy request.
	// - Otherwise, just copy the part to the new multipart writer.
	mr := multipart.NewReader(body, boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("interating over multipart form: %w", err)
		}

		if p.FormName() == "model" {
			value, err := io.ReadAll(p)
			if err != nil {
				return fmt.Errorf("reading multipart form value: %w", err)
			}
			r.Model, r.Adapter = SplitModelAdapter(string(value))
			r.RequestedModel = string(value)
			// WORKAROUND ALERT:
			// Omit the "model" field from the proxy request to avoid FasterWhisper validation issues:
			// See https://github.com/fedirz/faster-whisper-server/issues/71
			continue
		}

		// Copy the part to the new multipart writer.
		pp, err := mw.CreatePart(p.Header)
		if err != nil {
			return fmt.Errorf("creating part: %w", err)
		}
		if _, err := io.Copy(pp, p); err != nil {
			return fmt.Errorf("copying part: %w", err)
		}
	}

	// Fully write to buffer.
	if err := mw.Close(); err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}
	r.Body = buf.Bytes()
	// Set a new content length based on the new body - which had the "model" field removed.
	r.ContentLength = int64(len(r.Body))

	return nil
}

func (r *Request) readJSONBody(body io.Reader, path string) error {
	switch path {
	case "/v1/completions":
		r.modelRequest = &openaiv1.CompletionRequest{}
	case "/v1/chat/completions":
		r.modelRequest = &openaiv1.ChatCompletionRequest{}
	case "/v1/embeddings":
		r.modelRequest = &openaiv1.EmbeddingRequest{}
	default:
		return fmt.Errorf("unknown path: %q", path)
	}

	if err := json.UnmarshalRead(body, r.modelRequest); err != nil {
		return fmt.Errorf("decoding: %w", err)
	}

	if r.modelRequest.GetModel() == "" {
		return errors.New("missing 'model' field")
	}

	r.RequestedModel = r.modelRequest.GetModel()
	r.Model, r.Adapter = SplitModelAdapter(r.RequestedModel)

	if r.Adapter != "" {
		// vLLM expects the adapter to be in the model field.
		r.modelRequest.SetModel(r.Adapter)
	}

	rewritten, err := json.Marshal(r.modelRequest)
	if err != nil {
		return fmt.Errorf("remarshalling: %w", err)
	}
	r.Body = rewritten
	r.ContentLength = int64(len(r.Body))

	return nil
}

func (r *Request) lookupModel(ctx context.Context, client ModelClient, path string) error {
	model, err := client.LookupModel(ctx, r.Model, r.Adapter, r.Selectors)
	if err != nil {
		return fmt.Errorf("lookup model: %w", err)
	}
	if model == nil {
		return fmt.Errorf("%w: %q", ErrModelNotFound, r.RequestedModel)
	}

	r.LoadBalancing = model.Spec.LoadBalancing

	if infReq, ok := r.modelRequest.(inferenceRequest); ok {
		if r.LoadBalancing.Strategy == k8sv1.PrefixHashStrategy && r.modelRequest != nil {
			r.Prefix = infReq.Prefix(r.LoadBalancing.PrefixHash.PrefixCharLength)
		}
	}

	return nil
}

// firstNChars returns the first n characters of a string.
// This function is needed because Go's string indexing is based on bytes, not runes.
func firstNChars(s string, n int) string {
	runes := []rune(s)
	return string(runes[:min(n, len(runes))])
}
