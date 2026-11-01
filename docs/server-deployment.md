# Linux ARM64 / macOS 服务端部署

服务端入口是 `cmd/server`，复用现有多账号 WebUI、登录、家庭管理、储存与订阅操作、隐藏邮件地址、六位码、受信任设备与推送/Circle 功能。没有改成只支持普通登录的替代产品。

## 运行方式与依赖

完整功能通过 Linux Docker 容器运行，支持 `linux/arm64` 与 `linux/amd64`。macOS（包括 Apple Silicon）通过 Docker 的 Linux 虚拟机运行同一镜像。无需 Windows、iCloud for Windows、Mac/iPhone 中继或 USB 设备。

Go 服务和后台编译为目标架构。ADI 使用固定的上游 Linux x86_64 模块；ARM64 镜像内置 QEMU 用户态指令模拟与独立 x86 Python 用户空间。无需在宿主机注册 binfmt，也无需特权容器。首次跨架构构建时，Docker 构建器需支持 amd64 阶段；Docker Desktop 已提供，Linux 可使用支持跨架构的 Buildx 构建器。

这不是完全开源的纯 Go ADI 实现。上游闭源模块只在构建时从官方发布包取得并校验 SHA256，仓库不存放第三方二进制。macOS 原生直接启动 Go HTTP 服务可以用于开发，但完整 ADI 运行时采用 Linux 容器，不能直接在 macOS 加载 ELF `.so`。

“无设备依赖”表示没有实体设备或 Windows 运行时依赖，不表示移除 Apple 协议的设备身份：每账号仍独立保存虚拟身份、ADI provisioning、APS 证书/私钥和推送令牌。新身份标记为 synthetic diagnostic，不能宣称为官方实体 Mac。Apple 的首次账号验证、条款、地区与风控要求仍照常执行；已有受信任身份的本机生成码与首次账号验证是两个步骤。

## Docker Compose

需要 Docker Engine / Docker Desktop 和 Compose v2，在仓库根目录执行：

```sh
mkdir -m 700 secrets
openssl rand -hex 32 > secrets/admin_token
printf '%s\n' 'change-this-password' > secrets/admin_password
chmod 444 secrets/admin_token secrets/admin_password
docker compose up -d --build
```

`secrets/` 目录仅当前用户可访问；文件只读使非 root 容器用户能读取 Compose 挂载的 secret。目录及文件均已排除 Git 和 Docker 构建上下文。请使用密码管理器保存或读取此文件，不要把内容提交到仓库或写入日志。

打开 `http://127.0.0.1:18741`。登录表单的管理员用户名由 `APPLESERVICES_ADMIN_USERNAME` 设置（默认 `admin`），密码由 `secrets/admin_password` 提供；若未设置密码文件则兼容使用 `secrets/admin_token`。管理员 API 也可使用 `Authorization: Bearer` 携带 `secrets/admin_token` 的内容。密码文件至少 8 个字符，token 至少 32 个字符。

“家庭共享”卡片可读取全部当前家庭成员的账号，并标记组织者（母号）及当前账号；组织者不必预先添加到本服务。成员账号只在管理员认证后的页面/API 中显示，并随读取时间保存在账号私有缓存中。工具通过临时私有文件传递这些字段，日志仍只输出数量与角色，不包含成员账号、DSID、联系方式或原始响应。升级后若只有旧的家庭摘要缓存，点击“重新核验 Apple”读取成员账号。

新账号加入家庭时，Apple 可能要求为临时家庭会话继续完成 `repair` 验证。控制台会在家庭卡片显示“发送短信验证码”，只有用户点击后才读取该会话的短信验证信息并请求验证码；Apple 已自动发送时不会重复发码。输入本次短信的六位验证码后，程序在原临时会话中验证，只有 Apple 签发绑定当前账号的 PET 后才提交加入请求，并继续双端成员及共享储存核验。等待期间可取消；验证码提交后等待 Apple 返回结果。家庭认证日志按本次尝试关联，记录阶段与完成/失败状态；失败的原始 Apple 错误与二次认证 HTTP 响应保存于目标账号下权限受限的 `.family-auth-diagnostics-*` 目录，仅供服务器私有排查，不进入页面、普通日志、同步接口或版本库。成功时移除本次原始诊断。会话和验证码仅在内存及进程管道中交接，不保存到磁盘，不覆盖当前账号登录；过期、验证拒绝、未签发 PET 或不支持的后续步骤均停止，不自动重试或再次发码。


