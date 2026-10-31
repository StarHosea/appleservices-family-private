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

打开 `http://127.0.0.1:18741`。HTTP Basic 用户名由 `APPLESERVICES_ADMIN_USERNAME` 设置（默认 `admin`），密码由 `secrets/admin_password` 提供；若未设置密码文件则兼容使用 `secrets/admin_token`。管理员 API 也可使用 `Authorization: Bearer` 携带 `secrets/admin_token` 的内容。密码文件至少 8 个字符，token 至少 32 个字符。

“家庭共享”卡片可读取全部当前家庭成员的账号，并标记组织者（母号）及当前账号；组织者不必预先添加到本服务。成员账号只在管理员认证后的页面/API 中显示，并随读取时间保存在账号私有缓存中。工具通过临时私有文件传递这些字段，日志仍只输出数量与角色，不包含成员账号、DSID、联系方式或原始响应。升级后若只有旧的家庭摘要缓存，点击“重新核验 Apple”读取成员账号。

隐藏邮件使用账号独立的 `hme-session.json`，存放在同一持久数据卷。文件原子写入、权限为 `0600`，保存 Web 登录/信任令牌及带原始作用域和有效期的 Cookie，不保存密码。容器重启后优先复用 Cookie；Apple 要求续期时先尝试已保存的 Web 令牌，仍无法恢复才显示密码备用入口。首次 Web 验证完成后、地区跳转或条款处理前即保存会话，减少中断后的重复验证。Apple 主动撤销或过期的凭据仍可能需要重新验证。

页面在账号及家庭状态就绪后自动读取地址，每次打开页面只自动尝试一次，失败原因留在卡片中。地址列表允许读取较大的账号池并分批显示。Apple 返回数量上限错误 `-41012` 时，在账号会话中保存已核实的上限状态，页面显示原因并将生成按钮标记为“已达 Apple 上限”；刷新页面或重启后仍保留。重新读取列表确认地址数量减少后，允许再次尝试生成。程序不会自动停用或删除已有地址。

```sh
docker compose ps
docker compose logs --tail 30
docker compose down
```

账号状态保存在 `account-data` 命名卷，停止/升级容器保留状态。不要使用 `docker compose down -v`，除非明确要删除全部身份与会话。首次部署使用空卷；不要直接混入其他账号的证书或 Windows 机器状态。本次变更未验证旧 Windows 状态的直接迁移。

“我的设备”会单独显示设备查询失败原因。iCloud 已激活或后台已登录，不代表设备管理凭据仍有效；Apple 返回 HTTP 401 时，需要点击“重新连接账号”更新凭据，保存旧会话并不能自动恢复。最近成功读取的设备显示字段保存在各账号私有的 `device-status.json` 中，绑定工作区和登录账号，不包含原始响应、设备 ID、序列号或认证令牌。重启、后台停止或查询失败时显示上次记录及读取时间；只有成功查询返回的空列表才显示 0 台设备。

## 首次登录返回 repair 后续步骤

Apple 在密码认证阶段返回 `au=repair` 时，程序不会将它当作短信挑战或成功登录。页面显示“客户端认证流程尚未完成”，保留账号的独立设备身份，并停止本次登录。这个返回值表示当前客户端尚未处理的后续认证步骤，不能据此断定账户官网存在待修复事项、密码错误、短信限流或需要接受条款。`account.apple.com` 能正常登录，也不能证明该原生认证步骤已完成。

再次连接时，诊断只记录预期账号字段、身份令牌、PET 和已知 URL 字段是否存在，不记录字段值或原始响应。按本次返回继续排查客户端兼容性；当前实现没有执行 repair 后续步骤，不能宣称已修复登录。

## 远程访问

Compose 默认仅把端口发布到宿主机回环地址。使用 HTTPS 反向代理时，将 `APPLESERVICES_PUBLIC_URL` 设为浏览器访问的精确来源（例如 `https://apple.example.com`），然后重新启动。代理需保留该来源的 `Host`，并转发 `Authorization`。无需启用跨域 CORS。

