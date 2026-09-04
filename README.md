# GitHub Copilot Proxies (ghecopilot)

[中文](README.md) | [English](README.en.md)

一个 **GitHub Copilot 反向代理服务器**，让你使用Coiplot CLI、VS Code、Visual Studio 2026等编辑器中的 GitHub Copilot 插件时，将 AI 请求转发到你**自建的 OpenAI 兼容服务**（如 Ollama、vLLM、OneAPI 等），从而摆脱对 GitHub 官方 Copilot 服务的依赖，实现**本地化 / 私有化部署**。

> 项目采用 [MIT License](LICENSE) 开源。

---


## 项目简介

GitHub Copilot 插件（VS Code / VS2026）默认将 AI 请求发送到 GitHub 官方服务器（`api.githubcopilot.com`、`proxy.githubcopilot.com` 等）。本项目通过以下手段实现"劫持"：

1. **域名劫持**：将 Copilot 插件请求的官方域名（如 `*.ghe.com`）通过 hosts 文件指向 `127.0.0.1`。
2. **自签名证书**：自动生成自签名 CA 证书，使 HTTPS 请求能被本地服务正确解密。
3. **反向代理**：将 Copilot 插件发来的对话请求（`/chat/completions`、`/responses`）透明转发到你指定的 OpenAI 兼容后端。
4. **请求改写**：在转发前对请求体做轻量改写——`auto`/空模型替换、模型存在性校验、同一会话模型一致性保持、按需注入采样参数，并清洗 `edit_file` 工具描述中的代码块标记。

最终效果：**Copilot 插件以为自己在和 GitHub 官方通信，实际上所有接口与 AI 能力都由你的本地模型提供。**

---

## 核心特性

| 特性 | 说明 |
|------|------|
| 🔄 **反向代理** | 将 Copilot 请求转发到任意 OpenAI 兼容服务（Ollama、vLLM、OneAPI、DeepSeek 等） |
| 🔐 **GHE完整认证** | 支持设备码（Device Code）和 OAuth 授权码两种登录流程，签发 JWT Token |
| 📜 **自签名证书** | 启动时自动生成 CA 证书和服务器证书，支持热更新 |
| 🌐 **自动 hosts 配置** | 启动时自动将所需域名写入系统 hosts 文件 |
| 🤖 **模型路由** | 支持 `auto` 模型自动映射、模型存在性校验、同一会话模型一致性保持 |
| 💬 **对话代理** | 纯反向代理透传 SSE 流式响应，转发前按需改写模型与采样参数 |
| 🛠️ **Agent/工具支持** | 支持 Copilot Agent 工具调用（file_search 等） |
| 📊 **遥测模拟** | 模拟 GitHub 遥测接口，避免插件报错 |
| 🌍 **多客户端支持** | VS Code、VS 2022、Copilot CLI 等 |
| 📝 **请求日志** | 按 `X-Interaction-Id` 分目录记录每次对话的完整请求 JSON |

---

## 工作原理

### 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                  编辑器 + Copilot 插件 + Copilot CLI                 │
│             (VS Code / VS2026 / Copilot CLI)                        │
│                                                                     │
│   插件配置指向: https://*.ghe.com  (通过 hosts 指向 127.0.0.1)        │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTPS (自签名证书)
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    ghecopilot (本项目, Go + Gin)                       │
│                                                                     │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────────────┐   │
│  │  认证模块    │  │  代理模块     │  │  请求改写模块              │  │
│  │  (auth)     │  │  (proxy)     │  │  (chat/completions/       │  │
│  │  设备码/OAuth│  │  反向代理到   │  │   responses)              │  │
│  │  JWT 签发   │  │  OpenAI 后端 │  │  模型替换 + 参数注入       │  │
│  └─────────────┘  └──────────────┘  └───────────────────────────┘  │
│                                                                     │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────────────┐  │
│  │  证书管理    │  │  hosts 管理  │  │  模型缓存                  │  │
│  │  自签名 CA  │  │  自动写入    │  │  模型一致性 (TTL 30min)    │  │
│  └─────────────┘  └──────────────┘  └───────────────────────────┘  │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTP (OpenAI 兼容 API)
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│              OpenAI 兼容后端 (UPSTREAM_API_BASE_URL)                 │
│   (Ollama / vLLM / OneAPI / DeepSeek / 其他)                        │
└─────────────────────────────────────────────────────────────────────┘
```

### 请求处理流程

以 VS Code Copilot 对话为例，完整请求链路如下：

```
1. 用户在 VS Code 中打开 Copilot Chat 面板，输入问题
2. Copilot 插件向 https://api.my.ghe.com/copilot_internal/v2/token 请求 Token
   └─> ghecopilot 返回伪装的 Copilot Token (含 endpoints 配置)
       └─> endpoints 中指定了 api/proxy/telemetry 地址
