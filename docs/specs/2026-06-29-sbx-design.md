# sbx 设计文档

- **日期**: 2026-06-29
- **状态**: 待评审(交 Codex / 用户复核)
- **范围**: v1 设计规格,单一实现计划可覆盖

---

## 1. 背景与目标

`sbx` 是一个 **sing-box 配置安全管理 CLI**。它把"改 sing-box 配置"变成一个**可校验、原子、可审计**的操作,既能给人用,也能被 AI agent 安全驱动。

源起:在单节点、多用户(可信小组)的 sing-box 代理(VLESS-REALITY + Hysteria2)运维中,最大的风险不是协议,而是 **配置变更安全(config mutation safety)**——人 / 脚本 / AI 误改一份单一来源的 `config.json`。`sbx` 把这层安全形式化为一个工具。

### 目标(v1)
- **用户生命周期**:`add` / `del` / `list`
- **安全核心**:语义不变量校验(invariant)+ `sing-box check`(schema)+ 原子应用 + git 审计
- **客户端导出**:每用户的 `vless://` / `hy2://` 分享链接 + 订阅
- **agent 友好**:`--json` 输出、`--dry-run` / `plan`、稳定退出码 + 机器可读错误

### 非目标(v1,明确不做)
- 从零生成服务端配置(init-from-scratch)→ v2
- WARP 分流管理 → v2
- 多节点 / 控制平面 / 计费 / RBAC → 超出"单节点可信小组"定位,不做
- 重写 sing-box 的 schema 校验 → 用它自己的 `check`

---

## 2. 架构

### 2.1 核心原则
**sing-box 是 schema 权威。** `sbx` 只拥有它真正独有的价值:**语义层**(invariant)+ **运维安全层**(atomic apply、git)+ **分享链接**。不重写上游、不被上游版本绑架。

### 2.2 配置编辑:保结构 JSON(方案 A)
不反序列化整个配置(那会丢未知字段、打乱顺序)。用 **gjson** 读、**sjson** 写,只动目标 inbound 的 `users` 数组,其余原样保留 → 抗上游 schema 漂移、git diff 最小。

**红队修正 1 — 两步定位+写入**:sjson 的 path 不保证支持 `#(tag=="x")` 查询式写入。因此:
1. gjson 用查询(`inbounds.#(tag=="reality-in")`)**定位 inbound 数组下标**;
2. sjson 用数值下标路径(如 `inbounds.2.users.-1` 追加,删除则重写过滤后的数组)。

**红队修正 2 — UUID 进程内生成**:用 `google/uuid`,不 shell 到 `sing-box generate uuid`。

**格式**:默认 sjson 最小改动;提供可选 `--normalize` 调 `sing-box format` 规范化整份文件(首次采纳会产生一次大 diff,故默认关闭)。

### 2.3 包划分(单一职责,可独立测试)

| 包 | 职责 | 依赖 |
|---|---|---|
| `cmd/sbx` | 入口、Cobra 命令注册 | 各 internal 包 |
| `internal/config` | 原始 JSON 载入/保存、按 tag 定位 inbound、增删 user(gjson/sjson 封装) | gjson/sjson |
| `internal/model` | 只给被管理部分定义小结构体:`User{Name,Secret}`、`InboundRef`、`RealityParams`、`Hy2Params` | 无 |
| `internal/invariant` | 语义校验纯函数:I1 集合相等、I2 secret 唯一 | model |
| `internal/validate` | 封装 `sing-box check`(shell out);支持 `--singbox-bin` 覆盖 | os/exec |
| `internal/apply` | 原子应用管线:flock → 同目录临时文件 → 校验 → fsync → 原子 rename;dry-run 短路 | config/validate/invariant |
| `internal/gitstore` | git 提交/回滚(shell 系统 git);默认开,`--no-git` 关;go-git 留 v2 | os/exec |
| `internal/link` | 从 user + inbound 参数拼 `vless://` / `hy2://` / 订阅 | model |
| `internal/output` | 人类可读 vs `--json` 渲染、稳定退出码 | 无 |

**红队修正 3 — REALITY 公钥推导**:客户端 `vless://` 需要 REALITY **public key**,而服务端 config 只存 **private key**。`internal/link` 必须用 **curve25519 从 private key 反推 public key**(Go 内可算)。server 地址(`config` 里通常是 `::`,无公网 IP)由 `--server` 提供,或落在边车文件 `.sbx.json`。

---

## 3. 命令面

全局 flag:`--config PATH`(默认 `/etc/sing-box/config.json`)、`--json`、`--singbox-bin PATH`、`--no-git`。