隐藏邮件使用账号独立的 `hme-session.json`，存放在同一持久数据卷。文件原子写入、权限为 `0600`，保存 Web 登录/信任令牌及带原始作用域和有效期的 Cookie，不保存密码。容器重启后优先复用 Cookie；Apple 要求续期时先尝试已保存的 Web 令牌，仍无法恢复才显示密码备用入口。首次 Web 验证完成后、地区跳转或条款处理前即保存会话，减少中断后的重复验证。Apple 主动撤销或过期的凭据仍可能需要重新验证。

页面在账号及家庭状态就绪后自动读取地址，每次打开页面只自动尝试一次，失败原因留在卡片中。地址列表允许读取较大的账号池并分批显示。Apple 返回数量上限错误 `-41012` 时，在账号会话中保存已核实的上限状态，页面显示原因并将生成按钮标记为“已达 Apple 上限”；刷新页面或重启后仍保留。重新读取列表确认地址数量减少后，允许再次尝试生成。程序不会自动停用或删除已有地址。

```sh
docker compose ps
docker compose logs --tail 30
docker compose down
```

账号状态保存在 `account-data` 命名卷，停止/升级容器保留状态。不要使用 `docker compose down -v`，除非明确要删除全部身份与会话。首次部署使用空卷；不要直接混入其他账号的证书或 Windows 机器状态。本次变更未验证旧 Windows 状态的直接迁移。

管理员网页登录会话也保存在同一数据卷的 `admin-sessions.json` 中，只保存会话哈希和过期时间。服务重启或镜像升级后，浏览器在 12 小时会话有效期内会继续保持登录；退出登录会立即撤销该会话。首次升级到持久化会话版本时，已有浏览器需要重新登录一次。

“我的设备”会单独显示设备查询失败原因。iCloud 已激活或后台已登录，不代表设备管理凭据仍有效；Apple 返回 HTTP 401 时，需要点击“重新连接账号”更新凭据，保存旧会话并不能自动恢复。最近成功读取的设备显示字段保存在各账号私有的 `device-status.json` 中，绑定工作区和登录账号，不包含原始响应、设备 ID、序列号或认证令牌。重启、后台停止或查询失败时显示上次记录及读取时间；只有成功查询返回的空列表才显示 0 台设备。

## 首次登录返回 repair 后续步骤

Apple 在密码认证阶段返回 `au=repair` 时，后台会在允许请求新验证码的短信/设备验证码登录模式下，使用同账号、同设备的新认证头读取 `/auth`。只有 Apple 明确返回 HSA2 账号、`authenticationType=hsa2`、六位码及有效的当前手机号 ID，才进入短信验证。短信能力由顶层 `mode=sms` 确认；若没有顶层 mode，则必须由当前选中手机号的 `pushMode=sms` 和 `hideSendSMSCodeOption=false` 共同确认，并核对发码使用同一手机号。短信被隐藏、验证码锁定或限流时停止；HTTP 201 已自动发码时不会再次发现手机号或重复发码。没有受信任设备的账号也能由此完成短信验证。验证码验证后仍须 Apple 签发 PET，并独立完成 iCloud 认证，才算登录成功。

WebUI 的原生 repair 短信验证完成后，后台保存 GSA 会话并进入 `awaiting_icloud_activation`，将短期 PET 直接交给现有的账号条款/NAC 激活流程，不先尝试旧版 iosbuddy delegate。激活助手使用 `-accept-terms -terms-only` 仅提交条款并检查 Apple 确认；设备 NAC 就绪后才进行一次 v2 delegate 和配额验收。条款查询返回 HTTP 401 时只报告查询被拒绝，不将其报告为待接受条款。

