Pier watches what happens **after** the deploy and warns you through the channels you choose. The idea is that you learn about the problem before your user does.

## Notification channels

In **Notifications**, register one or more channels:

| Channel | What to enter |
|---|---|
| Discord | Webhook URL of the channel |
| Slack | URL of the *incoming webhook* |
| Telegram | Bot token and chat id |
| Email (SMTP) | Server, port, user, password and recipients. TLS can be required |
| Webhook | Any URL that receives a JSON POST |

In each channel, choose the events it receives and use **Test** to check it.

## What raises an alert

| Situation | When it fires |
|---|---|
| Deploy failed or finished | At each deploy (you choose which) |
| Container restarting in a loop | 3 restarts or more in 10 minutes |
| Container killed for lack of memory | When Docker records `OOMKilled` |
| Failing health check | When the container becomes `unhealthy` after the deploy |
| High disk, memory or CPU | Above 90%, 90% and 95% for 10 minutes (editable) |
| Certificate with a problem | Invalid, self-signed or expiring in less than 14 days |
| Backup or task that did not run | 10 minutes after the expected time without a successful run |
| Restore test failed | When the weekly test of a database fails |
| Site down | When an uptime check fails |
| Server unreachable | When Pier loses contact with a server |

"Did not run" is different from "failed". Pier also warns you when a scheduled backup simply did not happen.

## Without flooding the inbox

- **No repetition:** while an alert stays active, the message is only repeated as a reminder every 6 hours.
- **Recovery:** when the problem goes away, you get a single message saying it is back to normal.
- **Silence:** during maintenance, silence everything or one specific resource, for up to 30 days. When the silence ends, Pier evaluates the situation again.
- The state of the alerts survives a Pier restart: you do not receive everything again.

## Uptime check

In **Infrastructure › Monitoring**, create a check for an application or stack:

- **URL:** one of the published domains.
- **Expected status:** default 200.
- **Interval:** from 1 minute to 1 day.

Each result is kept in a 7-day history. The check starts from the Pier server itself. To know whether the whole server went down, also use an external monitor.

## Live monitoring

In **Infrastructure › Monitoring**, each server shows:

- CPU;
- memory used and available;
- disk of the data directory and swap;
- uptime, load average and containers.

The readings update every 15 seconds while the page is open. The limits of the capacity alerts are edited on the same screen.


## Metrics history

The server and resource charts let you choose a time window. Pier collects samples in the background; the page does not need to stay open. One-minute samples are kept for 24 hours; longer periods use 15-minute aggregates, kept for 30 days. Aggregated series show average and maximum when available.

A gap means a missing reading, an unreachable server or an unsupported metric; it does not mean zero consumption. Resources with several containers are aggregated. In applications with [multiple targets](/en/docs/multi-server/), CPU and memory explicitly add up the configured targets, without depending on the collection order. Each field only has a total if all targets have that reading; a partial collection leaves a gap. Host metrics remain separate, and container CPU can go above 100% when it uses several cores. Old points, from before this fix, may represent only one target; overwritten values cannot be recovered. It does not replace external monitoring of Pier itself. For logs in external storage, see [server operations](/en/docs/server-operations/#log-shipping).