| 命令 | 说明 | 关键 flag |
|---|---|---|
| `sbx init` | 采纳现有 `config.json` 入管理:校验、初始化 git 基线 | `--config` `--no-git` |
| `sbx user add <name>` | 生成 UUID,写入两个 inbound,过管线 | `--uuid` `--dry-run` `--json` `--no-reload` |
| `sbx user del <name>` | 从两个 inbound 移除,过管线 | `--dry-run` `--json` `--no-reload` |
| `sbx user list` | 列出受管用户(读自 config) | `--json` |
| `sbx verify` | schema(`sing-box check`)+ invariant(I1/I2) | `--json` |
| `sbx link <name>` | 输出该用户 `vless://` + `hy2://` + 可选订阅 | `--server` `--format vless\|hy2\|sub\|all` `--json` |
| `sbx reload` | check + verify + `systemctl reload`(fallback restart) | `--json` |

"受管用户"定义:出现在 `reality-in` inbound 的 user;`del` 同时从两个 inbound 移除。

---

## 4. 数据流:增删用户管线(add/del 共用)

1. `flock` 串行化(I5)
2. gjson 载入原始 config
3. 计算候选:sjson 写(add:两 inbound 各 append;del:两 inbound 各过滤重写)
4. **校验候选**:`sing-box check`(schema)+ invariant(I1/I2)。任一失败 → 中止,报告失败 `kind`,非零退出,**不落盘**
5. `--dry-run`:输出 diff + 校验结果,停止,**不落盘**
6. **原子应用**:同目录临时文件 → fsync → 原子 rename 覆盖 config(I3)
7. reload:`systemctl reload sing-box`(fallback restart),除非 `--no-reload`
8. git commit(reload 后,记录已生效内容)(I4)
9. 输出结果(human / `--json`)

**失败一律 fail-closed**:全管线通过前 live config 不变。

---

## 5. 错误处理 + agent 接口

### 稳定退出码
| 码 | 含义 |
|---|---|
| 0 | ok |
| 1 | generic |
| 2 | usage error |
| 3 | schema_invalid(`sing-box check` 失败) |
| 4 | invariant_violated |
| 5 | io / apply_error |
| 6 | reload_failed |
| 7 | lock_timeout |

### `--json` 统一信封
```json
{
  "ok": true,
  "action": "user.add",
  "dry_run": false,
  "result": { "name": "alice", "uuid": "..." },
  "error": null
}
```
错误时:`"ok": false`,`"error": { "kind": "...", "detail": "..." }`。
`kind` 取值:`schema_invalid`、`invariant_violated:I1`、`invariant_violated:I2`、`duplicate_user`、`user_not_found`、`lock_timeout`、`reload_failed`、`io_error`。

**agent 工作流**:`sbx user add alice --dry-run --json`(看 diff + 校验)→ 确认 → `sbx user add alice --json`(执行)。工具本身就是机器可读的安全边界——这就是 "agent harness" 的含义,无需任何新 DSL。

---

## 6. 不变量(形式化)
- **I1**:`reality-in.users` 与 `hy2-in.users` 表示同一集合(`name ↔ name`,`reality.uuid == hy2.password`)
- **I2**:secret 唯一
- **I3**:live config 只经"候选通过 `check` + invariant → 原子 rename"替换
- **I4**:每次已应用变更提交 git(diff + 回滚)
- **I5**:变更经 `flock` 串行化

---

## 7. 测试策略
- **单元**:
  - invariant 纯函数(表驱动:集合分叉 / 重复 secret / 空集)
  - config 外科编辑(golden 文件:加用户后**仅** `users` 变、未知字段保留、键顺序稳定)
  - link 生成(golden `vless://` / `hy2://` 串;含 curve25519 公钥推导单测)
- **集成**:PATH 上放**假 `sing-box` stub**(可控返回 0 / 非 0)+ 临时 git repo + 临时 config;断言:原子应用、git 提交、`--dry-run` 不落盘、invariant 失败中止、**临时文件须同目录**(跨文件系统 mktemp 会破坏原子 rename)
- **E2E**(可选):`rogpeppe/go-testscript`
- **CI**:GitHub Actions — `go test` / `go vet` / `golangci-lint` / build matrix;`goreleaser` 发 Release

---

## 8. 分发 / 仓库
- 单静态二进制;`goreleaser` → GitHub Releases(linux amd64/arm64);`go install`
- `LICENSE`:MIT(默认,可改);`README` quickstart;`examples/config.json`
- **名称 `sbx` 为占位**,推送 GitHub 前确认无重名冲突(`sbctl` 已被安全启动工具占用,故避开)

---

## 9. 开放项 / v2 路线
- `init`-from-scratch(REALITY keypair + 自签证书 + 合理默认)
- WARP 分流(OpenAI / Anthropic 干净出口)管理
- MCP server(agent 直接调用工具)
- go-git 替代系统 git(真正零外部依赖)
- 第二节点 / 多节点(需要时才做,非当前定位)
