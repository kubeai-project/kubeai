# How to Configure Kafka Messaging for KubeAI

This guide explains how to configure KubeAI to use Apache Kafka as its messaging system for handling inference requests and responses.

## Prerequisites

*   A running Kafka cluster accessible from your Kubernetes cluster.
*   Two Kafka topics created: one for requests and one for responses.

## Configuration

To use Kafka, you need to configure the `messaging` section in your KubeAI Helm `values.yaml` file.

Here's an example configuration:

```yaml
messaging:
  # Optional: Maximum backoff duration for retrying failed message processing.
  errorMaxBackoff: 30s

  # Define one or more message streams.
  streams:
  - # URL for consuming requests. Format: kafka://<consumer-group>?topic=<request-topic>
    # Replace <consumer-group> with your desired Kafka consumer group ID.
    # Replace <request-topic> with the name of your Kafka topic for requests.
    requestsURL: "kafka://my-kubeai-group?topic=kubeai-requests"

    # URL for publishing responses. Format: kafka://<response-topic>
    # Replace <response-topic> with the name of your Kafka topic for responses.
    responsesURL: "kafka://kubeai-responses"

    # Optional: Maximum concurrent message handlers for this stream.
    maxHandlers: 5000
```

### Parameter Explanations

*   `messaging.errorMaxBackoff`: (Optional) Sets the maximum time KubeAI will wait before retrying to process a message after an error.
*   `messaging.streams`: An array defining different message streams KubeAI can handle.
*   `requestsURL`: The connection string for the request topic.
    *   `kafka://`: Specifies the Kafka protocol.
    *   `<consumer-group>`: Identifies the Kafka consumer group KubeAI will join to read requests. Using a unique group ensures messages are distributed among KubeAI instances if scaled.
    *   `?topic=<request-topic>`: Specifies the Kafka topic from which KubeAI will consume inference requests.
*   `responsesURL`: The connection string for the response topic.
    *   `kafka://`: Specifies the Kafka protocol.
    *   `<response-topic>`: Specifies the Kafka topic to which KubeAI will publish inference responses.
*   `maxHandlers`: (Optional) Controls the maximum number of concurrent requests KubeAI will process from this stream. Adjust based on your expected load and resource availability.
*   **Kafka Client Configuration**: Advanced Kafka client settings (like brokers, SASL authentication, TLS) are configured directly within the `requestsURL` and `responsesURL` using URL parameters, following the [Go CDK Kafka documentation](https://gocloud.dev/pubsub/kafkapubsub/). For example, to specify brokers and SASL: `kafka://my-group?topic=requests&brokers=b1:9092,b2:9092&sasl_mechanism=PLAIN&sasl_user=user&sasl_password=pass`. *Note: For TLS certificates, you might need to mount them as secrets into the KubeAI pod and reference their paths in the URL parameters (e.g., `cacert_path=/path/to/ca.crt`).*
