# GitHub Copilot Proxies (ghecopilot)

[中文](README.md) | [English](README.en.md)

A **GitHub Copilot reverse proxy server** that lets you use the GitHub Copilot plugin in Copilot CLI, VS Code, Visual Studio 2026, and other editors by forwarding AI requests to **your own OpenAI-compatible service** (such as Ollama, vLLM, OneAPI, etc.), freeing you from a dependency on GitHub's official Copilot service and enabling **local / private deployment**.

> The project is open-sourced under the [MIT License](LICENSE).

---

## Overview

The GitHub Copilot plugin (VS Code / VS2026) sends AI requests to GitHub's official servers (`api.githubcopilot.com`, `proxy.githubcopilot.com`, etc.) by default. This project "hijacks" that traffic through the following mechanisms:

1. **Domain hijacking**: Point the official domains requested by the Copilot plugin (e.g. `*.ghe.com`) to `127.0.0.1` via the hosts file.
2. **Self-signed certificates**: Automatically generate a self-signed CA certificate so that HTTPS requests can be correctly decrypted by the local service.
3. **Reverse proxy**: Transparently forward the plugin's chat requests (`/chat/completions`, `/responses`) to the OpenAI-compatible backend you specify.
4. **Request rewriting**: Perform lightweight rewrites on the request body before forwarding — replacing `auto`/empty models, validating model existence, keeping model consistency within the same session, injecting sampling parameters on demand, and cleaning up code-block markers in the `edit_file` tool description.

The end result: **the Copilot plugin believes it is talking to GitHub's official service, while in reality all interfaces and AI capabilities are provided by your local model.**

---

## Key Features

| Feature | Description |
|------|------|
| 🔄 **Reverse proxy** | Forwards Copilot requests to any OpenAI-compatible service (Ollama, vLLM, OneAPI, DeepSeek, etc.) |
| 🔐 **Full GHE authentication** | Supports both the Device Code and OAuth Authorization Code login flows, issuing JWT tokens |
| 📜 **Self-signed certificates** | Automatically generates CA and server certificates at startup, with hot reload support |
| 🌐 **Automatic hosts configuration** | Automatically writes the required domains into the system hosts file at startup |
| 🤖 **Model routing** | Supports automatic mapping of the `auto` model, model existence validation, and model consistency within the same session |
| 💬 **Chat proxy** | Pure reverse proxy that passes through SSE streaming responses, rewriting the model and sampling parameters before forwarding |
| 🛠️ **Agent/tool support** | Supports Copilot Agent tool calls (file_search, etc.) |
| 📊 **Telemetry simulation** | Simulates GitHub's telemetry endpoints to avoid plugin errors |
| 🌍 **Multi-client support** | VS Code, VS 2022, Copilot CLI, and more |
| 📝 **Request logging** | Records the full request JSON for each conversation in a directory keyed by `X-Interaction-Id` |

---

## How It Works

### Overall Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Editor + Copilot Plugin + Copilot CLI              │
│             (VS Code / VS2026 / Copilot CLI)                        │
│                                                                     │
│   Plugin configured to point at: https://*.ghe.com                  │
│   (pointed to 127.0.0.1 via the hosts file)                        │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTPS (self-signed certificate)
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    ghecopilot (this project, Go + Gin)               │
│                                                                     │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────────────┐   │
│  │  Auth module │  │  Proxy module │  │  Request rewrite module   │  │
│  │  (auth)     │  │  (proxy)     │  │  (chat/completions/       │  │
│  │  Device     │  │  Reverse     │  │   responses)              │  │
│  │  Code/OAuth │  │  proxy to    │  │  Model replacement +      │  │
│  │  JWT issue  │  │  OpenAI      │  │  parameter injection      │  │
│  └─────────────┘  │  backend     │  └───────────────────────────┘   │
│                   └──────────────┘                                  │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────────────────┐   │
│  │  Certificate │  │  Hosts       │  │  Model cache              │  │
│  │  management  │  │  management  │  │  Model consistency        │  │
│  │  Self-signed │  │  Auto-write  │  │  (TTL 30 min)             │  │
│  │  CA         │  │              │  │                           │  │
│  └─────────────┘  └──────────────┘  └───────────────────────────┘   │
└──────────────────────────────┬──────────────────────────────────────┘
                               │ HTTP (OpenAI-compatible API)
                               ▼
