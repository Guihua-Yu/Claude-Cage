---
title: 网络与代理
description: 导入节点与订阅、配置多次中转，理解联网检查、时区和出口变化规则。
group: 开始使用
order: 2
keywords: Clash Mihomo YAML SOCKS5 住宅 IP DNS DoH 暂停 断网 代理链 订阅
---
<figure class="guide-shot">
  <a href="../../www/public/docs/screenshots/proxy-subscription.jpg" target="_blank" rel="noopener" aria-label="查看原图：添加订阅链接与首次主机下载选项"><img src="../../www/public/docs/screenshots/proxy-subscription.jpg" alt="添加订阅链接与首次主机下载选项" width="2560" height="1764" loading="lazy" decoding="async" /></a>
  <figcaption>添加订阅链接与首次主机下载选项。截图版本：0.1.6-alpha-preview（开发构建） · macOS 原生应用 · 2026-11-07。<a href="../../www/public/docs/screenshots/proxy-subscription.jpg" target="_blank" rel="noopener">查看原图（2560 × 1764） ↗</a></figcaption>
</figure>

## 导入自己的代理

入口是首页“配置网络与代理”，或“设置 → 网络与代理”。点击“添加来源”，选择导入方式：

| 来源 | 怎么填 |
| --- | --- |
| 订阅链接 | 输入名称和 HTTPS 节点订阅地址。首次没有可用节点时，可明确勾选“通过主机网络下载这份订阅”。 |
| 本地 YAML | 拖入或点击选择导出的 YAML、YML、JSON 文件（单个不超过 8 MiB），也可直接粘贴 Clash/Mihomo YAML 内容。读取后可编辑，点击“导入并验证”完成导入。 |
| 手动节点 | 填写 HTTP 或 SOCKS5 服务器、端口和对应认证字段。 |

点击“导入并验证”，进入“节点”，选最终出口，再点“应用代理”。“保存设置”用于其他设置，不能替代应用代理。未应用的选择只是草稿。

订阅必须包含 `proxies` 或 `payload` 节点。Base64 列表、单个节点分享链接、只引用远程 provider 的配置暂不支持。导入只采用支持的节点与代理链，不采用订阅中的主机系统代理、TUN、监听端口或直连规则。

普通节点参数保留并交给 Cage 随附的 Mihomo 核心校验。例如，从 FlClash 复制的 VLESS 节点可以保留 `xudp: true`，无需手动改成其他字段。内核可能忽略未知字段；配置校验通过后，仍需应用节点并检查实际出口。客机公网 UDP 的阻断规则不受这些协议选项影响。

VMess、VLESS 和 Trojan 的 WebSocket 节点可以导入旧字段 `ws-path`、`ws-headers`。节点须明确设置 `network: ws`，Cage 会将路径和请求头转换为 `ws-opts.path`、`ws-opts.headers`，已保存的旧节点在应用代理时也会转换。新旧字段可同时存在，路径须一致，请求头按名称合并并忽略名称的字母大小写。相同请求头的值冲突、字段写法重复或旧字段用于其他传输方式时，来源会显示具体原因，需要修改该节点后重新导入。

Hysteria2 节点可使用 `mport` 指定端口跳跃范围、`pinSHA256` 指定证书指纹，导入时会转换为核心支持的字段并保留功能。若节点使用不支持的协议、引用本机文件或网卡、管理字段重复、别名参数冲突或代理链不可用，会跳过该节点，并在来源下显示导入数量、跳过数量和原因。点击「查看原因」可展开具体节点；依赖失败节点的代理链也会跳过。若所有节点均被跳过，导入失败；更新影响当前出口或中转节点时保留原配置，请先应用其他节点再更新。其余节点还须整体通过核心配置校验，校验失败时原配置保持不变，可在设置的「日志与诊断」中导出诊断包查看校验记录。

同一个订阅地址可能按请求的客户端身份返回不同格式。Cage 下载时使用 Clash 客户端身份，服务商返回 YAML 时可以导入；若返回 Base64 或分享链接列表，仍需向服务商获取 Clash/Mihomo YAML。