3. 插件向 https://api.my.ghe.com/chat/completions 发送对话请求
   └─> ghecopilot 中间件拦截 (Host 匹配)
       ├─> 读取请求体
       ├─> 清洗 messages.0.content 与 edit_file 工具描述中的 ``` 代码块
       ├─> model=auto/空 → 替换为 COPILOT_AUTO_MODEL
       ├─> 模型不在上游列表 → 替换为 COPILOT_CHAT_DEFAULT_MODEL
       ├─> 检查模型一致性 (mapModels 缓存)
       ├─> 指定模型注入 repetition_penalty / temperature / top_p
       ├─> 记录请求 JSON 到 logs/{interactionId}/
       └─> 转发到 UPSTREAM_API_BASE_URL/v1/chat/completions
4. 后端返回 SSE 流式响应
   └─> ghecopilot 逐行透传 SSE 数据给插件
       └─> 插件渲染对话内容
```

### 认证机制

项目支持两种认证流程，均模拟 GitHub 官方的 OAuth 设备码 / 授权码流程：

#### 设备码流程 (Device Code Flow)

```
1. 插件 POST /login/device/code {client_id}
   └─> ghecopilot 生成 device_code + user_code，返回 verification_uri
       └─> verification_uri = https://my.ghe.com/login/device?user_code=XXX

2. 插件打开浏览器访问 verification_uri
   └─> ghecopilot 返回登录页面 (login.html)
       └─> 用户输入 user_code (可选输入 LOGIN_PASSWORD)

3. 插件轮询 POST /login/oauth/access_token {client_id, device_code}
   └─> ghecopilot 检查授权状态
       ├─> 未完成: 返回 {error: "authorization_pending"}
       └─> 已完成: 签发 JWT Token (有效期 100 年)
```

#### OAuth 授权码流程 (Authorization Code Flow)

```
1. 插件 GET /login/oauth/authorize?client_id=...&redirect_uri=...&state=...
   └─> ghecopilot 生成 oauth_code，缓存 client_id ↔ code 映射
       └─> 如果 state 是 URL (VS Code 本地回调)，直接 302 跳转
           └─> 跳转 URL 携带 code + access_token (JWT)

2. 插件 POST /login/oauth/access_token {client_id, code}
   └─> ghecopilot 从缓存查找 code 对应的 client_id
       └─> 签发 JWT Token
```

#### JWT Token 结构

```json
{
  "user_display_name": "github",
  "card_code": "xxx",
  "client": "Iv1.b507a08c87ecfe98",
  "exp": 4102444800,
  "iat": 1700000000,
  "iss": "user"
}
```

JWT 使用 `JWT_SECRET` 环境变量签名，修改后需重新登录。

#### Copilot Token (伪装 Token)

`/copilot_internal/v2/token` 接口返回的 Token 格式为：

```
chat=1;exp=3935931593;mcp=1;sku=copilot_for_business_seat;st=dotcom;tid=uuid;u=github;8kp=1:sha256签名
```

其中 `8kp=1:` 后的签名 = `SHA256(token_string + ";salt=" + JWT_SECRET)`。

该 Token 中的 `endpoints` 字段告诉插件后续请求应发往哪些地址：
- `api` → `COPILOT_API_BASE_URL`
- `proxy` → `COPILOT_PROXY_BASE_URL`
- `telemetry` → `COPILOT_TELEMETRY_BASE_URL`

### 证书与域名管理

#### 自签名证书

启动时 `certificate.InitCertificates()` 执行以下操作：

1. 检查 `cert/` 目录下是否已有证书文件
2. 如果没有，生成：
   - **CA 证书** (`ca.pem` / `ca.key`)：RSA 2048 自签名 CA
   - **服务器证书** (`ssl.pem` / `ssl.key`)：由 CA 签发，SAN 包含 `HOSTS_DOMAINS` 中所有域名
3. 启动后台 goroutine 监控证书文件变化，变化时触发 HTTPS 服务器热重载

#### 域名配置

`HOSTS_DOMAINS` 环境变量指定需要劫持的域名列表，例如：

