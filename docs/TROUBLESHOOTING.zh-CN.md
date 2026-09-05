# 故障排查

先运行下面三条命令：

```bash
katago-remote doctor
katago-remote status
katago-remote logs
```

如果提示找不到命令，把 `katago-remote` 换成 `~/.local/bin/katago-remote`。

## 提示找不到 `nvidia-smi`

服务器系统镜像没有可用的 NVIDIA 驱动。最省事的办法是重新选择云厂商提供的 NVIDIA/CUDA 镜像；在普通容器中临时安装驱动通常更慢，也更容易失败。

## KataGo 提示缺少 CUDA 或 cuDNN

优先选择带 CUDA 12 和 cuDNN 9 的镜像。安装器会先试官方 cuDNN 9.8 引擎，再试官方 cuDNN 8.9.7 兼容引擎；它不会静默安装或替换服务器的系统显卡库。

用下面命令保存完整诊断：

```bash
katago-remote doctor 2>&1 | tee katago-remote-doctor.txt
```

公开求助时不要附上完整 WSS 链接。

## 重启以后原链接不能用了

域名来自 Cloudflare 临时通道，服务或服务器重启后通常会变化。重新运行：

```bash
katago-remote start
katago-remote show
```

把新显示的链接粘贴到软件中。

## 软件立即提示连接失败

1. 确认复制了完整链接，包括 `wss://`、`/katago/` 和最后的私密令牌。
2. 运行 `katago-remote status`，确认显示 `Running`。
3. 确认服务器可以访问外网 HTTPS。
4. 运行 `katago-remote restart`，使用新链接重试。

同一时间只允许一个客户端连接。请先关闭旧客户端，再连接另一台电脑。

## 不小心公开了链接

立即运行：

```bash
katago-remote reset-link
```

旧会话会停止，旧令牌立即失效，并生成新的私密链接。

## 卸载

```bash
katago-remote stop
rm -f ~/.local/bin/katago-remote
rm -rf ~/.local/share/katago-remote-one-click
```

如果设置过 `KATAGO_REMOTE_HOME`，删除前请先核对实际目录。