┌─────────────────────────────────────────────────────────────────────┐
│              OpenAI-compatible backend (UPSTREAM_API_BASE_URL)       │
│   (Ollama / vLLM / OneAPI / DeepSeek / others)                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Request Processing Flow

Taking a VS Code Copilot conversation as an example, the full request chain is as follows:

```
1. The user opens the Copilot Chat panel in VS Code and types a question
2. The Copilot plugin requests a token from https://api.my.ghe.com/copilot_internal/v2/token
   └─> ghecopilot returns a spoofed Copilot token (with endpoints configuration)
       └─> the endpoints specify the api/proxy/telemetry addresses
3. The plugin sends the chat request to https://api.my.ghe.com/chat/completions
   └─> ghecopilot middleware intercepts it (Host match)
       ├─> Reads the request body
       ├─> Cleans up ``` code blocks in messages.0.content and the edit_file tool description
       ├─> model=auto/empty → replaced with COPILOT_AUTO_MODEL
       ├─> Model not in the upstream list → replaced with COPILOT_CHAT_DEFAULT_MODEL
       ├─> Checks model consistency (mapModels cache)
       ├─> Injects repetition_penalty / temperature / top_p for the specified model
       ├─> Logs the request JSON to logs/{interactionId}/
       └─> Forwards to UPSTREAM_API_BASE_URL/v1/chat/completions
4. The backend returns an SSE streaming response
   └─> ghecopilot passes the SSE data through line by line to the plugin
       └─> The plugin renders the conversation content
```

### Authentication Mechanism

The project supports two authentication flows, both simulating GitHub's official OAuth Device Code / Authorization Code flows:

#### Device Code Flow

```
1. The plugin POSTs to /login/device/code {client_id}
   └─> ghecopilot generates a device_code + user_code and returns a verification_uri
       └─> verification_uri = https://my.ghe.com/login/device?user_code=XXX

2. The plugin opens the browser to the verification_uri
   └─> ghecopilot returns the login page (login.html)
       └─> The user enters the user_code (optionally enters LOGIN_PASSWORD)

3. The plugin polls POST /login/oauth/access_token {client_id, device_code}
   └─> ghecopilot checks the authorization status
       ├─> Not complete: returns {error: "authorization_pending"}
       └─> Complete: issues a JWT token (valid for 100 years)
```

#### Authorization Code Flow

```
1. The plugin GETs /login/oauth/authorize?client_id=...&redirect_uri=...&state=...
   └─> ghecopilot generates an oauth_code and caches the client_id ↔ code mapping
       └─> If state is a URL (VS Code local callback), it 302-redirects directly
           └─> The redirect URL carries code + access_token (JWT)

2. The plugin POSTs to /login/oauth/access_token {client_id, code}
   └─> ghecopilot looks up the client_id corresponding to the code from the cache
       └─> Issues a JWT token
