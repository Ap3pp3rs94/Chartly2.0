# Scaling  (Chartly 2.0)

Chartly services are designed to scale horizontally.

## General Rules

- Scale **stateless** services first (gateway, orchestrator)
- Scale **CPU-bound** services by CPU utilization (normalizer, analytics)
- Scale **IO-bound** services by queue latency and saturation (connector-hub)

## HPA

The Helm chart includes an HPA template for each component.
Enable autoscaling per service via values:

```yaml
services:
  gateway:
    autoscaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 10
      targetCPUUtilizationPercentage: 70
```

## Database / Cache

- Postgres and Redis are stateful: scale cautiously.
- Use read replicas for query-heavy workloads.
