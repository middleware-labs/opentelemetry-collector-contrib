To enable this receiver, add the following in the receivers field of otel-config.yaml.

```yaml
receivers:
  apachedruid:
    endpoint: localhost:8081
    read_timeout: 60s
```

and under `services.pipelines.metrics`, add the `apachedruid` receiver. 

```yaml
receivers:
- apachedruid
```


