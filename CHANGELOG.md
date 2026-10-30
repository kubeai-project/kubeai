## v0.25.0 - 2026-10-30

- docs: fix dead OpenAI OpenAPI spec and benchmark script links by @sarankumar1325 in https://github.com/kubeai-project/kubeai/pull/785
- feat: route /v1/classify to model servers by @namrastanov in https://github.com/kubeai-project/kubeai/pull/783
- build(deps): bump golang from 1.27.2 to 1.27.3 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/781
- Enhance SECURITY.md with detailed security policy by @m4oc in https://github.com/kubeai-project/kubeai/pull/773
- feat: support Kubernetes 1.37 with aligned client libraries by @m4oc in https://github.com/kubeai-project/kubeai/pull/772
- fix: refresh endpoint addresses by @spicysharkco in https://github.com/kubeai-project/kubeai/pull/766
- fix: update Grafana dashboard for vLLM V1 metrics by @m4oc in https://github.com/kubeai-project/kubeai/pull/760
- docs: add vLLM Whisper transcription example by @m4oc in https://github.com/kubeai-project/kubeai/pull/759
- build: upgrade Go to 1.27.2 and migrate to standard JSON v2 by @m4oc in https://github.com/kubeai-project/kubeai/pull/758
- feat(models): add Spark-X2.5-4B model configs for SGLang and vLLM engines by @dongjiang1989 in https://github.com/kubeai-project/kubeai/pull/755
- docs: add SGLang and llama.cpp NVIDIA GPU examples by @Andreh7777 in https://github.com/kubeai-project/kubeai/pull/754
- Feat/api endpoint migration by @m4oc in https://github.com/kubeai-project/kubeai/pull/753

## v0.24.0 - 2026-10-07

- Feat/systemone api by @m4oc in https://github.com/kubeai-project/kubeai/pull/745
- ci: fix gh-release-workflow by @ffais in https://github.com/kubeai-project/kubeai/pull/741
- feat(chart): optional runtime AI inventory via k8s-aibom subchart (default off) by @glenmessenger in https://github.com/kubeai-project/kubeai/pull/739
- feat: pull oci:// models through llmman serve by @ericcurtin in https://github.com/kubeai-project/kubeai/pull/728
- add SGLang and llama.cpp inference engines by @krim404 in https://github.com/kubeai-project/kubeai/pull/727
- Controller watch scope by @DavideRutigliano in https://github.com/kubeai-project/kubeai/pull/692

## v0.23.5 - 2026-09-30

- Fix/otel autoscaling cardinality by @m4oc in https://github.com/kubeai-project/kubeai/pull/735
- fix: execute Ollama preparation before startup probe succeeds by @ruslan-shaydullin in https://github.com/kubeai-project/kubeai/pull/733

## v0.23.4 - 2026-07-30

- build(deps): bump github.com/onsi/ginkgo/v2 from 2.22.0 to 2.32.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/708
- build(deps): bump github.com/prometheus/client_golang from 1.22.0 to 1.24.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/707
- build(deps): bump gocloud.dev from 0.43.0 to 0.46.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/706
- build(deps): bump go.opentelemetry.io/otel/metric from 1.43.0 to 1.44.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/705
- build(deps): bump the actions-all group with 2 updates by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/704
- ci: add milestone closing step to release workflow by @ffais in https://github.com/kubeai-project/kubeai/pull/703
- ci: add missing checkout step in GitHub release workflow by @ffais in https://github.com/kubeai-project/kubeai/pull/702
- refactor: clean up release workflow by removing outdated comments and adding steps for chart publishing as oci artifact by @ffais in https://github.com/kubeai-project/kubeai/pull/701
- build(deps): bump the actions-all group across 1 directory with 7 updates by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/697
- Bump golang.org/x/crypto from 0.45.0 to 0.52.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/688
- build(deps): bump golang.org/x/net from 0.54.0 to 0.55.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/686
- Bump alpine from 3.22 to 3.24 in /components/model-loader by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/666
- build(deps): bump google.golang.org/grpc from 1.73.0 to 1.79.3 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/662
- Bump k8s.io/api from 0.34.1 to 0.34.2 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/625

## v0.23.4 - 2026-07-30

- build(deps): bump github.com/onsi/ginkgo/v2 from 2.22.0 to 2.32.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/708
- build(deps): bump github.com/prometheus/client_golang from 1.22.0 to 1.24.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/707
- build(deps): bump gocloud.dev from 0.43.0 to 0.46.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/706
- build(deps): bump go.opentelemetry.io/otel/metric from 1.43.0 to 1.44.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/705
- build(deps): bump the actions-all group with 2 updates by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/704
- ci: add milestone closing step to release workflow by @ffais in https://github.com/kubeai-project/kubeai/pull/703
- ci: add missing checkout step in GitHub release workflow by @ffais in https://github.com/kubeai-project/kubeai/pull/702
- refactor: clean up release workflow by removing outdated comments and adding steps for chart publishing as oci artifact by @ffais in https://github.com/kubeai-project/kubeai/pull/701
- build(deps): bump the actions-all group across 1 directory with 7 updates by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/697
- Bump golang.org/x/crypto from 0.45.0 to 0.52.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/688
- build(deps): bump golang.org/x/net from 0.54.0 to 0.55.0 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/686
- Bump alpine from 3.22 to 3.24 in /components/model-loader by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/666
- build(deps): bump google.golang.org/grpc from 1.73.0 to 1.79.3 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/662
- Bump k8s.io/api from 0.34.1 to 0.34.2 by @app/dependabot in https://github.com/kubeai-project/kubeai/pull/625

## v0.23.3 - 2026-07-17

- ci: improve image build workflow by @ffais in https://github.com/kubeai-project/kubeai/pull/696
- fix: include api/ package in unit test coverage by @DavideRutigliano in https://github.com/kubeai-project/kubeai/pull/672
- fix: exclude terminating pods from rollout plan calculation by @reyshazni in https://github.com/kubeai-project/kubeai/pull/659

