# Prometheus Monitoring Safety Device (PromMSD)

tl;dr: The watcher of the watchers for Prometheus and Alertmanager.

## What?

Just like train drivers have a dead man's handle (or dead man's switch, in US
English) this is the same concept for Prometheus and Alertmanager.

One way to think of this is it inverts alerting: If an alert stops being
received this will generate and deliver an alert to alertmanager instances. A
key point to note is this does not have persistent state -- it needs to receive
alerts first, then it will alert for the lack of alerts later.

### Why the name?

A "dead man's" device is more correctly called a ["Driver Safety
Device"][machinerysafety101],this is your safety check for your monitoring, so
it's monitoring safety rather than driver safety.

[machinerysafety101]: https://machinerysafety101.com/2011/03/28/stop-using-the-term-deadman/

### Alternatives

This isn't for everyone:

- Monitor your Prometheus instance from another one
- Use cloud services, like [Dead Man's Snitch](https://deadmanssnitch.com) (see
  [here][dms-howto] for one way to configure it).

[dms-howto]: https://www.noqcks.io/notes/2018/01/29/prometheus-alertmanager-deadmansswitch/

#### Why write this then?

In more complex setups this can be a shared instance, that doesn't require
every Prometheus have an exact pair, or additional configuration elsewhere.
Additionally it can deliver to different alertmanager instances than the
Prometheus instance.

It also provides some level of round-trip testing, i.e. a probe for your
Prometheus and Alertmanager together, and once you have prommsd running within
your infrastructure can easily be enabled with a standard rule on your
Prometheus instances, rather than needing configuration elsewhere.

## Setup

### Alerting rule

Configure an alerting rule to always alert if your instance is healthy. The
simplest is just `expr: 1`. We recommend a slightly more advanced rule (see
below) while checking the "up" status of yourself may seen redundant it
provides a basic end-to-end sanity check of your Prometheus instance
consistency and potentially your service discovery.

```yaml
groups:
  - name: heartbeat
    rules:
      # This alert should always fire
      - alert: ExpectedAlertHeartBeat
        expr: sum(up{job="prometheus"}) by (job, namespace, cluster) > 0
        # Extra safety: if a Prometheus is restarting frequently don't fire straight
        # away. (Ideally you also have something detecting if jobs are restarting, maybe
        # via Kubernetes metrics, but this provides some extra confidence.)
        for: 30s
        labels:
          severity: heartbeat
        annotations:
          summary: |
            Expected alert to test {{ $labels.job }} presence
          # These are parameters, see below...
          msd_identifiers: job namespace cluster
          msd_alertname: NoAlertConnectivity
          msd_override_labels: severity=critical
          msd_activation: 10m
          msd_alertmanagers: |
            http://local.alertmanager
            http://alertmanager1.fully.qualified:xxx
            http://alertmanager2.fully.qualified:xxx
          msda_summary: Alert connectivity from {{ $labels.namespace }} in {{ $labels.cluster }} is degraded.
          msda_description: Alerts aside from this alert may not be delivered. Check Prometheus and Alertmanager health.
```

Parameters:

- `msd_identifiers`: Labels to use to uniquely identify an alert. (Often "job" would
  be enough, but because the dead man's handle is a shared instance you may also
  need to include namespace, or other variables). Space separated.
- `msd_alertname`: Alert name to use for the triggered alert.
- `msd_override_labels`: All the labels from the alert will be copied, but override
  these (key=value string, with space separator).
- `msd_activation`: Duration after no alert is seen to trigger an alert. (Can
  use Go durations, e.g. "60s", "10m".)
- `msd_alertmanagers`: Space separated list of alertmanager URLs. Recommended to
  have your local one here and at least one remote one. On Kubernetes you may wish
  to repeat the same instance as both the in-cluster and out-of-cluster address,
  assuming you can reach the ingress, this can allow you to share the
  configuration between clusters without changes. You can also specify
  `webhook+http://host/...` to directly target a alertmanager compatible
  webhook.
- `msda_NAME`: `NAME` will become an annotation on the generated alert.

The alert that will be raised once `msd_activation` is reached will have all
the labels from the original alert (except "alertname" and "severity") as
labels, in addition to any labels set in `msd_override_labels` overriding those
labels.

Annotations will be added from parameters called `msda_*`, e.g. `msda_summary`
becomes `summary`.

### Sending to a webhook

The recommended configuration is to route the alerts this generates via an
alertmanager, but in some cases it is useful to send straight to a webhook
component. Specifying `webhook+http://webhook/alert` in `msd_alertmanagers` will
allow you to do this.

It is expected this is connected to a system that understands incidents, as it
will repeat notifications frequently.

### Alert routing

In the alertmanager configuration; an alert route that routes
`severity=heartbeat` to a receiver called `prommsd`:

```yaml
route:
  # ...

  routes:
      - match:
          severity: heartbeat
        receiver: prommsd
        # Always set group_interval to 0s, prommsd does its own grouping.
        group_interval: 0s
        repeat_interval: 1m
```

An entry in the receivers section for `prommsd`:

```yaml
receivers:
  - name: prommsd
    webhook_configs:
      - send_resolved: false
        url: http://localhost:9111/alert
```

It is possible to use `continue: true` and deliver to additional prommsd
instances running elsewhere. In general we don't recommend this be instances
far away from this, as that is replacing connectivity monitoring -- use a
blackbox probe instead. In particular this could be very noisy if there is a
connectivity issue whereas a Prometheus alert can be grouped and more easily
silenced.

### Monitoring of the monitor

A rule to make sure this is running (this only needs to be on one Prometheus
instance, e.g. in the Prometheus in Kubernetes namespace that runs this, if
there's a Prometheus instance per namespace):

```yaml
groups:
  - name: prommsd
    rules:
      - alert: JobDown
        expr: up{job="prommsd"} == 0 or absent(up{job="prommsd"})
        for: 5m
        labels:
          severity: critical
        annotations:
           summary: "Expected {{ $labels.job }} to be running to monitor the monitor"
```

## Running

    go build ./cmd/prommsd

    ./prommsd

We hope to provide a Docker image soon.

### Checking

There is a status interface available on the HTTP port. In addition Go's
[x/net/trace](https://godoc.org/golang.org/x/net/trace) is available.

### Metrics

Standard Go metrics are provided.

Overall metrics:

- `prommsd_build_info` has build information.
- `prommsd_alertcheck_monitored_instances` a gauge with the currently monitored
    number of instances.

Alert reception:

- `prommsd_alerthook_received_total` heartbeat alerts received on "/alert"
- `prommsd_alerthook_errors_total` errors handling the heartbeats

Alert sending:

- `prommsd_alertmanager_sent_total` alerts sent to alertmanager (firing and resolved)
- `prommsd_alertmanager_errors_total` errors sending to alertmanager (with a
  `type` label for the kind of failure)

These metrics mostly exist for debugging issues, for prommsd itself we
recommend simply monitoring that the prommsd job is `up` via Prometheus. You
should also monitor your alertmanager is sending alerts separately.

### Limitations

This approach aims to be very simple and all state is stored in memory, this
means a restart of the service will lose the pending alerts. This may sound bad
but actually in many cases isn't a problem -- this is good at noticing a
Prometheus or Alertmanager instance having problems. (Persisting some state
between restarts may be something we consider as it could make sense in some
environments, but in general we believe in those cases a more resilient approach
is to run multiple instances of this instead -- let us know your use cases).
You should consider carefully how this fits your deployment -- if using
Kubernetes a reasonable approach is to run an instance of this inside each
Kubernetes cluster, but (via msd_alertmanagers) able to send alerts to
Alertmanagers outside the cluster. Combined with a federated Prometheus setup
(or blackbox probes) that checks cross cluster connectivity this covers
Prometheus configuration problems while the cluster connectivity is handled by
rules inside your Prometheus.

### Testing

Silence the heartbeat alert (`alertname=ExpectedAlertHeartBeat` if you're using
the configuration from here).

You can then watch the status interface and see that the timer for when the
alert will fire starts counting down, with the alert firing just after the
countdown finishes.

### Maintenance

Silence the paging alert (`alertname=NoAlertConnectivity` if `msd_alertname` is
set as above).

If you're removing an instance there is a delete button on the interface. Make
sure the alert is deleted from Prometheus, so that it doesn't get recreated on
prommsd, then delete it. Currently there is no authorization enforced --
because the alert is regularly repeated, deleting has minimal impact, unless an
outage occurs.

## Development

Contributions are welcome.

The only thing to keep in mind is this aims to have minimal dependencies on
Prometheus and components, in order to provide low dependency alerting about
monitoring system failure. Consider carefully if you add another dependency to
this -- could instead it be done with a Prometheus rule or another external
service?

## Licence

Copyright 2021 G-Research

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
these files except in compliance with the License. You may obtain a copy of the
License at: http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