```
HOSTS_DOMAINS=my.ghe.com,api.my.ghe.com,copilot-proxy.my.ghe.com,copilot-telemetry-service.my.ghe.com
```

启动时 `configureHosts()` 会：
1. 解析域名列表
2. 检查系统 hosts 文件 (`C:\Windows\System32\drivers\etc\hosts`)
3. 将缺失的域名添加为 `127.0.0.1` 映射
4. 通配符域名 `*.domain.com` 会同时添加 `*.domain.com` 和 `domain.com`

> ⚠️ 修改 hosts 文件需要管理员权限。如果自动配置失败，请手动添加。

### 模型路由与一致性

#### auto 模型映射

当 Copilot 插件发送 `model: "auto"` 或空模型时，ghecopilot 会将其替换为 `COPILOT_AUTO_MODEL` 环境变量指定的模型。

#### 模型存在性校验

Copilot CLI 可能请求上游不存在的模型（如 `claude-sonnet-4`），ghecopilot 会检查模型是否存在于上游 `/v1/models` 列表中，不存在时替换为 `COPILOT_CHAT_DEFAULT_MODEL` 指定的模型，避免上游返回 503。

#### 会话模型一致性

Copilot 插件在一次 Agent 对话中可能多次请求，且可能切换模型。ghecopilot 使用 `mapModels` 缓存（TTL 30 分钟，上限 100000 条）以 `X-Interaction-Id` 为键记录首次使用的模型，后续同一 `interactionId` 的请求会强制使用首次的模型，避免多轮对话中模型不一致导致上下文混乱。

同一请求多轮agent保持模型一致，有的时候，你指定的是本地部署的模型，比如Qwen3.8-27B随便用，token完全自由

但在同一请求多轮agent任务的时候，有的时候会自动切换到其它的收费模型，可能你并不想这样

```go
// 同一请求多轮agent保持模型一致
if firstModel, has := mapModels.getOrSet(interactionId, model); has && model != firstModel {
    body, _ = sjson.SetBytes(body, "model", firstModel)
}
```

#### 模型列表

`/models` 接口从上游 `/v1/models` 动态获取模型列表（首次构建后永久缓存，如需刷新，重启服务即可），并为每个模型填充完整的 Copilot 模型元数据（capabilities、limits、billing 等），使插件的模型选择器能正常显示。其中 `COPILOT_CHAT_DEFAULT_MODEL` 对应的模型标记为 `is_chat_default`，`COPILOT_CHAT_FALLBACK_MODEL` 对应的模型标记为 `is_chat_fallback`。

### 对话处理

对话请求（`/chat/completions`、`/responses`）由 `OpenAIProxy` 中间件以**纯反向代理**方式处理：请求体经轻量改写后透传到上游，SSE 流式响应原样返回给插件，不做协议格式转换。

改写规则（仅对 `*chat/completions` 路径生效）：

1. **代码块清洗**：移除 `messages.0.content` 和 `edit_file` 工具描述中的 ``` 代码块标记
2. **auto 模型替换**：`model` 为 `auto` 或空时，替换为 `COPILOT_AUTO_MODEL`
3. **模型存在性校验**：模型不在上游 `/v1/models` 列表中时（如 Copilot CLI 默认传的 `claude-sonnet-4`），替换为 `COPILOT_CHAT_DEFAULT_MODEL`，避免上游返回 503
4. **会话模型一致性**：以 `X-Interaction-Id` 为键，同一会话多轮 Agent 请求强制使用首次的模型，避免中途切换到其他（收费）模型
5. **采样参数注入**：模型在 `CHAT_CUSTOM_PARAMS_MODELS` 列表中时，注入 `repetition_penalty` / `temperature` / `top_p`（值为 0 或空则不注入）
6. **请求日志**：改写后的完整请求体异步写入 `logs/{interactionId}/`
7. **SSE 透传**：上游响应逐行透传，客户端断开时静默中止（`http.ErrAbortHandler`）

拦截的 Host 范围：

- `localhost:11434`（Ollama 兼容端口，全部路径）
- `api.githubcopilot.com` 的 `/responses`、`/chat/completions`
- `api.my.ghe.com` 的 `/chat/completions*`

其余请求（模型列表、Token、用户信息等）由本地 Gin 路由处理。

---

## 环境要求

| 依赖 | 版本要求 | 说明 |
|------|---------|------|
| Go | 1.26+ | 编译 ghecopilot |
| Windows | 10/11 | 主要目标平台（hosts 自动配置依赖 Windows 路径） |
| OpenAI 兼容后端 | - | Ollama / vLLM / OneAPI 等，需支持 `/v1/chat/completions` |
| Python | 3.9+ | 仅 Embedding 服务需要（如果需要的话） |
| 管理员权限 | - | 修改 hosts 文件需要 |

---

## 快速开始

### 1. 克隆项目

```bash
git clone <repo-url>
cd ghecopilot
```

### 2. 配置环境变量

```bash
# 从模板复制
copy .env.example .env
```

编辑 `.env` 文件，**至少修改以下配置**：

```env
# 上游 OpenAI 兼容服务地址（必填），建议使用New-API
UPSTREAM_API_BASE_URL=http://192.168.2.207:3000
UPSTREAM_API_KEY=sk-your-api-key

