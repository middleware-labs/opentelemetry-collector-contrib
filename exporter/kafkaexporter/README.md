# Kafka Exporter

<!-- status autogenerated section -->
| Status        |           |
| ------------- |-----------|
| Stability     | [beta]: traces, metrics, logs   |
| Distributions | [core], [contrib] |
| Issues        | [![Open issues](https://img.shields.io/github/issues-search/open-telemetry/opentelemetry-collector-contrib?query=is%3Aissue%20is%3Aopen%20label%3Aexporter%2Fkafka%20&label=open&color=orange&logo=opentelemetry)](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues?q=is%3Aopen+is%3Aissue+label%3Aexporter%2Fkafka) [![Closed issues](https://img.shields.io/github/issues-search/open-telemetry/opentelemetry-collector-contrib?query=is%3Aissue%20is%3Aclosed%20label%3Aexporter%2Fkafka%20&label=closed&color=blue&logo=opentelemetry)](https://github.com/open-telemetry/opentelemetry-collector-contrib/issues?q=is%3Aclosed+is%3Aissue+label%3Aexporter%2Fkafka) |
| [Code Owners](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/CONTRIBUTING.md#becoming-a-code-owner)    | [@pavolloffay](https://www.github.com/pavolloffay), [@MovieStoreGuy](https://www.github.com/MovieStoreGuy) |

[beta]: https://github.com/open-telemetry/opentelemetry-collector#beta
[core]: https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol
[contrib]: https://github.com/open-telemetry/opentelemetry-collector-releases/tree/main/distributions/otelcol-contrib
<!-- end autogenerated section -->

Kafka exporter exports logs, metrics, and traces to Kafka. This exporter uses a synchronous producer
that blocks and does not batch messages, therefore it should be used with batch and queued retry
processors for higher throughput and resiliency. Message payload encoding is configurable.

The following settings are required:
- `protocol_version` (no default): Kafka protocol version e.g. `2.0.0`.

The following settings can be optionally configured:
- `brokers` (default = localhost:9092): The list of kafka brokers.
- `resolve_canonical_bootstrap_servers_only` (default = false): Whether to resolve then reverse-lookup broker IPs during startup.
- `client_id` (default = "sarama"): The client ID to configure the Sarama Kafka client with. The client ID will be used for all produce requests.
- `topic` (default = otlp_spans for traces, otlp_metrics for metrics, otlp_logs for logs): The name of the default kafka topic to export to. See [Destination Topic](#destination-topic) below for more details.
- `topic_from_attribute` (default = ""): Specify the resource attribute whose value should be used as the message's topic. See [Destination Topic](#destination-topic) below for more details. 
- `encoding` (default = otlp_proto): The encoding of the traces sent to kafka. All available encodings:
  - `otlp_proto`: payload is Protobuf serialized from `ExportTraceServiceRequest` if set as a traces exporter or `ExportMetricsServiceRequest` for metrics or `ExportLogsServiceRequest` for logs.
  - `otlp_json`:  payload is JSON serialized from `ExportTraceServiceRequest` if set as a traces exporter or `ExportMetricsServiceRequest` for metrics or `ExportLogsServiceRequest` for logs. 
  - The following encodings are valid *only* for **traces**.
    - `jaeger_proto`: the payload is serialized to a single Jaeger proto `Span`, and keyed by TraceID.
    - `jaeger_json`: the payload is serialized to a single Jaeger JSON Span using `jsonpb`, and keyed by TraceID.
    - `zipkin_proto`: the payload is serialized to Zipkin v2 proto Span.
    - `zipkin_json`: the payload is serialized to Zipkin v2 JSON Span.
  - The following encodings are valid *only* for **logs**.
    - `raw`: if the log record body is a byte array, it is sent as is. Otherwise, it is serialized to JSON. Resource and record attributes are discarded.
- `partition_traces_by_id` (default = false): configures the exporter to include the trace ID as the message key in trace messages sent to kafka. *Please note:* this setting does not have any effect on Jaeger encoding exporters since Jaeger exporters include trace ID as the message key by default.
- `partition_metrics_by_resource_attributes` (default = false)  configures the exporter to include the hash of sorted resource attributes as the message partitioning key in metric messages sent to kafka.
- `partition_logs_by_resource_attributes` (default = false)  configures the exporter to include the hash of sorted resource attributes as the message partitioning key in log messages sent to kafka.
- `auth`
  - `plain_text`
    - `username`: The username to use.
    - `password`: The password to use
  - `sasl`
    - `username`: The username to use.
    - `password`: The password to use
    - `mechanism`: The SASL mechanism to use (SCRAM-SHA-256, SCRAM-SHA-512, AWS_MSK_IAM, AWS_MSK_IAM_OAUTHBEARER or PLAIN)
    - `version` (default = 0): The SASL protocol version to use (0 or 1)
    - `aws_msk.region`: AWS Region in case of AWS_MSK_IAM or AWS_MSK_IAM_OAUTHBEARER mechanism
    - `aws_msk.broker_addr`: MSK Broker address in case of AWS_MSK_IAM mechanism
  - `tls`: see [TLS Configuration Settings](https://github.com/open-telemetry/opentelemetry-collector/blob/main/config/configtls/README.md) for the full set of available options.
    - `ca_file`: path to the CA cert. For a client this verifies the server certificate. Should
      only be used if `insecure` is set to false.
    - `cert_file`: path to the TLS cert to use for TLS required connections. Should
      only be used if `insecure` is set to false.
    - `key_file`: path to the TLS key to use for TLS required connections. Should
      only be used if `insecure` is set to false.
    - `insecure` (default = false): Disable verifying the server's certificate chain and host 
      name (`InsecureSkipVerify` in the tls config)
    - `server_name_override`: ServerName indicates the name of the server requested by the client
      in order to support virtual hosting.
  - `kerberos`
    - `service_name`: Kerberos service name
    - `realm`: Kerberos realm
    - `use_keytab`: Use of keytab instead of password, if this is true, keytab file will be used instead of password
    - `username`: The Kerberos username used for authenticate with KDC
    - `password`: The Kerberos password used for authenticate with KDC
    - `config_file`: Path to Kerberos configuration. i.e /etc/krb5.conf
    - `keytab_file`: Path to keytab file. i.e /etc/security/kafka.keytab
    - `disable_fast_negotiation`: Disable PA-FX-FAST negotiation (Pre-Authentication Framework - Fast). Some common Kerberos implementations do not support PA-FX-FAST negotiation. This is set to `false` by default.
- `metadata`
  - `full` (default = true): Whether to maintain a full set of metadata. When
    disabled, the client does not make the initial request to broker at the
    startup.
  - `retry`
    - `max` (default = 3): The number of retries to get metadata
    - `backoff` (default = 250ms): How long to wait between metadata retries
- `timeout` (default = 5s): Is the timeout for every attempt to send data to the backend.
- `retry_on_failure`
  - `enabled` (default = true)
  - `initial_interval` (default = 5s): Time to wait after the first failure before retrying; ignored if `enabled` is `false`
  - `max_interval` (default = 30s): Is the upper bound on backoff; ignored if `enabled` is `false`
  - `max_elapsed_time` (default = 120s): Is the maximum amount of time spent trying to send a batch; ignored if `enabled` is `false`
- `sending_queue`
  - `enabled` (default = true)
  - `num_consumers` (default = 10): Number of consumers that dequeue batches; ignored if `enabled` is `false`
  - `queue_size` (default = 1000): Maximum number of batches kept in memory before dropping data; ignored if `enabled` is `false`;
  User should calculate this as `num_seconds * requests_per_second` where:
    - `num_seconds` is the number of seconds to buffer in case of a backend outage
    - `requests_per_second` is the average number of requests per seconds.
- `producer`
  - `max_message_bytes` (default = 1000000) the maximum permitted size of a message in bytes
  - `required_acks` (default = 1) controls when a message is regarded as transmitted.   https://pkg.go.dev/github.com/IBM/sarama@v1.30.0#RequiredAcks
  - `compression` (default = 'none') the compression used when producing messages to kafka. The options are: `none`, `gzip`, `snappy`, `lz4`, and `zstd` https://pkg.go.dev/github.com/IBM/sarama@v1.30.0#CompressionCodec
  - `flush_max_messages` (default = 0) The maximum number of messages the producer will send in a single broker request.

Example configuration:

```yaml
exporters:
  kafka:
    brokers:
      - localhost:9092
    protocol_version: 2.0.0
```

## Destination Topic
The destination topic can be defined in a few different ways and takes priority in the following order:
1. When `topic_from_attribute` is configured, and the corresponding attribute is found on the ingested data, the value of this attribute is used.
2. If a prior component in the collector pipeline sets the topic on the context via the `topic.WithTopic` function (from the `github.com/open-telemetry/opentelemetry-collector-contrib/pkg/kafka/topic` package), the value set in the context is used.
3. Finally, the `topic` configuration is used as a default/fallback destination. 