非回环监听必须配置管理员凭据和公开来源；未配置时拒绝启动。匿名请求不能读取页面或 API；唯一匿名端点 `/healthz` 只返回进程 HTTP 健康状态。浏览器操作还要求独立 CSRF 凭据。程序不信任任意 `X-Forwarded-Host`。

已有 API 保持原来的 `/api/*` 路由。外部客户端使用 `Authorization: Bearer <管理员凭据>`，且 `Host` 必须匹配公开来源；凭据不要放入 URL。此管理员可操作全部账号，不能当作多租户隔离服务。

配置对应环境变量：

| 环境变量 | 用途 |
| --- | --- |
| `APPLESERVICES_DIR` | 持久状态目录；容器默认 `/data` |
| `APPLESERVICES_LISTEN` | HTTP 监听地址；容器默认 `0.0.0.0:18741` |
| `APPLESERVICES_PUBLIC_URL` | 浏览器精确来源，不含路径 |
| `APPLESERVICES_ADMIN_TOKEN_FILE` | 至少 32 字符的管理员凭据文件 |
| `APPLESERVICES_ADMIN_USERNAME` | HTTP Basic 管理员用户名，默认 `admin` |
| `APPLESERVICES_ADMIN_PASSWORD_FILE` | HTTP Basic 管理员密码文件，至少 8 字符 |
| `APPLESERVICES_ADI_LIBRARY` | 固定 Linux 模块的路径；镜像已配置 |
| `APPLESERVICES_ADI_FENCE` | 同一镜像编译的 ADI 辅助进程传输隔离库；镜像已配置 |

SIGTERM 会停止该服务管理的账号后台并保留身份。控制端口仍只监听容器回环地址，不对外发布。镜像以 UID/GID 10001 运行，Compose 不授予 Linux capabilities 或特权。

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
chmod 600 /home/muhaoxing/apps/applefamily/secrets/admin_token /home/muhaoxing/apps/applefamily/secrets/admin_password
```

工作流会把域名反代配置写入服务器已有的 1Panel OpenResty，并将应用绑定到 `http://applefamily.shaungdeng.space`。请先添加 `applefamily.shaungdeng.space` 指向 `140.238.56.209` 的 A 记录；DNS 生效后，在 1Panel 为该域名申请 HTTPS 证书，并把反代配置的 `listen 80` 入口改为 HTTPS 重定向和证书配置。工作流不会覆盖 `account-data` 命名卷，也不会执行 `docker compose down -v`。

NAC 所用上游 Python hooks、Apple 验证模块、XNU 汇编均固定版本/摘要。镜像构建时验证 XNU 全部五组上游已知答案；不会使用上游的 donor `data.plist`。Store/SAP 所需的独立校验资产由现有实现按需下载到容器缓存，账号状态与这些代码缓存分开。

## 验证记录（2026-10-31）

在 Apple Silicon Mac 的 Linux ARM64 容器中，已实际验证独立 APS 证书与令牌申请、原生 Go courier 连接与令牌绑定、ADI 认证头与六位码生成、关闭后复用同一 provisioning，以及 NAC 初始化、Apple 会话交换与签名。ARM64 运行时通过镜像内显式 QEMU 执行 ADI，没有依赖宿主机的 binfmt 来启动该辅助进程。

最终 ARM64 和 AMD64 镜像均构建成功，并在同一 Mac 上以 Docker 通过上述匿名检查，运行时移除全部 capabilities 并启用 no-new-privileges。AMD64 镜像的测试使用 Docker 跨架构执行，未在独立 x86 Linux 主机上复验。

Compose 已验证匿名拒绝、管理员页面与 Bearer API、浏览器 CSRF、Host/Origin 校验、非 root 运行、健康检查、卷内数据重启保留及 SIGTERM 正常退出。Go 全量测试、相关竞态检查、Linux ARM64 容器内的面板/ADI/后台测试通过。

未使用真实账号执行迁移后的登录、设备受信任、验证码被 Apple 接受、家庭/HME 操作或订阅购买验收；这些能力的现有实现和入口保留，不能把上述匿名运行时验证描述为全部账号业务已验收。