# 自动模型（Copilot 选择 auto 时使用）
COPILOT_AUTO_MODEL=Qwen3.8-27B

# 兜底模型（请求的模型不在上游列表时使用）
COPILOT_CHAT_DEFAULT_MODEL=Qwen3.8-27B

# Fallback 模型
COPILOT_CHAT_FALLBACK_MODEL=Qwen3.8-27B

# 域名配置（根据你的网络环境修改）
HOSTS_DOMAINS=my.ghe.com,api.my.ghe.com,copilot-proxy.my.ghe.com,copilot-telemetry-service.my.ghe.com
COPILOT_MAIN_BASE_URL=https://my.ghe.com
COPILOT_API_BASE_URL=https://api.my.ghe.com
COPILOT_PROXY_BASE_URL=https://copilot-proxy.my.ghe.com
COPILOT_TELEMETRY_BASE_URL=https://copilot-telemetry-service.my.ghe.com

# JWT 密钥（建议修改）
JWT_SECRET=your-random-secret
```

### 3. 启动服务

**方式一：使用启动脚本（推荐）**

```powershell
.\start.ps1
```

脚本会自动：
1. 复制 `.env.example` → `.env`（如果 `.env` 不存在）
2. 停止旧的 ghecopilot 进程
3. 编译 `ghecopilot.exe`
4. 启动服务

**方式二：手动编译运行**

```bash
go build -o ghecopilot.exe
set GIN_MODE=release
.\ghecopilot.exe
```

### 4. 验证服务

启动成功后，终端会显示：

```
[HOSTS] ✅ 所有域名已配置
Starting HTTP server on 0.0.0.0:11434
Starting HTTPS server on 0.0.0.0:443
```

Windows 会弹出一个"运行成功"消息框。

访问 `https://my.ghe.com/help` 查看配置指南页面。

### 5. 配置客户端

