# Cloud VM Manager

![界面示例](pic/示例.jpg)

一个轻量级网页工具，用于管理 Azure、GCP、OCI 虚拟机实例。

当前支持：

- 按配置账号手动加载机器列表
- Azure、GCP、OCI 实例开机、关机、重启
- Azure、GCP、OCI 更换公网 IP
- OCI 实例编辑：修改实例名称、规格、Flex OCPU 和内存
- OCI 编辑页按规格本身限制提示 OCPU / 内存可填写范围
- OCI 安全规则管理：安全列表、网络安全组、创建并关联网络安全组、入站/出站规则编辑
- OCI 数据传输用量监控：手动查询、周期检测、阈值提醒、超阈值自动停机
- Cloudflare DNS 更新，使用 API Token
- 手动更新 DNS，或在换 IP 后按开关决定是否同步 DNS
- 网页可视化管理 DNS 绑定，每个 VM 实例可单独配置
- DNS 管理页面：查看和保存 Cloudflare 账号、DNS 绑定列表、预览脱敏后的 dns.conf
- 代理池：手动添加或逐行批量导入 HTTP / SOCKS5 代理，支持备注和连通性测试
- Azure、GCP、OCI 账号可绑定主代理和 fallback 代理；未配置 fallback 代理时自动回退直连
- 可选登录认证，适合需要暴露到公网的场景
- 网页管理页修改登录账号密码，密码落盘前自动 bcrypt 加密
- 网页管理页手动重载配置，修改配置后无需重启服务
- 网页管理页检查更新、配置 GitHub 下载加速源并应用 Release 更新

## 目录结构

```text
config/                  # 配置目录（Docker 映射 /app/config）
├── config.conf          # 主配置：云账号 + 认证
├── dns.conf             # DNS 配置：Cloudflare 账号 + DNS 绑定
├── proxy.conf           # 代理池 + 账号代理绑定（网页自动创建）
└── keys/                # 密钥文件目录
    ├── gcp01.pem
    └── oci.pem
```

## 本地运行

```bash
go mod tidy
go run .
```

访问：

```text
http://localhost:3000
```

## 配置

程序读取顺序：

```text
config/config.conf -> config/config.ini -> config/config.yaml -> config.conf -> config.ini -> config.yaml
```

初始化：

```bash
mkdir -p config/keys
cp config.example.conf config/config.conf
```

DNS 配置（Cloudflare 账号和绑定）放在 `config/dns.conf`，也可以在网页 DNS 管理页面维护，或通过 VM 实例页面可视化配置。

代理配置放在 `config/proxy.conf`，通常直接在网页「代理池」页面维护。

密钥文件放到 `config/keys/`，配置文件里使用容器内路径 `/app/config/keys/xxx.pem`。

详细字段说明见 [CONFIGURATION.md](CONFIGURATION.md)。

## 宝塔反向代理

如果项目需要公网访问，建议先在 `config/config.conf` 开启认证，并使用 HTTPS：

```ini
auth=begin
[main]
enabled=true
username=admin
password_hash=your-bcrypt-hash
session_secret=your-random-secret-at-least-32-chars
session_hours=12
cookie_secure=true
auth=end
```

宝塔面板操作：

1. 网站 -> 添加站点，绑定你的域名并申请 SSL。
2. 网站设置 -> 反向代理 -> 添加反向代理。
3. 目标 URL 填：

```text
http://127.0.0.1:3000
```

4. 在反向代理的"配置文件"或"Nginx 高级配置"里确认增加这些字段：

```nginx
proxy_set_header Host $host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
```

完整 location 示例：

```nginx
location / {
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

其中 `Host` 和 `X-Forwarded-Proto` 很重要：项目开启认证后会校验 POST 请求来源，缺少这两个头可能导致登录、保存配置、开关机等 POST 操作被拒绝。

## Docker 部署

准备配置目录：

```bash
mkdir -p config/keys
cp config/config.example.conf config/config.conf
```

把 GCP/OCI 等 PEM 或 JSON 密钥文件放入 `config/keys/`，配置文件里使用容器内路径 `/app/config/keys/xxx.pem`。

### 方式一：Docker 自构建

```bash
docker build -t cloud-vm-manager:local .

docker run -d \
  --name cloud-vm-manager \
  -p 3000:3000 \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/runtime:/app/runtime \
  --restart unless-stopped \
  cloud-vm-manager:local
```

### 方式二：Docker Run 使用镜像

```bash
docker pull ghcr.io/crossgg/cloud-vm-manager:latest

docker run -d \
  --name cloud-vm-manager \
  -p 3000:3000 \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/runtime:/app/runtime \  
  --restart unless-stopped \
  ghcr.io/crossgg/cloud-vm-manager:latest
```

### 方式三：Docker Compose

使用仓库里的 [docker-compose.yaml](docker-compose.yaml)：

```bash
docker compose up -d
```

当前 compose 会映射：

```yaml
volumes:
  - ./config:/app/config
  - ./runtime:/app/runtime