开通状态查询的 HTTP 401 本身不证明账号有待接受条款。自动连接可继续尝试一次同身份的 Mac v2/NAC 验证，此时不提交条款、不附加 `termsAccepted`；iCloud delegate 和实时配额均成功后才安装验收会话。单独的 `activation-plan` 仍返回查询错误。Linux ARM/QEMU 的 NAC 单次模拟预算为 180 秒，整个激活最多运行 5 分钟；其他平台保留 60 秒默认预算，指令数量上限不变。

容器通过 `APPLESERVICES_NAC_PYTHON=/opt/nac-runtime/bin/python` 使用宿主架构的 Python/Unicorn 执行 NAC，避免 ARM 上在 x86 Python 模拟器中再次运行 x86 指令模拟器。只有 XNU 属性加密小例程通过 `--encrypt-python` 留在原有 x86 运行时；硬件参数使用私有管道传递，不进入命令行或日志。此模式保留 60 秒单次预算。未配置原生 NAC Python 的 Linux 安装仍使用 180 秒兼容预算。

原生 repair 短信验证交接后，服务端直接完成 NAC、条款和配额验证，不依赖浏览器保持打开。按所有者授权，程序自动接受 Apple 为该账号返回的当前 iCloud 与媒体服务条款。若首次 setup 查询为 401、随后 delegate 明确返回 `MOBILEME_TERMS_OF_SERVICE_UPDATE`，只重新读取一次条款计划、提交一次当前版本，并在 Apple 明确确认成功后继续一次 delegate；其他认证拒绝不重复提交。成功保存验收会话并读取实时配额后，自动恢复该账号后台。

“使用已有短信验证码”和 Circle 登录不会自动探测此分支，避免探测请求触发新短信。其他尚未识别的 repair 仍显示“客户端认证流程尚未完成”，保留账号独立身份和诊断检查点后停止；Apple 明确返回限流或锁定时停止发码。不能仅凭 `repair` 断定密码错误、账号异常或需要接受条款。`account.apple.com` 能正常登录，也不能证明原生认证步骤已完成。

日志只记录预期字段是否存在，不记录字段值或原始响应。未完成的认证会话原子保存为该账号目录下的 `gsa-repair.json`（权限 `0600`）。`url` 字段可能只是 `repair` 操作标记，并非网页地址；保存检查点不会访问其中的 URL。检查点不包含提交的密码，也不覆盖成功登录的 `session.json`，但包含敏感认证材料，不能提交、导出或当作登录成功凭据。诊断后台可通过 `-resume-repair` 继续账号绑定且未超过 30 分钟的挑战，不进行空密码认证；Apple 没有签发验证后凭据时，要求使用当前密码重新连接。

## 远程访问

Compose 默认仅把端口发布到宿主机回环地址。使用 HTTPS 反向代理时，将 `APPLESERVICES_PUBLIC_URL` 设为浏览器访问的精确来源（例如 `https://apple.example.com`），然后重新启动。代理需保留该来源的 `Host`，并转发 `Authorization`。无需启用跨域 CORS。

非回环监听必须配置管理员凭据和公开来源；未配置时拒绝启动。浏览器通过 `/login` 页面表单输入配置的管理员用户名和密码，不使用浏览器 Basic 认证弹窗；登录失败在表单内提示。登录 Cookie 采用 HttpOnly、SameSite=Strict，HTTPS 下使用 Secure 和 `__Host-` 前缀；会话有效期 12 小时，服务重启需重新登录。控制台提供“退出登录”以撤销当前会话。连续一分钟内失败 10 次会暂时拒绝登录请求。

匿名请求只能访问登录页、其样式及 `/healthz`（只返回进程 HTTP 健康状态），不能读取控制台或账户 API。表单登录和退出要求 Origin 匹配公开来源，登录后的浏览器 API 操作还要求独立 CSRF 凭据。程序不信任任意 `X-Forwarded-Host`。现有客户端仍可主动发送 Basic 凭据，但服务器不再发送 `WWW-Authenticate` 挑战。

已有 API 保持原来的 `/api/*` 路由。外部客户端使用 `Authorization: Bearer <管理员凭据>`，且 `Host` 必须匹配公开来源；凭据不要放入 URL。此管理员可操作全部账号，不能当作多租户隔离服务。

配置对应环境变量：