按照下方 [客户端配置](#客户端配置) 章节配置你的编辑器。

---

## 配置说明

### 环境变量详解

所有配置通过 `.env` 文件或系统环境变量设置。

#### 通用配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `11434` | HTTP 端口。设为 `11434` 可兼容 Ollama 默认端口（不是必须11434端口的，如果vs2026中指定的是Ollama，则只能填写http://localhost:11434，所以默认11434端口也为了方便） |
| `HTTPS_PORT` | `443` | HTTPS 端口。如与本地冲突可更改，需自行反代 |
| `HOST` | `0.0.0.0` | 监听地址 |

#### 反向代理配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `UPSTREAM_API_BASE_URL` | - | **必填**。上游 OpenAI 兼容服务地址，建议使用New-Api，如 `http://192.168.2.207:3000` |
| `UPSTREAM_API_KEY` | - | **必填**。上游 API Key。 |

#### 域名与 hosts 配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `HOSTS_DOMAINS` | - | 需要劫持的域名列表（逗号分隔）。启动时自动写入 hosts |
| `COPILOT_MAIN_BASE_URL` | `https://my.ghe.com` | 主服务地址（登录页面等）。**必须 HTTPS** |
| `COPILOT_API_BASE_URL` | `https://api.my.ghe.com` | API 服务地址。域名 `api` 前缀**必须固定** |
| `COPILOT_PROXY_BASE_URL` | `https://copilot-proxy.my.ghe.com` | 代理服务地址。域名 `copilot-proxy` 前缀**必须固定** |
| `COPILOT_TELEMETRY_BASE_URL` | `https://copilot-telemetry-service.my.ghe.com` | 遥测服务地址。域名 `copilot-telemetry-service` 前缀**必须固定** |

> ⚠️ 域名前缀规则：`api`、`copilot-proxy`、`copilot-telemetry-service` 是 Copilot 插件硬编码的前缀，不能更改。主域名`.ghe.com`也不能更改，因为官方GHE只认这个域名，不过好像`*.ghe.com.example.com`也行，大概是因为校验了域名中包含`.ghe.com`的原因。

#### 模型配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `COPILOT_AUTO_MODEL` | - | 当 Copilot 模型选择为 `auto`（或为空）时使用的模型名 |
| `COPILOT_CHAT_DEFAULT_MODEL` | - | 请求的模型不在上游列表时使用的兜底模型，同时标记为模型列表的 `is_chat_default` |
| `COPILOT_CHAT_FALLBACK_MODEL` | - | 标记为模型列表的 `is_chat_fallback` 的模型 |
| `CHAT_REPETITION_PENALTY` | 空 | 对话模型的重叠惩罚，范围 `[1.0, 2.0]` |
| `CHAT_TEMPERATURE` | 空 | 对话模型温度，范围 `[0, 2]` |
| `CHAT_TOP_P` | 空 | 对话模型核采样，范围 `[0, 1.0]` |
| `CHAT_CUSTOM_PARAMS_MODELS` | - | 允许设置上述参数的模型列表（逗号分隔） |

#### 认证配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `JWT_SECRET` | - | **必填**。JWT 签名密钥。修改后需重新登录 |
| `LOGIN_PASSWORD` | 空 | 登录页面访问密码。空=不设置 |
| `COPILOT_TOKEN_TTL_SECONDS` | `2147483647` | 伪装 Token 有效期 (秒)。自用可设很大 |

#### 其他配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `HTTP_CLIENT_TIMEOUT` | `300` | 全局 HTTP 请求超时 (秒) |
| `AGENT_TOOLS` | 空 | 自定义 Agent 工具定义 (JSON 数组) |

---

## 客户端配置（服务启动后，可打开`https://my.ghe.com/help` 查看配置指南）

作者重度使用的是Copilot CLI, VS Code, VS2026这三个，其它带Copilot插件的客户端没用过，请自行测试

### Copilot CLI
1. 安装Copilot CLI
2. 命令行输入`copilot`启动
3. 输入`/login`
4. 选择2 GitHub Enterprise Cloud (*.ghe.com)
5. 输入`https://my.ghe.com`
6. `Sign in with your browser (recommended)` 与 `Sign in with a device code` 随便选一个
7. 完成登录

### VS Code

1. 安装 **GitHub Copilot** 插件
2. 打开 `settings.json`（参考路径`C:\Users\{username}\AppData\Roaming\Code\User\settings.json`），添加以下配置：

```json
{
    "github-enterprise.uri": "https://my.ghe.com",
    "github.copilot.advanced": {
        "authProvider": "github-enterprise"
    }
}
```

3. 重启 VS Code，点击 Copilot 登录，选择 **GitHub Enterprise**，输入 `https://my.ghe.com`
4. 浏览器会打开登录页面，输入设备码完成登录

### Visual Studio 2026

1. 更新 VS 2026 最新版
2. 开启 GitHub Enterprise 账户支持：
   - **工具** → **环境** → **账户** → 勾选 **包含 GitHub Enterprise 服务器账户**
3. 点击 **添加 GitHub 账户**，切换到 **GitHub Enterprise** 选项卡
4. 输入 `https://my.ghe.com`
5. 完成设备码登录

---

## API 接口一览

### 认证接口

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/help` | 配置指南页面 |
| `POST` | `/login/device/code` | 启动设备码登录流程 |
| `POST` | `/login/device` | 提交设备码授权 |
| `GET` | `/login/device` | 设备码登录页面 |
| `POST` | `/login/oauth/access_token` | 获取 OAuth Access Token |
| `GET` | `/login/oauth/authorize` | OAuth 授权 |
| `GET` | `/redirect` / `/callback` | OAuth 回调 |
| `GET` | `/site/sha` | Enterprise 验证 |
| `GET` | `/login/config` | 登录页面配置 |
| `GET` | `/github/login/device/code` | GitHub 模拟登录页面 |
| `POST` | `/github/login/device/code` | 获取 GitHub 设备码 |
| `POST` | `/github/login/ghu-token` | 获取 GitHub 用户 Token |

### Copilot 核心接口

| 方法 | 路径 | 认证 | 说明 |
|------|------|------|------|
| `ANY` | `/models` | 无 | 模型列表 |
| `ANY` | `/models/*model-id` | 无 | 模型详情 |
| `POST` | `/auto` | 无 | 模型详情 (auto) |
| `ANY` | `/_ping` | 无 | 健康检查 |
| `POST` | `/telemetry` | 无 | 遥测数据 (空接口，只为了不报404)|
| `ANY` | `/agents` | 无 | Agent 列表 |
| `ANY` | `/agents/swe/models` | 无 | SWE 模型列表 |
| `ANY` | `/copilot_internal/user` | 无 | 内部用户信息 |
| `ANY` | `/copilot_internal/managed_settings` | 无 | 管理设置 |
| `ANY` | `/copilot/mcp_registry` | 无 | MCP 注册表 |
| `ANY` | `/embeddings/models` | 无 | Embedding 模型列表 |
| `GET` | `/copilot_internal/v2/token` | AccessToken | **Copilot Token 签发** |
| `GET` | `/user` | AccessToken | 用户信息 |
| `GET` | `/user/orgs` | AccessToken | 用户组织 |
| `GET` | `/api/v3/user` | AccessToken | 用户信息 (v3) |
| `GET` | `/api/v3/user/orgs` | AccessToken | 用户组织 (v3) |
| `GET` | `/teams/:teamID/memberships/:username` | AccessToken | 团队成员 |
| `POST` | `/chunks` | AccessToken | 文本分块 + 嵌入 |
| `GET` | `/agents/:name` | 无 | Agent 定义 |
| `POST` | `/embeddings` | Token | **Embedding** |
| `GET` | `/api/v3/meta` | 无 | API v3 meta |
| `GET` | `/api/v3/` | 无 | API v3 根 |
| `GET` | `/` | 无 | API v3 根 |
| `ANY` | `/api/v3/copilot_internal/user` | 无 | 内部用户 (v3) |

### 认证说明

- **AccessToken**：JWT Token，通过 `Authorization: Bearer <jwt>` 传递
- **Token**：Copilot 伪装 Token，通过 `Authorization: Bearer <copilot_token>` 传递

---

## 常见问题

### Q: 启动时提示"端口已被占用"

检查 `PORT` (11434) 和 `HTTPS_PORT` (443) 是否被其他程序占用。可以修改 `.env` 中的端口号，或停止占用端口的程序，HTTPS只能使用443端口，带端口的域名不被GitHub Enterprise识别。

### Q: 浏览器访问 `https://my.ghe.com` 提示证书不安全

证书不信任，会导致服务无法使用，必须是证书安全可信任才能登录本地my.ghe.com，可以将 `cert/ca.pem` 导入系统信任证书存储, 第一运行的时候，会提示是否导入根证书，需要点确定，别点取消。

### Q: hosts 文件自动配置失败

修改 hosts 文件需要**管理员权限**。请：
1. 以管理员身份运行终端
2. 或手动编辑 `C:\Windows\System32\drivers\etc\hosts`，添加：
   ```
   127.0.0.1 my.ghe.com
   127.0.0.1 api.my.ghe.com
   127.0.0.1 copilot-proxy.my.ghe.com
   127.0.0.1 copilot-telemetry-service.my.ghe.com
   ```

### Q: Copilot 插件登录失败

1. 确认 `COPILOT_MAIN_BASE_URL` 配置正确且可访问
2. 确认 hosts 文件已正确配置
3. 确认浏览器能打开 `https://my.ghe.com/login/device?user_code=XXX`
4. 检查 `LOGIN_PASSWORD` 是否设置，如设置则登录时需输入

### Q: 对话没有响应

1. 只要上游服务正常，api key是对的，基本没问题

### Q: 上游返回 503 提示模型不存在

插件（尤其是 Copilot CLI）可能请求上游不存在的模型（如 `claude-sonnet-4`）。ghecopilot 会自动将其替换为 `COPILOT_CHAT_DEFAULT_MODEL` 指定的模型，请确认该变量已配置为上游实际存在的模型。

### Q: 修改 JWT_SECRET 后插件断连

修改 `JWT_SECRET` 后需要**重新登录** Copilot 插件，因为旧 Token 的签名已失效。

### Q: 如何部署到 Linux

项目主要面向 Windows（hosts 自动配置依赖 Windows 路径），但核心服务可在 Linux 上运行：
1. 手动配置 `/etc/hosts`
2. 证书和 HTTPS 正常工作
3. 将ca证书在windows中导入使其受信任
4. 将所有HOSTS_DOMAINS中的域名指向Linux服务所在机子的IP

---

## 许可证

[MIT License](LICENSE)

Copyright (c) 2026 mayb
