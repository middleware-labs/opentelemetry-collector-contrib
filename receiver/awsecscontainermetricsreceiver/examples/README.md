# ECS Daemon Agent Deployment

This folder contains example configurations for deploying the `awsecscontainermetrics` receiver as a **daemon agent**—one collector task per EC2 instance in your ECS cluster.

## Do You Need an ECS Service?

**Yes.** To run the collector as a daemon (one task per EC2 instance), you must create an **ECS Service** with `schedulingStrategy: DAEMON`. The service:

- Places exactly one task on each active EC2 instance in the cluster
- Keeps tasks running and restarts them if they stop
- Automatically adds new tasks when you scale up (add instances)
- Removes tasks when instances are terminated

Without a service, you would have to manually run a task on each instance.

**Note:** The DAEMON scheduling strategy does **not** work with Fargate. You must use EC2 launch type.

---

## Prerequisites

1. **ECS cluster** with EC2 capacity (not Fargate)
2. **IAM roles:**
   - **Task execution role** – for pulling images and writing logs
   - **Task role** – for ECS/EC2 API calls (see permissions below)
3. **Config file** – collector config available at `/etc/otel/config.yaml` inside the container

---

## IAM Permissions (Task Role)

The task role needs the following ECS permissions for daemonset mode. A ready-to-use policy is in `iam-policy-ecs-daemon.json`.

| Action | Purpose |
|--------|---------|
| `ecs:ListTasks` | List running tasks in the cluster |
| `ecs:DescribeTasks` | Get task details (including instance-local tasks) |
| `ecs:ListServices` | List services in the cluster |
| `ecs:DescribeServices` | Get service and deployment details |
| `ecs:DescribeTaskDefinition` | Resolve task definition (family, limits) |

Instance IP for ECS agent/cgroups/Docker is read from the EC2 instance metadata service (no IAM required). No `ec2:*` permissions are needed for this receiver.

---

## Collector Config

Create a config file (e.g. `config.yaml`) with the daemonset receiver:

```yaml
receivers:
  awsecscontainermetrics:
    collection_interval: 60s
    run_as: daemonset
    cluster: YOUR_ECS_CLUSTER_NAME
    region: us-east-1
    docker_socket_path: /var/run/docker.sock
    cgroups_mount_path: /host/sys/fs/cgroup

exporters:
  awsemf:
    namespace: ECS/ContainerInsights
    log_group_name: '/aws/ecs/containerinsights'
  logging:
    verbosity: basic

service:
  pipelines:
    metrics:
      receivers: [awsecscontainermetrics]
      exporters: [awsemf, logging]
```

**Config delivery options:**

1. **Custom image** – Build a Docker image with `config.yaml` baked in; set `command` to point at it
2. **EC2 user data** – Have user data write the config to `/opt/otel-config/config.yaml` before the ECS agent starts
3. **EFS** – Mount an EFS volume with the config (replace the `otel-config` host volume with EFS in the task definition)

---

## Task Definition

Edit `ecs-daemon-task-definition.json`:

- Replace `YOUR_ACCOUNT_ID` with your AWS account ID
- Replace `us-east-1` with your region
- Ensure the `otel-config` volume points to where your config lives (host path or EFS)

**Key settings:**

- `requiresCompatibilities: ["EC2"]` – must use EC2
- Host volumes for `/var/run/docker.sock` and `/sys/fs/cgroup` – required for full metrics
- `networkMode: awsvpc` – recommended for EC2

---

## Service Definition

Edit `ecs-daemon-service.json`:

- Replace `YOUR_ECS_CLUSTER_NAME` with your cluster name
- Replace `subnets` and `securityGroups` with your VPC subnets and security group

---

## Deployment

```bash
# Register task definition
aws ecs register-task-definition --cli-input-json file://ecs-daemon-task-definition.json

# Create the daemon service
aws ecs create-service --cli-input-json file://ecs-daemon-service.json
```

Or use the AWS Console: create the task definition, then create a service with **DAEMON** scheduling strategy.

---

## Security Group

The task needs:

- **Outbound** – HTTPS (443) for ECS/EC2 APIs, CloudWatch, etc.
- **Inbound** – Only if other tasks send metrics to this collector (e.g. OTLP port 4317)

---

## Verifying

1. Check the service: `aws ecs describe-services --cluster YOUR_CLUSTER --services otel-ecs-metrics-agent`
2. Confirm one task per instance: `aws ecs list-tasks --cluster YOUR_CLUSTER --service-name otel-ecs-metrics-agent`
3. Check CloudWatch Logs for collector output