```

`runtime/` 会保存系统设置页安装的新版可执行文件和前端资源。更新后即使容器重启或重新创建，也会继续使用该版本。

## 实例管理

进入「实例管理」后，先选择左侧或顶部的云账号，再加载该账号下的机器。每张 VM 卡片会显示实例名称、区域、规格、公网 IP、内网 IP、资源组/项目/区间等信息。

通用操作：

- 「开机」「关机」「重启」：对当前实例发送电源操作。
- 「换 IP」：为实例更换公网 IP。不同云厂商的实现方式不同，成功后会刷新卡片。
- 「换 IP 后更新 DNS」：只有配置了 DNS 绑定的机器才可勾选；勾选后，换 IP 成功会同步更新 Cloudflare 记录。
- 「更新 DNS」：不换 IP，直接用当前公网 IP 更新已绑定的 Cloudflare 记录。
- 「DNS 绑定」：打开可视化绑定面板，为当前 VM 配置一个或多个域名记录。

OCI 专用操作：

- 「编辑」：打开 OCI 实例编辑面板，可修改实例名称、配置规格、Flex OCPU 和内存。
- 「安全规则」：管理主 VNIC 关联的安全列表和网络安全组规则，也可以新建网络安全组并关联到实例。

### OCI 实例编辑

OCI 编辑面板会尽量贴近 OCI 控制台的交互：

- 先按 AMD、Intel、Ampere、专用和上一代分组展示可用规格。
- 选择规格后，显示该规格的处理器说明、是否 Flex、最大 VNIC 数。
- Flex 规格支持输入 OCPU 和内存，固定规格会锁定 OCPU/内存输入框。
- OCPU / 内存提示会考虑规格本身范围，以及内存和 OCPU 的比例限制。

编辑页不会额外查询账号剩余额度，因此不需要 OCI Limits / Resource Availability 权限。最终是否能保存成功仍以 OCI UpdateInstance API 返回为准；如果账号配额不足或当前可用容量不足，OCI 会在提交时返回失败原因。

调整运行中实例规格可能触发短暂停机。编辑面板默认勾选「允许停机完成规格变更」，提交前会再次确认。

### OCI 安全规则

「安全规则」面板支持两类资源：

- 安全列表：读取主 VNIC 所在子网关联的 Security List，支持编辑入站/出站规则。
- 网络安全组：读取主 VNIC 关联的 NSG，支持新增、删除、更新入站/出站规则。

规则支持常用协议预设（SSH、HTTP、HTTPS、RDP 等），也可以填写 IANA 协议号；TCP/UDP 支持端口范围，ICMP 支持类型和代码。

### OCI 数据传输监控

选择 OCI 账号后，实例管理页会显示「OCI 数据传输用量监控」面板：

- 「手动获取」：立即查询当月 VCN 出网流量。
- 「监控设置」：配置周期检测、阈值、是否超阈值自动停机、停机方式。
- 阈值默认按 9000 GB 设计，适合接近 OCI 免费流量上限前留出缓冲。

自动停机只会停止当前 OCI 账号下正在运行的实例。启用前请确认账号范围和阈值设置。

## 代理池

代理池页面支持 HTTP、HTTPS 和 SOCKS5 代理。手动添加时填写 `host:port` 或 `user:password@host:port`；批量导入每行一条：

```text
http://user:password@127.0.0.1:8080 # 东京出口
socks5://127.0.0.1:1080 # 备用线路
```

账号绑定按 `provider/account` 精确匹配。绑定后，该账号的令牌、实例列表与详情、账单/监控、状态操作、换 IP、OCI 编辑与安全规则等所有官方云 API 请求均使用主代理；主代理发生连接错误或返回代理网关错误时，切换到所选 fallback。fallback 可选择池内另一条代理，默认值为直连。

`proxy.conf` 由页面自动创建和维护：

```ini
proxy=begin
[proxy-example]
url=http://127.0.0.1:8080
remark=东京出口
proxy=end

proxy_binding=begin
[binding-1]
provider=azure
account=az001
proxy=proxy-example
fallback=direct
proxy_binding=end
```

## DNS 管理

### 网页管理

- **DNS 管理页面**：查看和保存 Cloudflare 账号（已存在的敏感字段以掩码显示）、DNS 绑定列表（支持删除）、预览当前 dns.conf（敏感字段已脱敏）
- **VM 实例页面**：每个 VM 卡片有「DNS 绑定」按钮，点击弹出可视化配置面板，自动填充 provider/account/vm 信息

### 配置文件

DNS 配置独立存放在 `config/dns.conf`：

```ini
cloudflare=begin
[cf01]
remark=主站 Cloudflare
api_token=your-cloudflare-api-token
zone_id=your-zone-id
cloudflare=end