| 环境变量 | 用途 |
| --- | --- |
| `APPLESERVICES_DIR` | 持久状态目录；容器默认 `/data` |
| `APPLESERVICES_LISTEN` | HTTP 监听地址；容器默认 `0.0.0.0:18741` |
| `APPLESERVICES_PUBLIC_URL` | 浏览器精确来源，不含路径 |
| `APPLESERVICES_ADMIN_TOKEN_FILE` | 至少 32 字符的管理员凭据文件 |
| `APPLESERVICES_ADMIN_USERNAME` | 管理员登录用户名，默认 `admin` |
| `APPLESERVICES_ADMIN_PASSWORD_FILE` | 管理员登录密码文件，至少 8 字符 |
| `APPLESERVICES_SYNC_TOKEN` | 可选的只读会话同步密钥，至少 32 字符；留空关闭开放同步接口 |
| `APPLESERVICES_SYNC_TOKEN_FILE` | 同步密钥文件；也可使用 `-sync-token-file`，优先于环境变量 |
| `APPLESERVICES_ADI_LIBRARY` | 固定 Linux 模块的路径；镜像已配置 |
| `APPLESERVICES_ADI_FENCE` | 同一镜像编译的 ADI 辅助进程传输隔离库；镜像已配置 |

SIGTERM 会停止该服务管理的账号后台并保留身份。控制端口仍只监听容器回环地址，不对外发布。镜像以 UID/GID 10001 运行，Compose 不授予 Linux capabilities 或特权。

## 与 cloud-main-center 同步账户会话

在 Compose 所在目录的私有 `.env` 中配置 `APPLESERVICES_SYNC_TOKEN=<至少32字符的独立随机密钥>`，执行 `docker compose up -d appleservices` 使配置生效。可使用 `openssl rand -hex 32` 生成密钥；不要复用管理员凭据，也不要提交 `.env`。自动部署会保留已有的这一行配置。直接运行二进制时也可以通过 `APPLESERVICES_SYNC_TOKEN_FILE` 或 `-sync-token-file` 提供权限受限的密钥文件。

在 cloud-main-center「系统设置 → 账户会话同步」填写本服务的 HTTPS 来源地址、相同密钥，并选择新增账户使用的收件配置，点击「一键同步账户会话」。已有账户按 Apple ID 匹配，保留原 ID、收件绑定、库存及自动创建设置；仅新增或更新，来源移除或缺少账户不会删除目标账户。首次同步传输可用会话；之后只拉取凭据版本有变化的会话，失败项下次重试。

独立开放接口（代理必须保留公开来源 Host 与 Authorization）：

- `GET /api/open/v1/sessions`：返回 `version: 1`、`items: [{id, appleId, revision, updatedAt}]`、`unavailable`。清单不含 Cookie、令牌或 DSID。
- `GET /api/open/v1/sessions/{id}?revision=<revision>`：返回 `version: 1`、`revision`、`session`。session 包括 `appleId`、`dsid`、`clientId`、`hmeUrl`、`accountCountryCode`、`authToken`、`trustToken` 及保留作用域和有效期的 Cookie 数组。版本发生变化时返回 409，下次同步重新读取清单。

两者要求 `Authorization: Bearer <同步密钥>`，响应禁止缓存，不接受浏览器 Origin；同步密钥不能操作管理面板。密钥未配置时接口返回 404。它只读取各账户已保存的 HME Web 会话，不重新登录、不接受条款，也不创建隐私邮箱。仅完成受信任设备普通登录、尚未完成隐私邮箱 Web 登录的账户计入 `unavailable`；应先在本服务完成其 Web 验证。目标会再次向 Apple 验证会话，实际可用性以 Apple 返回为准。

会话包含可用的认证材料，应只将同步密钥交给受信任的目标服务，并通过 HTTPS 或可信本机连接访问。目标加密保存密钥和会话，普通设置读取不返回密钥原文。

## 构建与真实运行时检查

```sh
docker buildx build --platform linux/arm64 -t appleservices:arm64 --load .
docker buildx build --platform linux/amd64 -t appleservices:amd64 --load .
docker compose run --rm --entrypoint /app/bin/portablecheck appleservices -nac
```

