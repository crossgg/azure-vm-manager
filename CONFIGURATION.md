# 配置说明

程序优先读取 `config/config.conf`，也兼容根目录的 `config.conf`。推荐使用 `config/` 目录统一管理配置和密钥。

## 目录结构

```text
config/
├── config.conf          # 主配置：云账号（Azure/GCP/OCI）+ 登录认证
├── dns.conf             # DNS 配置：Cloudflare 账号 + DNS 绑定（可通过网页管理）
└── keys/                # 密钥文件
    ├── gcp01.pem
    └── oci.pem
```

Docker 部署时映射 `./config:/app/config`，密钥文件路径使用 `/app/config/keys/xxx.pem`。

## 登录认证

认证是可选功能。公网部署时建议开启；内网或本地调试可以关闭。认证配置只写在配置文件里，不需要写进 README。

完整示例：

```ini
auth=begin
[main]
enabled=true
username=admin
password_hash=$2a$12$replace-with-bcrypt-password-hash
session_secret=replace-with-at-least-32-random-characters
session_hours=12
cookie_secure=true
auth=end
```

字段含义：

```ini
enabled=true                     # 是否开启登录认证。true: 访问 API 前必须登录；false: 不启用认证
username=admin                   # 网页登录用户名，可以自定义，例如 admin、ops、vm-admin
password_hash=...                # 登录密码的 bcrypt 哈希值，只保存哈希，不保存明文密码
session_secret=...               # 用来签名登录会话 Cookie 的随机密钥，至少 32 个字符
session_hours=12                 # 登录会话有效时间，单位是小时；超过后需要重新登录
cookie_secure=true               # 是否只允许 HTTPS 发送 Cookie；公网 HTTPS 部署用 true，本地 HTTP 调试用 false
```

字段怎么获取或生成：

- `enabled`：手动填写。公网部署建议 `true`；只在本机或可信内网使用时可以设为 `false`。
- `username`：手动填写你想使用的管理员用户名。
- `password_hash`：由你的登录密码生成 bcrypt 哈希。不要把明文密码写入配置文件。
- `session_secret`：生成一个长度至少 32 字符的随机字符串，越随机越好。
- `session_hours`：按需要手动填写，例如 `8`、`12`、`24`。
- `cookie_secure`：如果页面通过 `https://` 访问，填写 `true`；如果是本地 `http://localhost:3000` 调试，填写 `false`。

安全行为：

- 连续登录失败会触发限速锁定，降低密码被在线爆破的风险。
- 通过管理页修改密码时，程序会自动生成新的 `session_secret`，所有旧登录会话会立即失效。
- 开启认证后，所有 `POST /api/*` 请求都会做同源校验。公网通过 Nginx、Caddy 等反向代理部署时，请保留正确的 `Host`，并传递 `X-Forwarded-Proto`，否则 HTTPS 站点的 POST 请求可能被判定为来源不匹配。

在 PowerShell 中生成 `session_secret`：

```powershell
-join ((48..57 + 65..90 + 97..122) | Get-Random -Count 48 | ForEach-Object {[char]$_})
```

生成 `password_hash` 的方式：

1. 如果你本机已有 bcrypt 工具，可以直接用该工具生成。
2. 如果使用 Go，可以临时新建一个小工具生成 bcrypt 哈希，成本值建议 `12`：

   ```go
   package main

   import (
   	"fmt"

   	"golang.org/x/crypto/bcrypt"
   )

   func main() {
   	hash, err := bcrypt.GenerateFromPassword([]byte("这里换成你的登录密码"), 12)
   	if err != nil {
   		panic(err)
   	}
   	fmt.Println(string(hash))
   }
   ```

生成后，把输出填到：

```ini
password_hash=生成出来的-bcrypt-哈希
```

不要把生成脚本里的明文密码提交到仓库。生成完成后可以删除临时脚本。

也可以在网页右侧的"管理"页面修改登录用户名和密码。网页里输入的是明文新密码，但后端保存到配置文件前会自动生成 bcrypt 哈希，配置文件中仍然只保存 `password_hash`。

## 配置重载

程序不会后台轮询配置文件。修改 `config/config.conf` 或 `config/dns.conf` 后，需要在网页"管理"页面点击"重载配置"按钮，程序才会重新读取配置。

在网页"管理"页面保存登录认证配置时，后端会先写入配置文件，然后自动执行一次配置重载，因此保存后会立即生效。重载失败时，程序会继续使用上一次成功加载的配置，并在管理页面显示错误信息。

## Azure

获取方式：

1. 登录 Azure CLI：`az login`
2. 创建 Service Principal：

   ```bash
   az ad sp create-for-rbac --name "vm-manager" --role Contributor --scopes /subscriptions/<subscription-id>
   ```

3. 从输出中获取 `appId`、`password`、`tenant`，填入配置。

字段说明：

```ini
azure=begin
[az001]
group=azure                 # 前端分组名称
subscription_id=xxx         # Azure Subscription ID
tenant_id=xxx               # Azure Tenant ID，也可写 tenant
client_id=xxx               # Service Principal App ID，也可写 appId
client_secret=xxx           # Service Principal Password，也可写 password
resource_group=rg-name      # VM 所在资源组
location=koreacentral       # 创建新公网 IP 时使用的区域
azure=end
```

## GCP

获取方式：

1. 在 Google Cloud 创建 Service Account。
2. 给该账号授予 Compute Instance Admin、Compute Network Admin 等足够权限。
3. 下载 service account JSON，或把其中的 `private_key` 单独保存为 PEM。
4. 配置 `project_id`、`client_email`、`key_file`。

字段说明：