```

#### JWT Token Structure

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

The JWT is signed with the `JWT_SECRET` environment variable; after changing it, you must log in again.

#### Copilot Token (Spoofed Token)

The token returned by the `/copilot_internal/v2/token` endpoint has the following format:

```
chat=1;exp=3935931593;mcp=1;sku=copilot_for_business_seat;st=dotcom;tid=uuid;u=github;8kp=1:sha256_signature
```

Where the signature after `8kp=1:` = `SHA256(token_string + ";salt=" + JWT_SECRET)`.

The `endpoints` field in this token tells the plugin which addresses to send subsequent requests to:
- `api` → `COPILOT_API_BASE_URL`
- `proxy` → `COPILOT_PROXY_BASE_URL`
- `telemetry` → `COPILOT_TELEMETRY_BASE_URL`

### Certificate and Domain Management

#### Self-Signed Certificates

At startup, `certificate.InitCertificates()` performs the following operations:

1. Checks whether certificate files already exist in the `cert/` directory
2. If not, generates:
   - **CA certificate** (`ca.pem` / `ca.key`): an RSA 2048 self-signed CA
   - **Server certificate** (`ssl.pem` / `ssl.key`): issued by the CA, with SANs containing all domains in `HOSTS_DOMAINS`
3. Starts a background goroutine that watches for certificate file changes and triggers an HTTPS server hot reload when they change

#### Domain Configuration

The `HOSTS_DOMAINS` environment variable specifies the list of domains to hijack, for example:

```
HOSTS_DOMAINS=my.ghe.com,api.my.ghe.com,copilot-proxy.my.ghe.com,copilot-telemetry-service.my.ghe.com
```

At startup, `configureHosts()`:
1. Parses the domain list
2. Checks the system hosts file (`C:\Windows\System32\drivers\etc\hosts`)
3. Adds any missing domains as `127.0.0.1` mappings
4. A wildcard domain `*.domain.com` adds both `*.domain.com` and `domain.com`

> ⚠️ Modifying the hosts file requires administrator privileges. If automatic configuration fails, add them manually.

### Model Routing and Consistency

#### auto Model Mapping

When the Copilot plugin sends `model: "auto"` or an empty model, ghecopilot replaces it with the model specified by the `COPILOT_AUTO_MODEL` environment variable.

#### Model Existence Validation

The Copilot CLI may request models that do not exist upstream (e.g. `claude-sonnet-4`). ghecopilot checks whether the model exists in the upstream `/v1/models` list, and if not, replaces it with the model specified by `COPILOT_CHAT_DEFAULT_MODEL`, avoiding a 503 from upstream.

#### Session Model Consistency

The Copilot plugin may make multiple requests within a single Agent conversation, and may switch models. ghecopilot uses the `mapModels` cache (TTL 30 minutes, cap of 100000 entries), keyed by `X-Interaction-Id`, to record the model first used; subsequent requests with the same `interactionId` are forced to use that first model, avoiding context confusion caused by inconsistent models across multiple turns.

Within a single request, multi-turn agent tasks keep the model consistent. Sometimes you specify a locally deployed model, e.g. Qwen3.8-27B, which is free to use with unlimited tokens.

But during multi-turn agent tasks within the same request, it may sometimes automatically switch to other paid models, which you may not want.

```go
// Keep the model consistent across multi-turn agent calls within the same request
if firstModel, has := mapModels.getOrSet(interactionId, model); has && model != firstModel {
    body, _ = sjson.SetBytes(body, "model", firstModel)
}
```

#### Model List

The `/models` endpoint dynamically fetches the model list from the upstream `/v1/models` (cached permanently after the first build; restart the service to refresh it), and fills in complete Copilot model metadata (capabilities, limits, billing, etc.) for each model so that the plugin's model selector displays correctly. The model corresponding to `COPILOT_CHAT_DEFAULT_MODEL` is marked as `is_chat_default`, and the model corresponding to `COPILOT_CHAT_FALLBACK_MODEL` is marked as `is_chat_fallback`.

### Chat Handling

Chat requests (`/chat/completions`, `/responses`) are handled by the `OpenAIProxy` middleware as a **pure reverse proxy**: the request body is lightly rewritten and then passed through to upstream, and the SSE streaming response is returned to the plugin as-is, with no protocol format conversion.

Rewrite rules (only effective for `*chat/completions` paths):

1. **Code-block cleaning**: removes ``` code-block markers in `messages.0.content` and the `edit_file` tool description
2. **auto model replacement**: when `model` is `auto` or empty, replaces it with `COPILOT_AUTO_MODEL`
3. **Model existence validation**: when the model is not in the upstream `/v1/models` list (e.g. `claude-sonnet-4` sent by default by the Copilot CLI), replaces it with `COPILOT_CHAT_DEFAULT_MODEL`, avoiding a 503 from upstream
4. **Session model consistency**: keyed by `X-Interaction-Id`, multi-turn Agent requests within the same session are forced to use the first model, avoiding a mid-conversation switch to another (paid) model
5. **Sampling parameter injection**: when the model is in the `CHAT_CUSTOM_PARAMS_MODELS` list, injects `repetition_penalty` / `temperature` / `top_p` (a value of 0 or empty means no injection)
6. **Request logging**: the rewritten full request body is asynchronously written to `logs/{interactionId}/`
7. **SSE pass-through**: the upstream response is passed through line by line; when the client disconnects, it is silently aborted (`http.ErrAbortHandler`)

Intercepted Host scope:

- `localhost:11434` (Ollama-compatible port, all paths)
- `/responses`, `/chat/completions` on `api.githubcopilot.com`
- `/chat/completions*` on `api.my.ghe.com`

All other requests (model list, token, user info, etc.) are handled by local Gin routes.

---

## Environment Requirements

| Dependency | Version Requirement | Description |
|------|---------|------|
| Go | 1.26+ | Compile ghecopilot |
| Windows | 10/11 | Primary target platform (hosts auto-configuration depends on Windows paths) |
| OpenAI-compatible backend | - | Ollama / vLLM / OneAPI, etc., must support `/v1/chat/completions` |
| Python | 3.9+ | Only required for the Embedding service (if needed) |
| Administrator privileges | - | Required to modify the hosts file |

