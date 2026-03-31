---
title: Codex SSH Login on Remote Servers — Use Loris Tunnel for a Cleaner Port-Forward Workflow
description: A practical codex ssh login guide for remote servers. Learn the SSH port-forward workaround from issue #2668 and how Loris Tunnel makes the flow easier to run daily.
---

# Codex SSH Login on Remote Servers — Use Loris Tunnel for a Cleaner Port-Forward Workflow

If you use Codex from a remote Linux server over SSH, you may hit a frustrating login problem: the auth flow expects a localhost callback, but your browser is on your laptop, not on the server.

The community discussion in [openai/codex issue #2668](https://github.com/openai/codex/issues/2668) documents a practical workaround using SSH local port forwarding. This article explains that pattern and shows how **Loris Tunnel** can make the same `codex ssh` workflow easier to operate.

## The problem in `codex ssh` environments

In a local setup, Codex opens a browser and completes auth through a local callback URL (for example `http://localhost:1455/...`).

On a remote host (SSH session), the callback listener is running on the **server's** localhost. If you open the URL directly on your laptop without forwarding, your browser cannot reach that remote localhost process.

## SSH-forward workaround from the issue discussion

The workaround is simple:

1. Start Codex on the remote server and wait for the login URL.
2. On your local machine, create an SSH local port forward:

```bash
ssh -N -L 1455:localhost:1455 root@YOUR_SERVER
```

3. Copy the full auth URL printed by Codex on the server.
4. Paste it into your local browser manually.
5. Complete login; Codex on the server receives the callback and finishes authentication.

Reference: [Codex login link does not work from SSH server](https://github.com/openai/codex/issues/2668).

## Why this works

`-L 1455:localhost:1455` maps your local port `1455` to `localhost:1455` on the remote server through SSH.

That makes your browser's `http://localhost:1455/...` effectively point to the callback listener on the server, which matches what the login flow expects.

## Where this gets annoying over time

The raw command works, but daily use can become noisy:

- You must keep a dedicated terminal session open.
- You may need custom ports (`9000 -> 1455`) to avoid conflicts.
- Network blips, laptop sleep, or VPN changes can break long-lived forwards.
- Team members on macOS and Windows often maintain different scripts.

For occasional use, that is acceptable. For repeated `codex ssh` operations, a managed tunnel workflow is usually better.

## How Loris Tunnel helps with the same flow

Loris Tunnel does not change the SSH/auth protocol. It manages the tunnel lifecycle in a desktop UI so the workaround is easier to repeat reliably.

| Need in codex ssh workflow | Loris Tunnel value |
| -------------------------- | ------------------ |
| Keep `localhost` forward alive | Auto-reconnect and health visibility for long-running forwards. |
| Reuse the same mapping often | Save tunnel profiles with labels and restart quickly. |
| Deal with non-default SSH ports / jumpers | Configure advanced SSH pathing without remembering long one-liners. |
| Reduce terminal babysitting | Start/stop/inspect forwards from one place. |

## Example profile mapping

If you currently use:

```bash
ssh -p 2222 -N -L 1455:localhost:1455 root@YOUR_SERVER
```

You can model the same path in Loris Tunnel with:

- SSH host: `YOUR_SERVER`
- SSH port: `2222`
- Local bind: `127.0.0.1:1455`
- Remote target: `127.0.0.1:1455`

Then run Codex remotely, copy the printed login URL, and paste it into your local browser as usual.

## Quick troubleshooting checklist

- On server, verify listener:
  - `ss -ltnp | rg 1455`
  - or `netstat -tulnp | rg 1455`
- If local port `1455` is occupied, forward another local port:
  - `ssh -N -L 9000:localhost:1455 root@YOUR_SERVER`
- If SSH runs on a custom port:
  - `ssh -p 2222 -N -L 1455:localhost:1455 root@YOUR_SERVER`
- If the login link opens in a wrong app, copy URL as plain text and paste manually in browser.

## Final takeaway

The SSH-forward workaround from issue #2668 is the right fix for remote `codex ssh` login callbacks. Loris Tunnel builds on that exact pattern and makes it practical for day-to-day use when you want fewer fragile shell sessions and more reliable tunnel operations.
