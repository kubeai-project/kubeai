package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"multi-turn-chat-go/benchmark"
	"net/http"
	"os"
	"time"

	"math/rand"

	"github.com/sashabaranov/go-openai"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		threads string
		format  string
		config  string
	)
	flag.StringVar(&threads, "threads", "", "Path to threads file")
	const (
		formatText = "text"
		formatJSON = "json"
	)
	flag.StringVar(&format, "format", formatText, "Format of results")
	flag.StringVar(&config, "config", "", "Path to config file (use '-' for stdin)")

	// Config-field flags:
	var cfg Config
	var temp float64
	flag.Float64Var(&temp, "temperature", 0, "Temperature to be sent in request")
	flag.IntVar(&cfg.MaxConcurrentThreads, "max-concurrent-threads", 0, "Number of threads to run in parallel - i.e. number of virtual users")
	flag.IntVar(&cfg.ThreadCount, "thread-count", 0, "Number of threads to process over the lifespan of the run")
	flag.IntVar(&cfg.MaxCompletionTokens, "max-completion-tokens", 0, "Number of tokens to generate per request")
	flag.StringVar(&cfg.RequestModel, "request-model", "", "Model field to send in requests")
	var requestTimeout time.Duration
	flag.DurationVar(&requestTimeout, "request-timeout", 0, "Timeout for each request")
	flag.BoolVar(&cfg.NoShuffle, "no-shuffle", false, "Do not shuffle the input dataset")
	flag.Int64Var(&cfg.Seed, "seed", 0, "Random shuffle seed")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Log a lot")

	flag.Parse()
	cfg.Temperature = float32(temp)
	cfg.RequestTimeout = benchmark.Duration(requestTimeout)

	if threads == "" {
		return errors.New("missing required flag: --threads")
	}

	switch format {
	case "text", "json":
	default:
		return fmt.Errorf("invalid format: %q, must be %q or %q",
			format, formatText, formatJSON)

	}

	if config != "" {
		if err := readJSON(config, &cfg); err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	openaiCfg := openai.DefaultConfig(os.Getenv("OPENAI_API_KEY"))
	openaiCfg.BaseURL = os.Getenv("OPENAI_BASE_URL")
	if openaiCfg.BaseURL == "" {
		return fmt.Errorf("missing required environment variable: OPENAI_BASE_URL")
	}
	httpt := http.DefaultTransport.(*http.Transport).Clone()
	httpt.MaxIdleConns = cfg.MaxConcurrentThreads
	httpc := &http.Client{
		Timeout: time.Duration(cfg.RequestTimeout),
	}
	openaiCfg.HTTPClient = httpc
	client := openai.NewClientWithConfig(openaiCfg)

	var inputThreads []benchmark.InputThread
	if err := readJSON(threads, &inputThreads); err != nil {
		return fmt.Errorf("reading input threads: %w", err)
	}

	// Randomize the input dataset (before trimming).
	rnd := rand.New(rand.NewSource(cfg.Seed))
	if cfg.NoShuffle {
		log.Println("Not shuffling dataset threads")
	} else {
		log.Println("Shuffling dataset threads")
		for i := range inputThreads {
			j := rnd.Intn(i + 1)
			inputThreads[i], inputThreads[j] = inputThreads[j], inputThreads[i]
		}
	}

	if cfg.ThreadCount > len(inputThreads) {
		return fmt.Errorf("specified thread count (%d) exceeds total number of threads in the dataset (%d)",
			cfg.ThreadCount, len(inputThreads))
	}
	log.Printf("Trimming dataset threads (%d) to specified thread count (%d)",
		len(inputThreads), cfg.ThreadCount)
	inputThreads = inputThreads[:cfg.ThreadCount]

	runner := benchmark.New(client, cfg.Config, inputThreads)
	result, err := runner.Run()
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	switch format {
	case formatText:
		fmt.Println(result.String())
	case formatJSON:
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding output as json: %w", err)
		}
		fmt.Println(string(out))
	}

	return nil
}

func readJSON(path string, x interface{}) error {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening file %q: %w", path, err)
		}
		defer f.Close()
		r = f
	}

	if err := json.NewDecoder(r).Decode(x); err != nil {
		return fmt.Errorf("decoding as json: %w", err)
	}

	return nil
}

type Config struct {
	benchmark.Config
	RequestTimeout benchmark.Duration `json:"request_timeout"`
	ThreadCount    int                `json:"thread_count"`
	NoShuffle      bool               `json:"no_shuffle"`
	Seed           int64              `json:"seed"`
}

func (c Config) Validate() error {
	if c.RequestTimeout <= 0 {
		return errors.New("request_timeout (--request-timeout) must be greater than 0")
	}
	if c.ThreadCount <= 0 {
		return errors.New("thread_count (--thread-count) is required and must be a positive value")
	}
	return c.Config.Validate()
}