---

## Quick Start

### 1. Clone the Project

```bash
git clone <repo-url>
cd ghecopilot
```

### 2. Configure Environment Variables

```bash
# Copy from the template
copy .env.example .env
```

Edit the `.env` file, **modifying at least the following configuration**:

```env
# Upstream OpenAI-compatible service address (required); New-API is recommended
UPSTREAM_API_BASE_URL=http://192.168.2.207:3000
UPSTREAM_API_KEY=sk-your-api-key

# Auto model (used when Copilot selects auto)
COPILOT_AUTO_MODEL=Qwen3.8-27B

# Fallback model (used when the requested model is not in the upstream list)
COPILOT_CHAT_DEFAULT_MODEL=Qwen3.8-27B

# Fallback model
COPILOT_CHAT_FALLBACK_MODEL=Qwen3.8-27B

# Domain configuration (modify according to your network environment)
HOSTS_DOMAINS=my.ghe.com,api.my.ghe.com,copilot-proxy.my.ghe.com,copilot-telemetry-service.my.ghe.com
COPILOT_MAIN_BASE_URL=https://my.ghe.com
COPILOT_API_BASE_URL=https://api.my.ghe.com
COPILOT_PROXY_BASE_URL=https://copilot-proxy.my.ghe.com
COPILOT_TELEMETRY_BASE_URL=https://copilot-telemetry-service.my.ghe.com

# JWT secret (recommended to change)
JWT_SECRET=your-random-secret
```

### 3. Start the Service

**Option 1: Use the startup script (recommended)**

```powershell
.\start.ps1
```

The script automatically:
1. Copies `.env.example` → `.env` (if `.env` does not exist)
2. Stops the old ghecopilot process
3. Compiles `ghecopilot.exe`
4. Starts the service

**Option 2: Compile and run manually**

```bash
go build -o ghecopilot.exe
set GIN_MODE=release
.\ghecopilot.exe
```

### 4. Verify the Service

After a successful start, the terminal displays:

```
[HOSTS] ✅ All domains configured
Starting HTTP server on 0.0.0.0:11434
Starting HTTPS server on 0.0.0.0:443
```

Windows pops up a "started successfully" message box.

Visit `https://my.ghe.com/help` to view the configuration guide page.

### 5. Configure the Client