```ini
gcp=begin
[gcp01]
group=gcp                                    # 前端分组名称
project_id=my-project                        # GCP Project ID
client_email=sa@project.iam.gserviceaccount.com
key_file=/app/config/keys/gcp.pem            # service account JSON 或 RSA private key PEM
gcp=end
```

GCP 机器 ID 在 DNS 绑定里写作 `zone|instance-name`，例如：

```ini
vm=asia-east1-a|vm-01
```

## OCI

获取方式：

1. 在 OCI 用户详情中添加 API Key。
2. 保存 private key PEM。
3. 记录 user OCID、tenancy OCID、fingerprint、region、compartment OCID。

字段说明：

```ini
oci=begin
[oci-jp]
group=oci                                    # 前端分组名称
user=ocid1.user.oc1...                       # User OCID
fingerprint=xx:xx:xx                         # API key fingerprint
tenancy=ocid1.tenancy.oc1...                 # Tenancy OCID
compartment_id=ocid1.compartment.oc1...
region=ap-tokyo-1
key_file=/app/config/keys/oci.pem            # OCI API private key PEM
oci=end
```

OCI 机器 ID 在 DNS 绑定里写 instance OCID。

### OCI 权限建议

OCI API Key 对应的用户/组需要能读取实例、VNIC、子网、规格、限额，并能执行你在网页上使用的操作。可以按最小权限拆分，也可以先在测试 compartment 内授予较完整的管理权限再收敛。

常用能力和权限方向：

- 实例列表、开关机、重启、编辑规格：需要读取和管理 Compute Instance。
- 换公网 IP：需要读取 VNIC / Private IP，并能创建、删除、解绑 Public IP。
- 安全列表：需要读取子网关联的 Security List，并能更新 Security List 规则。
- 网络安全组：需要读取、创建、更新 Network Security Group，并能更新 VNIC 的 NSG 关联。
- 实例编辑最大可用 OCPU / 内存提示：需要读取 Limits / Resource Availability；如果没有该权限，页面会回退显示规格允许范围。
- 数据传输监控：需要读取 Monitoring 指标，当前通过 `oci_vcn` 的 `VnicToNetworkBytes` 汇总查询。

示例策略需要根据你的 tenancy、group、compartment 名称调整：

```text
Allow group vm-manager to manage instances in compartment <compartment-name>
Allow group vm-manager to manage virtual-network-family in compartment <compartment-name>
Allow group vm-manager to read metrics in compartment <compartment-name>
Allow group vm-manager to inspect limits in tenancy
Allow group vm-manager to read resource-availability in tenancy
```

如果只想使用查看和开关机功能，可以进一步收窄权限；如果要使用安全规则、换 IP、编辑实例、数据传输监控，则需要保留对应资源的管理或读取权限。

### OCI 数据传输监控配置

OCI 账号块可以配置数据传输监控参数，也可以在网页「实例管理」中选择 OCI 账号后通过「监控设置」保存。保存后会写回 `config/config.conf` 并自动重载。

```ini
oci=begin
[oci-jp]
group=oci
user=ocid1.user.oc1...
fingerprint=xx:xx:xx
tenancy=ocid1.tenancy.oc1...
compartment_id=ocid1.compartment.oc1...
region=ap-tokyo-1
key_file=/app/config/keys/oci.pem
dt_monitor_enabled=false
dt_monitor_interval=300
dt_monitor_threshold=9000
dt_monitor_auto_stop=false
dt_monitor_stop_method=soft
oci=end
```

字段含义：

```ini
dt_monitor_enabled=false       # 是否启动周期检测
dt_monitor_interval=300        # 检测周期，单位秒，最小建议 60
dt_monitor_threshold=9000      # 流量阈值，单位 GB
dt_monitor_auto_stop=false     # 超过阈值后是否自动停机
dt_monitor_stop_method=soft    # soft: SOFTSTOP；hard: STOP
```

自动停机会停止该 OCI 账号下所有正在运行的实例。启用前建议先手动查询一次用量，并确认阈值和账号范围。

## Cloudflare DNS

DNS 配置独立存放在 `config/dns.conf`，也可以通过网页 DNS 管理页面维护。

只支持 API Token，不使用 Global API Key。Token 至少需要目标 zone 的 DNS edit 权限。

```ini
cloudflare=begin
[cf01]
remark=主站 Cloudflare                        # 可选，账号备注
api_token=your-cloudflare-api-token           # Cloudflare API Token
zone_id=your-zone-id                          # Cloudflare Zone ID
cloudflare=end
```

> **安全说明**：Cloudflare 的 `api_token` 和 `zone_id` 不会在网页前端显示。网页 DNS 管理页只显示账号名称和备注。dns.conf 预览也会自动脱敏敏感字段。

## DNS 绑定

`dns` 段定义"某台机器的公网 IP 应绑定到哪个域名"。只有配置了绑定的机器，网页才会显示"更新 DNS"按钮和"换 IP 后更新 DNS"开关。

DNS 绑定可以：
- 手动编辑 `config/dns.conf`
- 在网页 DNS 管理页面维护绑定列表
- 在每个 VM 卡片上点击「DNS 绑定」按钮可视化配置

```ini
dns=begin
[gcp01-main]
cloudflare=cf01                  # 使用哪个 cloudflare 配置
provider=gcp                     # azure / gcp / oci
account=gcp01                    # 对应账号名
vm=asia-east1-a|vm-01            # Azure: VM 名称；GCP: zone|name；OCI: instance OCID
domain=vm.example.com            # 要更新的域名
type=A                           # 当前支持 A 记录
ttl=1                            # 1 表示 Cloudflare automatic TTL
proxied=false                    # 是否开启 Cloudflare proxy
dns=end
```