<figure class="guide-shot">
  <a href="../../www/public/docs/screenshots/proxy-manual.jpg" target="_blank" rel="noopener" aria-label="查看原图：手动填写 HTTP / SOCKS5 节点"><img src="../../www/public/docs/screenshots/proxy-manual.jpg" alt="手动填写 HTTP / SOCKS5 节点" width="2560" height="1764" loading="lazy" decoding="async" /></a>
  <figcaption>手动填写 HTTP / SOCKS5 节点。截图版本：0.1.6-alpha-preview（开发构建） · macOS 原生应用 · 2026-11-07。<a href="../../www/public/docs/screenshots/proxy-manual.jpg" target="_blank" rel="noopener">查看原图（2560 × 1764） ↗</a></figcaption>
</figure>

## 通过中转连接住宅出口

如果住宅节点无法直接连接，在“节点”页切换到“链式代理”。按连接顺序选择中转，将住宅节点设为“最终出口”；需要多次中转时点“添加中转”。节点可以来自不同来源。

应用前检查预览，例如：`Cage → 中转节点 → 住宅代理 → 目标网站`。目标网站看到的是最终出口。订阅更新不会覆盖已保存的组合；使用中的节点被移除时会保留原配置并显示错误，需要你重新选择。

<figure class="guide-shot">
  <a href="../../www/public/docs/screenshots/network-settings.jpg" target="_blank" rel="noopener" aria-label="查看原图：网络与代理页面及出口变化规则"><img src="../../www/public/docs/screenshots/network-settings.jpg" alt="网络与代理页面及出口变化规则" width="2560" height="1764" loading="lazy" decoding="async" /></a>
  <figcaption>网络与代理页面及出口变化规则。截图版本：0.1.6-alpha-preview（开发构建） · macOS 原生应用 · 2026-11-07。<a href="../../www/public/docs/screenshots/network-settings.jpg" target="_blank" rel="noopener">查看原图（2560 × 1764） ↗</a></figcaption>
</figure>

## 为什么应用了节点仍然离线

代理核心就绪和工作环境获准联网是两个步骤。环境启动后先保持业务外网关闭，执行出口、DNS、系统与浏览器配置、共享目录、宿主管理接口和网络阻断等关键检查。Linux 客机的 DNS 项同时要求解析器只指向环境内地址，且到公共 DNS 的 UDP/53、TCP/53 与 DoT/853 直连未绕过网关；宿主管理接口项还探测链路本地元数据地址和用户代理端口。任一关键项失败或证据缺失都会继续关闭外网。

出口检测通过所选代理读取公网 IPv4 地址，依次使用 `https://ipv4.icanhazip.com/` 和备用服务 `https://api.ipify.org/`。双栈节点使用其 IPv4 出口完成检查；宿主、客机终端和浏览器使用相同的检测方式。启动检查无法取得有效地址时，业务外网保持关闭，可稍后重试或检查节点。

点击底部网络状态查看失败项，修复后重新检查。核心运行但环境未启动时会显示“待启动环境”。关键检查通过、联网授权有效时才显示“已联网”。

“出口采样不完整”会列出未取得结果的路径：宿主、虚拟机终端或浏览器，以及超时、检测通道返回的 HTTP 状态或缺少地址等原因。终端代理检查通过只证明该次终端采样成功，宿主和浏览器仍须分别取得结果。动态 IP 规则决定哪些出口地址可以接受，采样失败时仍须重新检测。

浏览器的出口采样没有响应或返回 HTTP 503 时，Cage 会等待一秒并重试一次。重试期间外网保持关闭；持续失败时保留最后一次检测结果。浏览器配置不符或响应缺少出口地址时，须先处理对应失败项。环境已经就绪、仅联网检查未通过时，页面会显示“环境已启动，联网检查未通过”，可直接重检网络。

离线时仍可使用终端、共享文件及环境内的本地服务。浏览器地址栏中的 `localhost` 指当前 Cage 客机；开发服务应在 Cage 终端里启动。

<figure class="guide-shot">
  <a href="../../www/public/docs/screenshots/region-settings.jpg" target="_blank" rel="noopener" aria-label="查看原图：跟随出口 IP 的时区与语言设置"><img src="../../www/public/docs/screenshots/region-settings.jpg" alt="跟随出口 IP 的时区与语言设置" width="2560" height="1764" loading="lazy" decoding="async" /></a>
  <figcaption>跟随出口 IP 的时区与语言设置。截图版本：0.1.6-alpha-preview（开发构建） · macOS 原生应用 · 2026-11-07。<a href="../../www/public/docs/screenshots/region-settings.jpg" target="_blank" rel="noopener">查看原图（2560 × 1764） ↗</a></figcaption>
</figure>

## 系统镜像的下载网络