Configure your editor according to the [Client Configuration](#client-configuration) section below.

---

## Configuration Reference

### Environment Variable Details

All configuration is set via the `.env` file or system environment variables.

#### General Configuration

| Variable | Default | Description |
|------|--------|------|
| `PORT` | `11434` | HTTP port. Setting it to `11434` is compatible with Ollama's default port (it is not required to be 11434; if VS2026 specifies Ollama, you can only enter http://localhost:11434, so the default 11434 port is also for convenience) |
| `HTTPS_PORT` | `443` | HTTPS port. Change it if it conflicts locally; you will need to reverse-proxy it yourself |
| `HOST` | `0.0.0.0` | Listen address |

#### Reverse Proxy Configuration

| Variable | Default | Description |
|------|--------|------|
| `UPSTREAM_API_BASE_URL` | - | **Required**. Upstream OpenAI-compatible service address; New-Api is recommended, e.g. `http://192.168.2.207:3000` |
| `UPSTREAM_API_KEY` | - | **Required**. Upstream API Key. |

#### Domain and hosts Configuration

| Variable | Default | Description |
|------|--------|------|
| `HOSTS_DOMAINS` | - | List of domains to hijack (comma-separated). Automatically written to hosts at startup |
| `COPILOT_MAIN_BASE_URL` | `https://my.ghe.com` | Main service address (login page, etc.). **Must be HTTPS** |
| `COPILOT_API_BASE_URL` | `https://api.my.ghe.com` | API service address. The `api` domain prefix **must be fixed** |
| `COPILOT_PROXY_BASE_URL` | `https://copilot-proxy.my.ghe.com` | Proxy service address. The `copilot-proxy` domain prefix **must be fixed** |
| `COPILOT_TELEMETRY_BASE_URL` | `https://copilot-telemetry-service.my.ghe.com` | Telemetry service address. The `copilot-telemetry-service` domain prefix **must be fixed** |

> ⚠️ Domain prefix rules: `api`, `copilot-proxy`, and `copilot-telemetry-service` are prefixes hardcoded by the Copilot plugin and cannot be changed. The main domain `.ghe.com` also cannot be changed, because the official GHE only recognizes this domain — although `*.ghe.com.example.com` also seems to work, probably because it validates that the domain contains `.ghe.com`.

#### Model Configuration

| Variable | Default | Description |
|------|--------|------|
| `COPILOT_AUTO_MODEL` | - | Model name used when the Copilot model selection is `auto` (or empty) |
| `COPILOT_CHAT_DEFAULT_MODEL` | - | Fallback model used when the requested model is not in the upstream list; also marked as `is_chat_default` in the model list |
| `COPILOT_CHAT_FALLBACK_MODEL` | - | Model marked as `is_chat_fallback` in the model list |
| `CHAT_REPETITION_PENALTY` | empty | Repetition penalty for the chat model, range `[1.0, 2.0]` |
| `CHAT_TEMPERATURE` | empty | Chat model temperature, range `[0, 2]` |
| `CHAT_TOP_P` | empty | Chat model nucleus sampling, range `[0, 1.0]` |
| `CHAT_CUSTOM_PARAMS_MODELS` | - | List of models allowed to set the above parameters (comma-separated) |

#### Authentication Configuration

| Variable | Default | Description |
|------|--------|------|
| `JWT_SECRET` | - | **Required**. JWT signing secret. You must log in again after changing it |
| `LOGIN_PASSWORD` | empty | Login page access password. Empty = not set |
| `COPILOT_TOKEN_TTL_SECONDS` | `2147483647` | Spoofed token validity (seconds). Can be set very large for personal use |

#### Other Configuration

| Variable | Default | Description |
|------|--------|------|
| `HTTP_CLIENT_TIMEOUT` | `300` | Global HTTP request timeout (seconds) |
| `AGENT_TOOLS` | empty | Custom Agent tool definitions (JSON array) |

---

## Client Configuration (after the service starts, you can open `https://my.ghe.com/help` to view the configuration guide)

The author uses Copilot CLI, VS Code, and VS2026 heavily; other clients with the Copilot plugin have not been tested, so please test them yourself.

### Copilot CLI
1. Install the Copilot CLI
2. Type `copilot` in the command line to start it
3. Type `/login`
4. Select 2 GitHub Enterprise Cloud (*.ghe.com)
5. Enter `https://my.ghe.com`
6. Choose either `Sign in with your browser (recommended)` or `Sign in with a device code`
7. Complete the login

### VS Code

1. Install the **GitHub Copilot** plugin
2. Open `settings.json` (reference path `C:\Users\{username}\AppData\Roaming\Code\User\settings.json`) and add the following configuration:

```json
{
    "github-enterprise.uri": "https://my.ghe.com",
    "github.copilot.advanced": {
        "authProvider": "github-enterprise"
    }
}
```

3. Restart VS Code, click Copilot login, select **GitHub Enterprise**, and enter `https://my.ghe.com`
4. The browser opens the login page; enter the device code to complete the login

### Visual Studio 2026

1. Update VS 2026 to the latest version
2. Enable GitHub Enterprise account support:
   - **Tools** → **Options** → **Accounts** → check **Include GitHub Enterprise server accounts**
3. Click **Add a GitHub account** and switch to the **GitHub Enterprise** tab
4. Enter `https://my.ghe.com`
5. Complete the device code login

---

## API Endpoint Overview

### Authentication Endpoints

| Method | Path | Description |
|------|------|------|
| `GET` | `/help` | Configuration guide page |
| `POST` | `/login/device/code` | Start the device code login flow |
| `POST` | `/login/device` | Submit device code authorization |
| `GET` | `/login/device` | Device code login page |
| `POST` | `/login/oauth/access_token` | Get the OAuth Access Token |
| `GET` | `/login/oauth/authorize` | OAuth authorization |
| `GET` | `/redirect` / `/callback` | OAuth callback |
| `GET` | `/site/sha` | Enterprise verification |
| `GET` | `/login/config` | Login page configuration |
| `GET` | `/github/login/device/code` | GitHub simulated login page |
| `POST` | `/github/login/device/code` | Get the GitHub device code |
| `POST` | `/github/login/ghu-token` | Get the GitHub user token |

### Copilot Core Endpoints

| Method | Path | Auth | Description |
|------|------|------|------|
| `ANY` | `/models` | None | Model list |
| `ANY` | `/models/*model-id` | None | Model details |
| `POST` | `/auto` | None | Model details (auto) |
| `ANY` | `/_ping` | None | Health check |
| `POST` | `/telemetry` | None | Telemetry data (empty endpoint, only to avoid 404) |
| `ANY` | `/agents` | None | Agent list |
| `ANY` | `/agents/swe/models` | None | SWE model list |
| `ANY` | `/copilot_internal/user` | None | Internal user info |
| `ANY` | `/copilot_internal/managed_settings` | None | Managed settings |
| `ANY` | `/copilot/mcp_registry` | None | MCP registry |
| `ANY` | `/embeddings/models` | None | Embedding model list |
| `GET` | `/copilot_internal/v2/token` | AccessToken | **Copilot token issuance** |
| `GET` | `/user` | AccessToken | User info |
| `GET` | `/user/orgs` | AccessToken | User organizations |
| `GET` | `/api/v3/user` | AccessToken | User info (v3) |
| `GET` | `/api/v3/user/orgs` | AccessToken | User organizations (v3) |
| `GET` | `/teams/:teamID/memberships/:username` | AccessToken | Team member |
| `POST` | `/chunks` | AccessToken | Text chunking + embedding |
| `GET` | `/agents/:name` | None | Agent definition |
| `POST` | `/embeddings` | Token | **Embedding** |
| `GET` | `/api/v3/meta` | None | API v3 meta |
| `GET` | `/api/v3/` | None | API v3 root |
| `GET` | `/` | None | API v3 root |
| `ANY` | `/api/v3/copilot_internal/user` | None | Internal user (v3) |

### Authentication Notes

- **AccessToken**: JWT token, passed via `Authorization: Bearer <jwt>`
- **Token**: Copilot spoofed token, passed via `Authorization: Bearer <copilot_token>`

---

## Frequently Asked Questions

### Q: The startup shows "port already in use"

Check whether `PORT` (11434) and `HTTPS_PORT` (443) are occupied by other programs. You can change the port numbers in `.env`, or stop the programs occupying the ports. HTTPS can only use port 443; domains with a port are not recognized by GitHub Enterprise.

### Q: The browser shows the certificate is not secure when visiting `https://my.ghe.com`

An untrusted certificate makes the service unusable; the certificate must be safe and trusted in order to log in to the local my.ghe.com. You can import `cert/ca.pem` into the system trusted certificate store. On the first run, you will be prompted whether to import the root certificate — click OK, not Cancel.

### Q: Automatic hosts file configuration fails

Modifying the hosts file requires **administrator privileges**. Please:
1. Run the terminal as administrator
2. Or manually edit `C:\Windows\System32\drivers\etc\hosts` and add:
   ```
   127.0.0.1 my.ghe.com
   127.0.0.1 api.my.ghe.com
   127.0.0.1 copilot-proxy.my.ghe.com
   127.0.0.1 copilot-telemetry-service.my.ghe.com
   ```

### Q: Copilot plugin login fails

1. Confirm that `COPILOT_MAIN_BASE_URL` is configured correctly and accessible
2. Confirm that the hosts file is configured correctly
3. Confirm that the browser can open `https://my.ghe.com/login/device?user_code=XXX`
4. Check whether `LOGIN_PASSWORD` is set; if set, it must be entered at login

### Q: No response in the conversation

1. As long as the upstream service is normal and the API key is correct, there is basically no problem

### Q: Upstream returns 503 saying the model does not exist

The plugin (especially the Copilot CLI) may request models that do not exist upstream (e.g. `claude-sonnet-4`). ghecopilot automatically replaces them with the model specified by `COPILOT_CHAT_DEFAULT_MODEL`. Please confirm that this variable is configured to a model that actually exists upstream.

### Q: The plugin disconnects after changing JWT_SECRET

After changing `JWT_SECRET`, you must **log in again** to the Copilot plugin, because the old token's signature is no longer valid.

### Q: How to deploy to Linux

The project is primarily aimed at Windows (hosts auto-configuration depends on Windows paths), but the core service can run on Linux:
1. Manually configure `/etc/hosts`
2. Certificates and HTTPS work normally
3. Import the CA certificate on Windows to make it trusted
4. Point all domains in HOSTS_DOMAINS to the IP of the machine where the Linux service resides

---

## License

[MIT License](LICENSE)

Copyright (c) 2026 mayb
