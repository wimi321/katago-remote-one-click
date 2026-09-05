# Troubleshooting

Start with:

```bash
katago-remote doctor
katago-remote status
katago-remote logs
```

If the command is not in `PATH`, replace it with `~/.local/bin/katago-remote`.

## `nvidia-smi` is missing

The selected server image does not include a working NVIDIA driver. Recreate the instance with the provider's NVIDIA/CUDA image. Installing a driver into an arbitrary container is usually slower and less reliable than selecting the correct image.

## KataGo cannot load CUDA or cuDNN

Choose a CUDA 12 image with cuDNN 9. The installer tries the official cuDNN 9.8 KataGo build first and the official cuDNN 8.9.7 compatibility build second. It does not silently install or replace system GPU libraries.

Run this to preserve the exact diagnostic:

```bash
katago-remote doctor 2>&1 | tee katago-remote-doctor.txt
```

Do not include your complete WSS link in a public report.

## The link worked before a restart

The hostname is provided by a Cloudflare Quick Tunnel and is temporary. Restarting the service or server normally creates a different hostname:

```bash
katago-remote start
katago-remote show
```

Paste the newly displayed link into the client.

## Connection fails immediately

1. Copy the complete link, including `wss://`, `/katago/`, and the final token.
2. Check that `katago-remote status` says `Running`.
3. Confirm that the server can make outbound HTTPS connections.
4. Run `katago-remote restart` and try the new link.

Only one client may be connected. Close the old client before connecting another one.

## The link was accidentally shared

Revoke it immediately:

```bash
katago-remote reset-link
```

This stops the old session, creates a new random token, and prints a new link.

## Uninstall

Stop the service first:

```bash
katago-remote stop
rm -f ~/.local/bin/katago-remote
rm -rf ~/.local/share/katago-remote-one-click
```

Review the paths before running the removal commands if `KATAGO_REMOTE_HOME` was customized.