系统镜像和镜像版本目录使用主机网络，与 Cage 客机的代理节点分开。Clash 等软件只开启“系统代理”也可以接管镜像下载，无需为了 Cage 下载镜像而开启 TUN；下载连接不绑定物理网卡，也不会修改代理软件的节点或规则。镜像网站若被主机代理规则设为直连，Cage 遵循该规则。

- macOS：读取系统 HTTP、HTTPS、SOCKS 代理及 PAC，每次请求和重定向按目标地址解析。
- Windows：读取当前用户的系统代理；PAC 与自动检测通过 WinHTTP 按目标地址解析。
- Linux：支持代理环境变量、GNOME 手动桌面代理及主机 TUN。此构建遇到 GNOME 自动 PAC 会停止并提示改用 TUN 或代理环境变量；其他桌面环境请设置代理环境变量或开启 TUN。

从终端启动时，目标协议对应的 `HTTP_PROXY`、`HTTPS_PROXY` 优先于系统设置，`ALL_PROXY` 作为缺省代理，并遵循对应的 `NO_PROXY` 例外。这些主机设置不会传入客机。代理连不上或已配置的 PAC 解析失败时，下载停止，不因失败切换直连；修复主机代理后，在初始化界面继续安装，已有下载会尝试续传。

客机进程和开发工具安装继续走 Cage 的独立代理。系统镜像下载成功不代表客机网络检查通过，进入工作台仍需完成客机的网络与隔离检查。

## 时区、语言与出口规则

在“设置 → 时区与语言”选择跟随出口或手动设置。自动模式通过代理查询出口位置，不使用主机位置替代。位置服务失败时可重试或切换节点；手动设置时区和语言仍须确认出口国家或地区。

网络页的“IP 变化规则”支持静态 IP，以及动态 IP 的同一城市、同一国家／地区或不限地区。城市模式还检查国家与州省。已保存的出口基准只有明确修改后才改变，重新检查不会自动接受新地址。

中国大陆出口禁止放行。检测地址命中内置大陆网段库，或定位结果为中国大陆时，业务外网保持关闭；静态 IP 和“不限地区”均不能豁免。无法确认出口国家或地区时也保持关闭，离线终端和文件仍可使用。地区定位和网段库可能存在误差；后台按周期采样，无法保证第三方代理每条连接都使用同一出口。

保存影响系统环境的配置可能要求重启。先保存文件；运行中不会因后台发现位置变化而静默重启任务。

## 暂停、恢复和网络切换

点击底部状态可手动暂停外网。暂停会切断已有外部连接，保留本地任务和文件操作；关闭、重开 Cage 或刷新订阅不会解除手动暂停。

后台会定期核对出口与环境配置。两个出口检测服务均超时时，Cage 在现有联网许可到期前继续重试，不延长许可；到期仍未确认时关闭外网，并尝试完整复检。发现出口或环境配置不符合规则时立即暂停。主机切换网络后会尝试恢复并完整复检；原先人为暂停的状态仍需主动恢复。控制服务失联后，已有网络授权到期关闭。固定出口还需要代理服务本身保证，周期采样不能证明所有连接都走同一 IP。

自动完整复检首次在 15 秒后尝试，失败后依次等待 30、60、120、240 秒，之后每次等待 300 秒；实际开始时间还取决于环境就绪、当前操作和内存压力。网络状态会显示剩余等待时间或等待原因。重新检查必须取得全部关键证据，自动恢复不会修改已保存的出口规则。

## DNS 与诊断

业务 DNS 通过所选代理转发。节点域名的启动解析属于准备通信，与客机业务 DNS 的设置分开。Cage 不修改主机系统代理或 TUN，也不改变本机其他代理软件的节点。

代理页“诊断”可检查核心状态、重启 Cage 核心或选择独立监听端口。端口占用时选择未占用端口即可，修改后按提示重启环境。具体症状见[故障排查](troubleshooting.md#节点已应用但无法联网)。

## 通过 Tailscale 私网使用 SSH

从侧栏「浏览器」下方打开「Tailscale」，在 Cage 浏览器授权后，可让本机与 Cage 环境通过私网双向 SSH。连接步骤、公钥和本机 SSH 服务要求见 [Tailscale 与 SSH 私网](tailscale.md)。

私网访问由用户主动启用，并受设备和 SSH 授权约束。外层通信继续经过 Cage 受控出口，暂停外网或代理中断会影响连接。这个入口与默认阻断的主机、局域网直连分别处理，不开放主机公网监听或子网路由。
