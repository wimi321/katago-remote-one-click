<div align="center">

# KataGo 远程算力一键部署

**让 Linux NVIDIA GPU 服务器一条命令变成可直接粘贴到围棋软件的私密 WSS 算力。**

[![CI](https://github.com/wimi321/katago-remote-one-click/actions/workflows/ci.yml/badge.svg)](https://github.com/wimi321/katago-remote-one-click/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/wimi321/katago-remote-one-click)](https://github.com/wimi321/katago-remote-one-click/releases/latest)
[![License](https://img.shields.io/github/license/wimi321/katago-remote-one-click)](LICENSE)

[English](README.md) | [故障排查](docs/TROUBLESHOOTING.zh-CN.md) | [安全说明](SECURITY.md)

</div>

不需要配置防火墙、域名、证书、Python 环境或 Cloudflare 账号。安装器会自动准备 KataGo、官方 Transformer 权重、安全连接服务和加密临时通道，最后直接显示可用的 WSS 链接与二维码。

## 一条命令完成

准备一台 **Linux x86_64 + NVIDIA 显卡**的服务器。租用服务器时优先选择带 **CUDA 12 + cuDNN 9** 的系统镜像，然后在终端运行：

```bash
curl -fsSL https://raw.githubusercontent.com/wimi321/katago-remote-one-click/v0.1.0/install.sh | bash
```

首次安装会下载数百 MB 文件，耗时取决于服务器网络。每个文件使用前都会核对固定 SHA-256，下载损坏或内容被替换时会立即停止。

安装成功后会看到类似下面的私密链接和二维码：

```text
wss://example-name.trycloudflare.com/katago/<私密令牌>
```

打开 LizzieYzy Next 的 **远程算力 > 自建算力**，粘贴完整链接，再点击 **一键启用自建算力**；也可以直接导入生成的二维码。

> 完整 WSS 链接相当于密码，不要发到群聊、论坛、Issue 或公开截图中。拿到链接的人可以在服务运行期间使用这台 KataGo。

## Google Colab

没有自己的 GPU 服务器时，可以使用带步骤说明的 Colab 笔记本。Colab 是否提供 GPU、GPU 型号和使用时长都由 Google 决定，本项目无法保证。

[![在 Colab 中打开](https://colab.research.google.com/assets/colab-badge.svg)](https://colab.research.google.com/github/wimi321/katago-remote-one-click/blob/v0.1.0/colab/KataGo_Remote_One_Click.ipynb)

## 安装了什么

| 组件 | 固定版本 | 用途 |
| --- | --- | --- |
| KataGo | 1.18.2 CUDA 12.1 | 官方分析引擎 |
| Transformer 10B | 官方中型权重 | 棋力、速度和显存占用更均衡的默认模型 |
| cloudflared | 2026.8.3 | 建立加密临时 WSS 通道 |
| katago-remote | 0.1.0 | 安全协议桥接、链接生成和启停管理 |

文件默认放在 `~/.local/share/katago-remote-one-click`。服务只监听服务器自身的 `127.0.0.1`，不会要求开放公网入站端口；WSS 路径使用随机 256 位令牌，一次只允许一个客户端连接。

## 日常使用

```bash
katago-remote show        # 重新显示当前链接和二维码
katago-remote status      # 查看是否正在运行
katago-remote logs        # 查看最近的错误信息
katago-remote restart     # 重启 KataGo，并生成新的临时域名
katago-remote stop        # 停止引擎和连接
katago-remote reset-link  # 立即废除旧令牌，生成新的私密链接
katago-remote doctor      # 检查显卡、文件和 KataGo 是否能启动
```

如果新开的终端提示找不到 `katago-remote`，使用完整命令：

```bash
~/.local/bin/katago-remote show
```

`trycloudflare.com` 域名是临时的，服务重启或服务器重启后通常会变化。重新运行 `katago-remote show`，把新链接粘贴到 LizzieYzy Next 即可。

## 使用已有引擎或权重

懂参数的用户可以让安装器直接使用服务器上已有的文件。先单独下载安装器，确保下面的变量传给 Bash，而不是只传给 `curl`：

```bash
curl -fsSLo /tmp/katago-remote-install.sh \
  https://raw.githubusercontent.com/wimi321/katago-remote-one-click/v0.1.0/install.sh

KATAGO_REMOTE_KATAGO_PATH=/opt/katago/katago \
KATAGO_REMOTE_MODEL_PATH=/opt/katago/model.bin.gz \
KATAGO_REMOTE_CONFIG_PATH=/opt/katago/analysis.cfg \
bash /tmp/katago-remote-install.sh
```

这些路径必须已经存在。安装器只记录路径，不复制、修改或删除自定义文件。

## 当前支持范围

首版支持 Linux x86_64 NVIDIA GPU 服务器，以及使用 KataGo Analysis JSON over WebSocket 的客户端，包括 LizzieYzy Next 和 KaTrain 兼容客户端。首版不支持原始 GTP over WebSocket、多人共享、CPU 服务器、Windows 服务器或长期生产级固定域名。

遇到问题时请先打开[中文故障排查](docs/TROUBLESHOOTING.zh-CN.md)。技术实现见[协议与架构说明](docs/PROTOCOL.md)。

## 致谢

本项目使用 [KataGo](https://github.com/lightvector/KataGo)、[KaTrain](https://github.com/sanderland/katrain) 和 [cloudflared](https://github.com/cloudflare/cloudflared) 的公开能力，是独立社区项目，与上述项目及 Cloudflare 无隶属关系。