`portablecheck` 不使用 Apple 账号密码，会向 Apple 申请一次独立 APS 身份，在临时私有目录中验证真实 ADI 认证头、六位码生成及重启复用；加 `-nac` 还验证 NAC 初始化、Apple 会话交换与签名。退出会清理临时状态，只打印阶段和长度，不打印码、令牌或身份值。

构建需联网获取上游代码和模块。公共资源按摘要缓存，NAC 源文件在官方源不可达时可从固定 commit 的 CDN 副本获取，仍执行相同摘要校验。国内网络可按需指定软件源：

```sh
docker compose build \
  --build-arg APT_MIRROR=https://mirrors.tuna.tsinghua.edu.cn \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  --build-arg PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple
docker compose up -d --no-build
```

该检查不等于真实账号受信任、六位码被 Apple 登录接受，或购买成功。登录/家庭/HME 等业务仍以 Apple 实际返回为准；迁移后的账号端到端验收与匿名运行时验收应分别记录。构建成功和 `/healthz` 正常不能替代这些验证。

## GitHub Actions 自动部署

`.github/workflows/deploy.yml` 会在 `master` 推送后构建并推送 `linux/arm64` GHCR 镜像，然后通过 SSH 登录服务器执行 `docker compose pull` 和重启。仓库需要配置以下 Actions secrets：

```text
DEPLOY_HOST=140.238.56.209
DEPLOY_USER=muhaoxing
DEPLOY_SSH_KEY=<服务器 SSH 私钥全文>
```

服务器首次部署前创建目录和两个运行时 secret。不要把这两个文件提交到仓库：

```sh
mkdir -p /home/muhaoxing/apps/applefamily/secrets
chmod 700 /home/muhaoxing/apps/applefamily/secrets
openssl rand -hex 32 > /home/muhaoxing/apps/applefamily/secrets/admin_token
printf '%s\n' 'change-this-password' > /home/muhaoxing/apps/applefamily/secrets/admin_password
chmod 444 /home/muhaoxing/apps/applefamily/secrets/admin_token /home/muhaoxing/apps/applefamily/secrets/admin_password
```

工作流会把域名反代配置写入服务器已有的 1Panel OpenResty，并将应用绑定到 `https://applefamily.shuangdeng.space`。请先添加 `applefamily.shuangdeng.space` 指向 `140.238.56.209` 的 A 记录，并在 1Panel 或 ACME 工具中为该域名申请 HTTPS 证书。工作流不会覆盖 `account-data` 命名卷，也不会执行 `docker compose down -v`。

NAC 所用上游 Python hooks、Apple 验证模块、XNU 汇编均固定版本/摘要。镜像构建时验证 XNU 全部五组上游已知答案；不会使用上游的 donor `data.plist`。Store/SAP 所需的独立校验资产由现有实现按需下载到容器缓存，账号状态与这些代码缓存分开。

## 验证记录（2026-10-31）

在 Apple Silicon Mac 的 Linux ARM64 容器中，已实际验证独立 APS 证书与令牌申请、原生 Go courier 连接与令牌绑定、ADI 认证头与六位码生成、关闭后复用同一 provisioning，以及 NAC 初始化、Apple 会话交换与签名。ARM64 运行时通过镜像内显式 QEMU 执行 ADI，没有依赖宿主机的 binfmt 来启动该辅助进程。

最终 ARM64 和 AMD64 镜像均构建成功，并在同一 Mac 上以 Docker 通过上述匿名检查，运行时移除全部 capabilities 并启用 no-new-privileges。AMD64 镜像的测试使用 Docker 跨架构执行，未在独立 x86 Linux 主机上复验。

Compose 已验证匿名拒绝、管理员页面与 Bearer API、浏览器 CSRF、Host/Origin 校验、非 root 运行、健康检查、卷内数据重启保留及 SIGTERM 正常退出。Go 全量测试、相关竞态检查、Linux ARM64 容器内的面板/ADI/后台测试通过。

未使用真实账号执行迁移后的登录、设备受信任、验证码被 Apple 接受、家庭/HME 操作或订阅购买验收；这些能力的现有实现和入口保留，不能把上述匿名运行时验证描述为全部账号业务已验收。