dns=begin
[my-binding]
cloudflare=cf01
provider=azure
account=az001
vm=myvm
domain=vm.example.com
type=A
ttl=1
proxied=false
dns=end
```

### 行为

- 没有绑定：网页不显示"更新 DNS"按钮，换 IP 后 DNS 开关不可用。
- 有绑定：机器卡片显示"更新 DNS"按钮，可以不换 IP 直接用当前公网 IP 更新 Cloudflare。
- 换 IP 时：只有勾选"换 IP 后更新 DNS"才会在换 IP 成功后更新 Cloudflare。

Cloudflare 使用 API Token（不使用 Global API Key）。

## 安全说明

- Cloudflare API Token 和 Zone ID 不会在网页前端显示，仅在保存时写入配置文件
- 代理 URL 中的密码在代理池页面和 API 列表响应中以掩码显示
- dns.conf 原始预览自动脱敏 `api_token`、`client_secret`、`password` 等敏感字段
- 认证开启后，所有 API 需要登录后才能访问
- 配置文件、密钥文件不要提交到 Git 仓库

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/auth` | 查询认证状态 |
| POST | `/api/login` | 登录 |
| POST | `/api/logout` | 退出登录 |
| GET | `/api/config/status` | 查询配置加载状态 |
| POST | `/api/config/reload` | 手动重载配置 |
| GET | `/api/update/status` | 查询当前版本和更新状态 |
| POST | `/api/update/apply` | 下载并应用 Release 更新 |
| GET | `/api/settings/auth` | 查询认证配置状态 |
| POST | `/api/settings/auth` | 修改认证配置 |
| POST | `/api/settings/update` | 保存更新下载加速源 |
| GET | `/api/accounts` | 获取本地配置账号列表 |
| GET | `/api/proxies` | 获取代理池和账号绑定 |
| POST | `/api/proxies` | 添加代理 |
| POST | `/api/proxies/import` | 逐行批量导入代理 |
| PUT | `/api/proxies/:id` | 修改代理和备注 |
| DELETE | `/api/proxies/:id` | 删除代理 |
| POST | `/api/proxies/:id/test` | 测试代理到 Azure 管理端点的连通性 |
| PUT | `/api/proxy-bindings` | 新增或更新账号代理绑定 |
| DELETE | `/api/proxy-bindings?provider=&account=` | 删除账号代理绑定 |
| GET | `/api/vms?provider=&account=` | 加载指定账号机器列表 |
| GET | `/api/vm/:provider/:account/:name` | 获取单台机器详情 |
| POST | `/api/vm/:provider/:account/:name/start` | 开机 |
| POST | `/api/vm/:provider/:account/:name/stop` | 关机 |
| POST | `/api/vm/:provider/:account/:name/restart` | 重启 |
| POST | `/api/vm/:provider/:account/:name/change-ip` | 换 IP |
| POST | `/api/vm/:provider/:account/:name/update-dns` | 更新 DNS |
| GET | `/api/refresh/:provider/:account/:name` | 刷新单台机器详情 |
| GET | `/api/vm/:provider/:account/:name/edit-options` | OCI 实例编辑选项和规格范围 |
| POST | `/api/vm/:provider/:account/:name/edit` | OCI 实例编辑保存 |
| GET | `/api/vm/:provider/:account/:name/security-lists` | OCI 安全列表 |
| POST | `/api/vm/:provider/:account/:name/security-lists/:listID/rules` | 保存 OCI 安全列表规则 |
| GET | `/api/vm/:provider/:account/:name/network-security-groups` | OCI 网络安全组 |
| POST | `/api/vm/:provider/:account/:name/network-security-groups` | 创建并关联 OCI 网络安全组 |
| POST | `/api/vm/:provider/:account/:name/network-security-groups/:groupID/rules` | 保存 OCI 网络安全组规则 |
| GET | `/api/oci/:account/data-transfer` | 手动查询 OCI 当月数据传输 |
| GET | `/api/oci/:account/data-transfer/config` | 查询 OCI 数据传输监控配置 |
| POST | `/api/oci/:account/data-transfer/config` | 保存 OCI 数据传输监控配置 |
| POST | `/api/oci/:account/data-transfer/start` | 启动 OCI 数据传输周期监控 |
| POST | `/api/oci/:account/data-transfer/stop` | 停止 OCI 数据传输周期监控 |
| GET | `/api/oci/:account/data-transfer/status` | 查询 OCI 数据传输监控状态 |
| GET | `/api/dns/cloudflare` | Cloudflare 账号列表（脱敏） |
| POST | `/api/dns/cloudflare` | 保存 Cloudflare 账号 |
| GET | `/api/dns/bindings` | DNS 绑定列表 |
| POST | `/api/dns/bindings` | 保存 DNS 绑定列表 |
| GET | `/api/dns/raw` | 预览 dns.conf（脱敏） |
| POST | `/api/dns/delete-binding` | 删除 DNS 绑定 |
| GET | `/api/vm/:provider/:account/:name/dns` | VM 的 DNS 绑定 |
| POST | `/api/vm/:provider/:account/:name/dns` | 保存 VM 的 DNS 绑定 |

## 注意

- GCP 机器 ID 使用 `zone|instance-name`。
- OCI 机器 ID 使用 instance OCID。
- Cloudflare Token 建议只授予目标 Zone 的 DNS edit 权限。
- `config/` 目录下的所有文件（config.conf、dns.conf、keys/）不要提交到仓库。

## License

MIT
