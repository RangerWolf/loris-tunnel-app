---
title: OpenClaw Remote Gateway + SSH Tunnels — Why Loris Tunnel Fits Your Stack
description: Pair OpenClaw’s remote gateway with Loris Tunnel for stable local port forwarding to ws://127.0.0.1:18789, auto-reconnect, and a desktop UI—without living in the terminal.
---

# OpenClaw Remote Gateway + SSH Tunnels — Why Loris Tunnel Fits Your Stack

If you run **[OpenClaw](https://openclaws.io/)** with a **remote gateway**, you already rely on **SSH local port forwarding**: your desktop app talks to `ws://127.0.0.1:18789` locally, and an SSH tunnel carries that traffic to the gateway on your server. That pattern is simple, secure, and standard—but it breaks the moment the tunnel drops.

**Loris Tunnel** is a desktop GUI for managing SSH tunnels (local, remote, and dynamic forwarding) with **automatic reconnection** and a clear view of connection health. It is a natural companion when you want OpenClaw’s remote-gateway workflow to feel boringly reliable.

## What OpenClaw expects from your network

According to OpenClaw’s [remote gateway setup](https://openclaws.io/docs/gateway/remote-gateway-readme), the usual shape is:

- On the **client**, OpenClaw connects to **`ws://127.0.0.1:18789`** (a local port).
- **SSH** forwards that local port to **`127.0.0.1:18789` on the remote host** where the gateway listens.

In `~/.ssh/config`, that often looks like a **`LocalForward 18789 127.0.0.1:18789`** on a dedicated host alias, plus a background **`ssh -N`** session—or a Launch Agent on macOS to keep it alive.

That works—until Wi‑Fi flickers, the machine sleeps, the remote host restarts, or you need **multiple jump hosts**. Then you are debugging `lsof`, `ps`, and `launchctl` instead of using the product.

## Where raw `ssh -N` hurts productivity

For OpenClaw users, the tunnel is not “nice to have”; it is **part of the control plane**. When it fails:

- The app cannot reach the gateway until you notice and restart forwarding.
- **Sleep / network changes** on laptops are especially painful.
- **Multi-hop** paths (bastion → internal host) are easy to get wrong in a one-off command.
- Teams on **Windows and macOS** may not share the same shell scripts or Launch Agent snippets.

You do not need to remove SSH from the stack—you need a **consistent way to create, monitor, and recover** the same forwards every day.

## How Loris Tunnel supports the same OpenClaw forward

Loris Tunnel is built for exactly this class of problem: **long-lived SSH port forwards** with operator-friendly controls.

| Need for OpenClaw-style gateways | How Loris Tunnel helps |
| -------------------------------- | ---------------------- |
| Stable **local forward** to remote `127.0.0.1:18789` | Create a tunnel with local port **18789** → remote **127.0.0.1:18789** (or import from an SSH command). |
| Recovery after network blips | **Smart reconnection** with backoff instead of manual restarts. |
| Visibility | **Latency monitoring** and tunnel status in the UI—not only “it broke.” |
| Complex paths | **Multi-hop jumper chains** when the gateway is not on a directly reachable host. |
| SOCKS or other forwards | **Local, remote, and dynamic (SOCKS5)** modes in one app. |

::: tip Map your existing SSH config
If you already use `LocalForward 18789 127.0.0.1:18789`, you can often **paste the equivalent `ssh` command** into Loris Tunnel’s importer and adjust labels, jumpers, and auto-start from there. See the [introduction](./20260316-introduction) for the full feature tour.
:::

## A practical mental model

1. **Gateway stays on the server** (or VM) where OpenClaw expects it—unchanged.
2. **Loris Tunnel** on your laptop or workstation holds the **SSH forward** that exposes the gateway WebSocket on your loopback interface.
3. **OpenClaw.app** keeps using **`ws://127.0.0.1:18789`** locally; it does not care whether `ssh -N` or Loris Tunnel maintains the tunnel—only that the port is reachable and stable.

## Who this is for

- Developers and operators who **live in OpenClaw** but want **less terminal babysitting**.
- Anyone who has already followed OpenClaw’s remote-gateway docs and wants **the same semantics with better resilience**.
- Teams that need **one portable tool** across macOS and Windows for SSH forwarding.

## Get Loris Tunnel

- **Releases**: [GitHub — loris-tunnel-app](https://github.com/RangerWolf/loris-tunnel-app/releases)
- **Product overview**: [Introduction to Loris Tunnel](./20260316-introduction)

::: info About OpenClaw
OpenClaw, its gateway, and port conventions are documented on **[openclaws.io](https://openclaws.io/)**. Loris Tunnel is an independent SSH tunnel manager; pairing them is a common operational pattern, not a bundled vendor integration.
:::

## Summary

**OpenClaw + SSH tunnel** is the right architecture for a **remote gateway**. **Loris Tunnel** makes that architecture **easier to run daily**: visual management, **auto-reconnect**, multi-hop support, and less time spent reviving stale `ssh -N` sessions—so you can focus on the agent, not the pipe.
